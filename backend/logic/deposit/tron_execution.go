package deposit

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/internal/tronnative"
	"github.com/0xabcd-k/DexWallet/backend/logic/account"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainreceipt"
	accounts "github.com/0xabcd-k/DexWallet/backend/model/account"
	model "github.com/0xabcd-k/DexWallet/backend/model/deposit"
	"github.com/0xabcd-k/DexWallet/backend/transaction"
	"github.com/shopspring/decimal"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

const tronCreditAction account.Action = "credit_tron_trx"

type TronDepositNetwork interface {
	ReadTRONEvidence(context.Context, string, string) (chainassets.TRONEvidence, error)
	NileTransactionSeen(context.Context, string) (bool, error)
	CheckNileDepositResources(context.Context, chainassets.DepositPreview) error
	SubmitNileTRX(context.Context, tronnative.Signed) error
}
type TronExecution struct {
	conn   sqlx.SqlConn
	db     *transaction.Runner
	ledger *account.Service
	chain  TronDepositNetwork
}

func NewTronExecution(conn sqlx.SqlConn, db *transaction.Runner, ledger *account.Service, chain TronDepositNetwork) *TronExecution {
	return &TronExecution{conn, db, ledger, chain}
}

func (s *TronExecution) Step(ctx context.Context, idx string) (*model.TronDepositOrder, error) {
	rows := model.NewTronDepositOrderModel(s.conn)
	o, err := rows.FindOneByQuoteIdx(ctx, idx)
	if err != nil {
		return nil, err
	}
	if o.State == TronCredited || (o.State == TronReview && o.FinalizedHeight.Valid) {
		return o, nil
	}
	record, err := model.NewDepositQuoteConfirmationModel(s.conn).FindOne(ctx, idx)
	if err != nil {
		return o, err
	}
	var q chainassets.DepositPreview
	if err = json.Unmarshal([]byte(record.SnapshotJson), &q); err != nil {
		return o, err
	}
	if q.QuoteIdx != idx || q.MessageSHA256 != o.TxId || q.ExpiresAt != record.QuoteExpiresAt.UnixMilli() || o.AcceptedAt.Before(record.ConfirmedAt) || o.AcceptedAt.UnixMilli() >= q.ExpiresAt {
		return o, errors.New("inconsistent Nile deposit admission")
	}
	signed, err := q.BindNileDeposit(o.UserSignature, o.OrderSignature)
	if err != nil {
		return o, err
	}
	evidence, err := s.chain.ReadTRONEvidence(ctx, chainassets.TRONNile, o.TxId)
	if err != nil {
		return o, err
	}
	receipt, bandwidth, err := chainreceipt.VerifyNileDeposit(evidence, q, o.UserSignature)
	switch {
	case err == nil || errors.Is(err, chainreceipt.ErrExecutionFailed):
		if err = s.settle(ctx, o, record.UserId, q, receipt, bandwidth, err == nil); err != nil {
			return o, err
		}
		return rows.FindOne(ctx, o.Id)
	case errors.Is(err, chainreceipt.ErrUnconfirmed):
		return o, nil
	case !errors.Is(err, chainreceipt.ErrTRONSolidAbsent):
		return o, err
	}
	if o.State == TronReview {
		return o, nil
	}
	seen, err := s.chain.NileTransactionSeen(ctx, o.TxId)
	if err != nil || seen {
		return o, err
	}
	if signed.Expired(time.Now()) {
		return s.review(ctx, o)
	}
	if err = s.chain.CheckNileDepositResources(ctx, q); err != nil {
		return o, err
	}
	return o, s.chain.SubmitNileTRX(ctx, signed)
}
func (s *TronExecution) review(ctx context.Context, o *model.TronDepositOrder) (*model.TronDepositOrder, error) {
	if _, err := s.conn.ExecCtx(ctx, "UPDATE tron_deposit_order SET state=? WHERE id=? AND state=?", TronReview, o.Id, TronSigned); err != nil {
		return o, err
	}
	return model.NewTronDepositOrderModel(s.conn).FindOne(ctx, o.Id)
}
func (s *TronExecution) settle(ctx context.Context, o *model.TronDepositOrder, user int64, q chainassets.DepositPreview, r chainreceipt.Receipt, bandwidth int64, succeeded bool) error {
	fee, err := strconv.ParseInt(r.Transaction.FeeAtoms, 10, 64)
	if err != nil {
		return err
	}
	return s.db.TransactCtx(ctx, func(ctx context.Context, session sqlx.Session) error {
		var current struct {
			State  string        `db:"state"`
			Height sql.NullInt64 `db:"finalized_height"`
		}
		if err := session.QueryRowCtx(ctx, &current, "SELECT state,finalized_height FROM tron_deposit_order WHERE id=? FOR UPDATE", o.Id); err != nil {
			return err
		}
		if current.State == TronCredited || (current.State == TronReview && current.Height.Valid) {
			return nil
		}
		if !succeeded || fee != 0 || bandwidth != int64(q.DelegatedBandwidth) {
			_, err := session.ExecCtx(ctx, "UPDATE tron_deposit_order SET state=?,actual_fee_sun=?,actual_bandwidth=?,finalized_height=? WHERE id=?", TronReview, fee, bandwidth, r.Transaction.Slot, o.Id)
			return err
		}
		var assigned bool
		if err := session.QueryRowCtx(ctx, &assigned, `SELECT EXISTS(SELECT 1 FROM chain_observed_transaction t JOIN chain_transfer_event e ON e.transaction_id=t.id
   JOIN chain_receive_request r ON r.matched_event_id=e.id WHERE t.network=? AND t.signature=? AND e.event_kind=? AND e.instruction_index=0)`, chainassets.TRONNile, o.TxId, chainreceipt.TRONTransfer); err != nil {
			return err
		}
		if assigned {
			return chainreceipt.ErrReceiveConflict
		}
		rate, err := decimal.NewFromString(q.USDPerCoin)
		if err != nil || !rate.IsPositive() || q.GasUSDPerCoin != q.USDPerCoin {
			return errors.New("invalid locked TRX/USD rate")
		}
		sun, err := strconv.ParseInt(r.Event.Atoms, 10, 64)
		if err != nil {
			return err
		}
		usd, _ := accounts.Lookup(accounts.KindUSD)
		net := decimal.New(sun, -6).Mul(rate).Truncate(usd.Scale)
		if !net.IsPositive() || net.StringFixed(usd.Scale) != q.NetUSD {
			return errors.New("TRX proceeds differ from confirmed quote")
		}
		voucher, err := s.ledger.Post(ctx, session, account.Posting{Source: account.Source{Type: account.BusinessDeposit, ID: o.Id, Action: tronCreditAction}, Entries: []account.Entry{{System: account.SystemUSDClearing, Side: account.SideDebit, Amount: net}, account.Credit(user, accounts.KindUSD, net)}})
		if err != nil {
			return err
		}
		_, err = session.ExecCtx(ctx, "UPDATE tron_deposit_order SET state=?,actual_fee_sun=0,actual_bandwidth=?,finalized_height=?,credit_voucher_id=?,credited_usd=? WHERE id=?", TronCredited, bandwidth, r.Transaction.Slot, voucher, net, o.Id)
		return err
	})
}
