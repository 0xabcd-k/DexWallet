package deposit

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"strings"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/internal/baseusdc"
	"github.com/0xabcd-k/DexWallet/backend/logic/account"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainreceipt"
	accounts "github.com/0xabcd-k/DexWallet/backend/model/account"
	model "github.com/0xabcd-k/DexWallet/backend/model/deposit"
	"github.com/0xabcd-k/DexWallet/backend/transaction"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/shopspring/decimal"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

const baseCreditAction account.Action = "credit_base_usdc"
const baseETHCreditAction account.Action = "credit_base_eth"

type BaseDepositNetwork interface {
	BaseDepositEnvelope(context.Context, chainassets.DepositPreview, []byte) (*types.Transaction, error)
	BroadcastBaseDeposit(context.Context, *types.Transaction, chainassets.DepositPreview, string) error
	ReadBaseEvidence(context.Context, string, string) (chainassets.BaseEvidence, error)
	ReadBaseDepositETHEvidence(context.Context, string, string) (chainassets.BaseEvidence, error)
}
type BaseExecution struct {
	conn   sqlx.SqlConn
	db     *transaction.Runner
	ledger *account.Service
	chain  BaseDepositNetwork
}

func NewBaseExecution(conn sqlx.SqlConn, db *transaction.Runner, ledger *account.Service, chain BaseDepositNetwork) *BaseExecution {
	return &BaseExecution{conn, db, ledger, chain}
}

type BaseBudget struct {
	Recipient                    string
	ETHWei, USDCAtoms, MaxFeeWei *big.Int
}

// Only the unsigned stage needs the local operator's one-order budget and key.
// Later steps reconstruct and observe the single durable wire even after expiry.
func (s *BaseExecution) Step(ctx context.Context, idx string, budget BaseBudget, key *ecdsa.PrivateKey) (*model.BaseDepositOrder, error) {
	rows := model.NewBaseDepositOrderModel(s.conn)
	o, err := rows.FindOneByQuoteIdx(ctx, idx)
	if err != nil {
		return nil, err
	}
	if o.State == BaseCredited || (o.State == BaseReview && (!o.TxHash.Valid || o.ActualFeeWei != "")) {
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
	if o.AssetId != q.AssetID || q.QuoteIdx != idx || q.ExpiresAt != record.QuoteExpiresAt.UnixMilli() || o.AcceptedAt.Before(record.ConfirmedAt) || o.AcceptedAt.UnixMilli() >= q.ExpiresAt || !strings.EqualFold(o.Sponsor, q.FeePayer) {
		return o, errors.New("inconsistent Base deposit admission")
	}
	a, err := q.BaseAuthorization()
	if err != nil {
		return o, err
	}
	data, err := a.Data(o.UserSignature)
	if err != nil {
		return o, err
	}
	cap, err := q.BaseFeeLimitWei()
	if err != nil {
		return o, err
	}
	mark := func(state string) (*model.BaseDepositOrder, error) {
		if state == o.State {
			return o, nil
		}
		_, err := s.conn.ExecCtx(ctx, "UPDATE base_deposit_order SET state=? WHERE id=? AND state=?", state, o.Id, o.State)
		if err != nil {
			return o, err
		}
		return rows.FindOne(ctx, o.Id)
	}
	if !o.TxHash.Valid {
		if time.Now().Unix() >= q.BaseValidBefore {
			return mark(BaseReview)
		}
		gross := budget.USDCAtoms
		if q.AssetID == chainassets.BaseSepoliaETH {
			gross = budget.ETHWei
		}
		if key == nil || gross == nil || budget.MaxFeeWei == nil || a.Amount.Cmp(gross) != 0 || cap.Cmp(budget.MaxFeeWei) > 0 || !common.IsHexAddress(budget.Recipient) || common.HexToAddress(budget.Recipient) != a.To || crypto.PubkeyToAddress(key.PublicKey) != common.HexToAddress(q.FeePayer) {
			return o, errors.New("explicit matching Base sponsor, recipient, asset quantity and fee budget required")
		}
		unsigned, err := s.chain.BaseDepositEnvelope(ctx, q, data)
		if err != nil {
			return o, err
		}
		signed, err := types.SignTx(unsigned, types.LatestSignerForChainID(unsigned.ChainId()), key)
		if err != nil {
			return o, err
		}
		if err = chainassets.CheckBaseDepositWire(q, signed, data); err != nil {
			return o, err
		}
		wire, err := signed.MarshalBinary()
		if err != nil {
			return o, err
		}
		if signed.Nonce() > math.MaxInt64 {
			return o, errors.New("unsupported sponsor nonce")
		}
		_, err = s.conn.ExecCtx(ctx, "UPDATE base_deposit_order SET state=?,sponsor_nonce=?,signed_wire=?,tx_hash=? WHERE id=? AND state=?", BaseSigned, signed.Nonce(), hexutil.Encode(wire), signed.Hash().Hex(), o.Id, Accepted)
		if err != nil {
			return o, err
		}
		return rows.FindOne(ctx, o.Id)
	}
	wire, err := hexutil.Decode(o.SignedWire)
	if err != nil {
		return o, err
	}
	var tx types.Transaction
	if err = tx.UnmarshalBinary(wire); err != nil {
		return o, err
	}
	if tx.Hash().Hex() != o.TxHash.String || !o.SponsorNonce.Valid || uint64(o.SponsorNonce.Int64) != tx.Nonce() {
		return o, errors.New("inconsistent saved Base transaction")
	}
	if err := chainassets.CheckBaseDepositWire(q, &tx, data); err != nil {
		return o, err
	}
	read := s.chain.ReadBaseEvidence
	if q.AssetID == chainassets.BaseSepoliaETH {
		read = s.chain.ReadBaseDepositETHEvidence
	}
	e, err := read(ctx, chainassets.BaseSepolia, o.TxHash.String)
	if err != nil {
		return o, err
	}
	r, err := chainreceipt.VerifyBaseDeposit(e, &tx, q, o.UserSignature)
	switch {
	case err == nil:
		if err = s.credit(ctx, o, record.UserId, q, r); err != nil {
			return o, err
		}
		return rows.FindOne(ctx, o.Id)
	case errors.Is(err, chainreceipt.ErrExecutionFailed):
		// Keep finalized reverts for review. For EIP-3009 another relayer
		// may already have transferred USDC under the same authorization.
		return mark(BaseReview)
	case errors.Is(err, chainreceipt.ErrUnconfirmed):
		return o, nil
	case !errors.Is(err, chainreceipt.ErrBaseHashAbsent):
		return o, err
	}
	if time.Now().Unix() >= q.BaseValidBefore {
		return mark(BaseReview)
	}
	if o.State == BaseReview {
		return o, nil
	}
	if !bytes.Equal(bytes.TrimSpace(e.Transaction), []byte("null")) || !bytes.Equal(bytes.TrimSpace(e.Receipt), []byte("null")) {
		return o, errors.New("incomplete Base history lookup")
	}
	return o, s.chain.BroadcastBaseDeposit(ctx, &tx, q, o.UserSignature)
}

func (s *BaseExecution) credit(ctx context.Context, o *model.BaseDepositOrder, userID int64, q chainassets.DepositPreview, r chainreceipt.Receipt) error {
	fee, ok := new(big.Int).SetString(r.Transaction.FeeAtoms, 10)
	if !ok || fee.Sign() < 0 {
		return errors.New("invalid verified network fee")
	}
	cap, err := q.BaseFeeLimitWei()
	if err != nil {
		return err
	}
	return s.db.TransactCtx(ctx, func(ctx context.Context, session sqlx.Session) error {
		var state string
		if err := session.QueryRowCtx(ctx, &state, "SELECT state FROM base_deposit_order WHERE id=? FOR UPDATE", o.Id); err != nil {
			return err
		}
		if state == BaseCredited {
			return nil
		}
		if state != BaseSigned && state != BaseReview {
			return errors.New("ineligible Base credit state")
		}
		if fee.Cmp(cap) > 0 {
			_, err := session.ExecCtx(ctx, "UPDATE base_deposit_order SET state=?,actual_fee_wei=?,finalized_height=? WHERE id=?", BaseReview, fee.String(), r.Transaction.Slot, o.Id)
			return err
		}
		net, err := baseCreditAmount(q, r.Event.Atoms, fee, cap)
		if err != nil {
			return err
		}
		var assigned bool
		if err := session.QueryRowCtx(ctx, &assigned, `SELECT EXISTS(SELECT 1 FROM chain_observed_transaction t JOIN chain_transfer_event e ON e.transaction_id=t.id
   JOIN chain_receive_request r ON r.matched_event_id=e.id WHERE t.network=? AND t.signature=? AND e.event_kind=? AND e.instruction_index=?)`, chainassets.BaseSepolia, o.TxHash.String, r.Event.Kind, r.Event.Instruction); err != nil {
			return err
		}
		if assigned {
			return fmt.Errorf("Base deposit %s: %w", o.QuoteIdx, chainreceipt.ErrReceiveConflict)
		}
		action := baseCreditAction
		if q.AssetID == chainassets.BaseSepoliaETH {
			action = baseETHCreditAction
		}
		voucher, err := s.ledger.Post(ctx, session, account.Posting{Source: account.Source{Type: account.BusinessDeposit, ID: o.Id, Action: action}, Entries: []account.Entry{{System: account.SystemUSDClearing, Side: account.SideDebit, Amount: net}, account.Credit(userID, accounts.KindUSD, net)}})
		if err != nil {
			return err
		}
		_, err = session.ExecCtx(ctx, "UPDATE base_deposit_order SET state=?,actual_fee_wei=?,finalized_height=?,credit_voucher_id=?,credited_usd=? WHERE id=?", BaseCredited, fee.String(), r.Transaction.Slot, voucher, net, o.Id)
		return err
	})
}

// Apply both original USD rates and round only the final ledger amount.
func baseCreditAmount(q chainassets.DepositPreview, atoms string, fee, cap *big.Int) (decimal.Decimal, error) {
	rate, err := decimal.NewFromString(q.USDPerCoin)
	if err != nil || !rate.IsPositive() {
		return decimal.Zero, errors.New("invalid locked deposit asset rate")
	}
	gasRate, err := decimal.NewFromString(q.GasUSDPerCoin)
	if err != nil || !gasRate.IsPositive() {
		return decimal.Zero, errors.New("invalid locked ETH/USD rate")
	}
	received, err := decimal.NewFromString(atoms)
	if err != nil {
		return decimal.Zero, err
	}
	usd, _ := accounts.Lookup(accounts.KindUSD)
	var scale int32
	switch q.AssetID {
	case baseusdc.AssetID:
		scale = 6
	case chainassets.BaseSepoliaETH:
		scale = 18
		if !rate.Equal(gasRate) {
			return decimal.Zero, errors.New("different ETH rates in same quote")
		}
	default:
		return decimal.Zero, errors.New("unsupported deposit settlement asset")
	}
	gross := received.Shift(-scale).Mul(rate)
	quoted := gross.Sub(decimal.NewFromBigInt(cap, -18).Mul(gasRate)).Truncate(usd.Scale)
	if quoted.StringFixed(usd.Scale) != q.NetUSD {
		return decimal.Zero, errors.New("original Base net differs from locked terms")
	}
	net := gross.Sub(decimal.NewFromBigInt(fee, -18).Mul(gasRate)).Truncate(usd.Scale)
	if !net.IsPositive() {
		return decimal.Zero, errors.New("nonpositive Base deposit credit")
	}
	return net, nil
}
