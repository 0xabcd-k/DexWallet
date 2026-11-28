package deposit

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/logic/account"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainreceipt"
	accounts "github.com/0xabcd-k/DexWallet/backend/model/account"
	model "github.com/0xabcd-k/DexWallet/backend/model/deposit"
	"github.com/0xabcd-k/DexWallet/backend/transaction"
	"github.com/shopspring/decimal"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

const tonCreditAction account.Action = "credit_ton_native"

const tonJettonCreditAction account.Action = "credit_ton_test_usdt"

type TonDepositNetwork interface {
	ReadTONJettonDepositEvidence(context.Context, chainassets.TONDepositRelay) (chainassets.TONEvidence, error)
	CheckTONDeposit(context.Context, chainassets.DepositPreview, []byte, []byte) error
	ReadTONDepositEvidence(context.Context, string) (chainassets.TONEvidence, error)
	SubmitTON(context.Context, string, []byte) error
}
type TonExecution struct {
	conn   sqlx.SqlConn
	db     *transaction.Runner
	ledger *account.Service
	chain  TonDepositNetwork
}

func NewTonExecution(conn sqlx.SqlConn, db *transaction.Runner, ledger *account.Service, chain TonDepositNetwork) *TonExecution {
	return &TonExecution{conn, db, ledger, chain}
}

type TonBudget struct {
	Sender, Recipient                        string
	MaxGrossNano, MaxJettonAtoms, MaxFeeNano int64
}

func (s *TonExecution) Step(ctx context.Context, idx string, b TonBudget, key ed25519.PrivateKey) (*model.TonDepositOrder, error) {
	rows := model.NewTonDepositOrderModel(s.conn)
	o, err := rows.FindOneByQuoteIdx(ctx, idx)
	if err != nil {
		return nil, err
	}
	if o.ExecutionFailed != 0 || o.State == TonCredited || (o.State == TonReview && (!o.DispatchHash.Valid || o.ReceiveHash.Valid)) {
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
	if q.QuoteIdx != idx || record.QuoteExpiresAt.UnixMilli() != q.ExpiresAt || o.AcceptedAt.Before(record.ConfirmedAt) || o.AcceptedAt.UnixMilli() >= q.ExpiresAt {
		return o, errors.New("inconsistent TON admission")
	}
	r, err := q.BindTONDeposit(o.UserSignature, o.OrderSignature)
	if err != nil {
		return o, err
	}
	var paymentKey sql.NullString
	if r.Jetton != nil {
		paymentKey = sql.NullString{String: chainassets.TONJettonPaymentKey(r.Sender, r.Jetton.PaymentHash), Valid: true}
	}
	if paymentKey != o.JettonPaymentKey || r.AuthHash != o.AuthHash {
		return o, errors.New("incorrect persisted TON authorization hash")
	}
	userSig, _ := hex.DecodeString(o.UserSignature)
	cap, err := decimal.NewFromString(q.MaxGas)
	if err != nil || !cap.IsPositive() || !cap.Shift(9).IsInteger() {
		return o, errors.New("invalid TON fee budget")
	}
	review := func() (*model.TonDepositOrder, error) {
		if o.State != TonReview {
			if _, err := s.conn.ExecCtx(ctx, "UPDATE ton_deposit_order SET state=? WHERE id=? AND state=?", TonReview, o.Id, o.State); err != nil {
				return o, err
			}
		}
		return rows.FindOne(ctx, o.Id)
	}
	if !o.DispatchHash.Valid {
		if time.Now().Unix() >= int64(q.TON.ValidUntil) {
			return review()
		}
		limit := b.MaxGrossNano
		if r.Jetton != nil {
			limit = b.MaxJettonAtoms
		}
		if o.State != Accepted || len(key) != ed25519.PrivateKeySize || b.Sender != r.Sender || b.Recipient != r.Recipient || limit <= 0 || r.Atoms.Cmp(big.NewInt(limit)) > 0 || cap.GreaterThan(decimal.New(b.MaxFeeNano, -9)) {
			return o, errors.New("matching test sponsor, payer, recipient and explicit TON budgets required")
		}
		sig := ed25519.Sign(key, r.PlatformDigest)
		r, err = q.TONRelay(userSig, sig)
		if err != nil {
			return o, err
		}

		_, err = s.conn.ExecCtx(ctx, "UPDATE ton_deposit_order SET state=?,platform_signature=?,dispatch_hash=? WHERE id=? AND state=?", TonSigned, hex.EncodeToString(sig), r.DispatchHash, o.Id, Accepted)
		if err != nil {
			return o, err
		}
		return rows.FindOne(ctx, o.Id)
	}
	sig, err := hex.DecodeString(o.PlatformSignature.String)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return o, errors.New("missing stored TON platform signature")
	}
	r, err = q.TONRelay(userSig, sig)
	if err != nil || r.DispatchHash != o.DispatchHash.String {
		return o, errors.New("signed TON dispatch differs from stored hash")
	}
	var received chainreceipt.Receipt
	var fee *big.Int
	var costs *chainreceipt.TONJettonCosts
	if r.Jetton != nil {
		e, readErr := s.chain.ReadTONJettonDepositEvidence(ctx, r)
		if readErr != nil {
			return o, readErr
		}
		received, fee, costs, err = chainreceipt.VerifyTONJettonDeposit(e, r)
	} else {
		e, readErr := s.chain.ReadTONDepositEvidence(ctx, r.DispatchHash)
		if readErr != nil {
			return o, readErr
		}
		received, fee, err = chainreceipt.VerifyTONDeposit(e, r)
	}
	switch {
	case err == nil || (errors.Is(err, chainreceipt.ErrExecutionFailed) && fee != nil):
		if err = s.settle(ctx, o, record.UserId, q, received, fee, costs, cap, err == nil); err != nil {
			return o, err
		}
		return rows.FindOne(ctx, o.Id)
	case errors.Is(err, chainreceipt.ErrExecutionFailed):
		_, err = s.conn.ExecCtx(ctx, "UPDATE ton_deposit_order SET state=?,execution_failed=1 WHERE id=? AND state IN (?,?) AND receive_hash IS NULL", TonReview, o.Id, TonSigned, TonReview)
		if err != nil {
			return o, err
		}
		return rows.FindOne(ctx, o.Id)
	case errors.Is(err, chainreceipt.ErrUnconfirmed):
		if time.Now().Unix() >= int64(q.TON.ValidUntil) {
			return review()
		}
		return o, nil
	case !errors.Is(err, chainreceipt.ErrTONDispatchAbsent):
		return o, err
	}
	if o.State == TonReview || time.Now().Unix() >= int64(q.TON.ValidUntil) {
		return review()
	}
	if err = s.chain.CheckTONDeposit(ctx, q, userSig, sig); err != nil {
		return o, err
	}
	if time.Now().Unix() >= int64(q.TON.ValidUntil) {
		return review()
	}
	return o, s.chain.SubmitTON(ctx, chainassets.TONTestnet, r.Wire)
}

func (s *TonExecution) settle(ctx context.Context, o *model.TonDepositOrder, user int64, q chainassets.DepositPreview, r chainreceipt.Receipt, fee *big.Int, costs *chainreceipt.TONJettonCosts, cap decimal.Decimal, success bool) error {
	if fee == nil || fee.Sign() <= 0 || !fee.IsInt64() {
		return errors.New("invalid TON trace fee")
	}
	cost := decimal.NewFromBigInt(fee, -9)
	return s.db.TransactCtx(ctx, func(ctx context.Context, tx sqlx.Session) error {
		var cur struct {
			Failed  int64          `db:"execution_failed"`
			State   string         `db:"state"`
			Receive sql.NullString `db:"receive_hash"`
		}
		if err := tx.QueryRowCtx(ctx, &cur, "SELECT execution_failed,state,receive_hash FROM ton_deposit_order WHERE id=? FOR UPDATE", o.Id); err != nil {
			return err
		}
		if cur.Failed != 0 || cur.State == TonCredited || cur.Receive.Valid {
			return nil
		}
		var excess, reserve, surplus, senderPaid sql.NullInt64
		action := tonCreditAction
		if costs != nil {
			excess = sql.NullInt64{Int64: costs.Excess, Valid: true}
			reserve = sql.NullInt64{Int64: costs.Reserve, Valid: true}
			surplus = sql.NullInt64{Int64: costs.Surplus, Valid: true}
			senderPaid = sql.NullInt64{Int64: costs.SenderPaid, Valid: true}
			action = tonJettonCreditAction
		}
		if !success || cost.GreaterThan(cap) {
			_, err := tx.ExecCtx(ctx, "UPDATE ton_deposit_order SET state=?,receive_hash=?,actual_fee_nano=?,jetton_excess_nano=?,jetton_reserve_nano=?,jetton_sender_fee_nano=?,relay_surplus_nano=? WHERE id=?", TonReview, r.Transaction.Signature, fee.Int64(), excess, reserve, senderPaid, surplus, o.Id)
			return err
		}
		var assigned bool
		if err := tx.QueryRowCtx(ctx, &assigned, `SELECT EXISTS(SELECT 1 FROM chain_observed_transaction t JOIN chain_transfer_event e ON e.transaction_id=t.id JOIN chain_receive_request c ON c.matched_event_id=e.id WHERE t.network=? AND t.signature=? AND e.event_kind=?)`, chainassets.TONTestnet, r.Transaction.Signature, r.Event.Kind); err != nil {
			return err
		}
		if assigned {
			return chainreceipt.ErrReceiveConflict
		}
		net, err := tonDepositProceeds(q, r.Event.Atoms, cost, senderPaid.Int64)
		if err != nil {
			return err
		}
		voucher, err := s.ledger.Post(ctx, tx, account.Posting{Source: account.Source{Type: account.BusinessDeposit, ID: o.Id, Action: action}, Entries: []account.Entry{{System: account.SystemUSDClearing, Side: account.SideDebit, Amount: net}, account.Credit(user, accounts.KindUSD, net)}})
		if err != nil {
			return err
		}
		_, err = tx.ExecCtx(ctx, "UPDATE ton_deposit_order SET state=?,receive_hash=?,actual_fee_nano=?,jetton_excess_nano=?,jetton_reserve_nano=?,jetton_sender_fee_nano=?,relay_surplus_nano=?,credit_voucher_id=?,credited_usd=? WHERE id=?", TonCredited, r.Transaction.Signature, fee.Int64(), excess, reserve, senderPaid, surplus, voucher, net, o.Id)
		return err
	})
}

func tonDepositProceeds(q chainassets.DepositPreview, atoms string, total decimal.Decimal, senderPaidNano int64) (decimal.Decimal, error) {
	recoverable := total.Sub(decimal.New(senderPaidNano, -9))
	rate, err := decimal.NewFromString(q.USDPerCoin)
	gasRate, gasErr := decimal.NewFromString(q.GasUSDPerCoin)
	if err != nil || gasErr != nil || !gasRate.IsPositive() || !rate.IsPositive() || (q.AssetID != chainassets.TONDepositTestUSDT && q.GasUSDPerCoin != q.USDPerCoin) {
		return decimal.Zero, errors.New("invalid locked TON/USD rate")
	}
	gross, err := decimal.NewFromString(atoms)
	if err != nil {
		return decimal.Zero, err
	}
	usd, _ := accounts.Lookup(accounts.KindUSD)
	net := gross.Shift(-int32(q.PayDecimals)).Mul(rate).Sub(recoverable.Mul(gasRate)).Truncate(usd.Scale)
	minimum, err := decimal.NewFromString(q.NetUSD)
	if err != nil || !minimum.IsPositive() || net.LessThan(minimum) {
		return decimal.Zero, errors.New("TON proceeds below confirmed quote")
	}
	return net, nil
}
