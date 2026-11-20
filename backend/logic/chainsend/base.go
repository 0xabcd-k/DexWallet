package chainsend

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"

	"github.com/0xabcd-k/DexWallet/backend/internal/baseusdc"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainreceipt"
	model "github.com/0xabcd-k/DexWallet/backend/model/chainsend"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/go-sql-driver/mysql"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

const BaseConfirmed = "confirmed"
const nativeTransferGas = 21000

var ErrUnsupportedBaseToken = errors.New("Base node does not support USDC payouts")
var ErrBaseNonceReserved = errors.New("Base sender nonce already reserved by another payout")
var ErrBaseNonceChanged = errors.New("Base pending nonce differs; reconcile original transaction hash")
var baseSigner = types.NewLondonSigner(big.NewInt(chainassets.BaseSepoliaChainID))

type BaseNode interface {
	PendingBaseNonce(context.Context, string) (uint64, error)
	SubmitBaseETH(context.Context, *types.Transaction) error
}
type Base struct {
	conn     sqlx.SqlConn
	rows     model.BasePayoutModel
	sender   string
	node     BaseNode
	receipts *chainreceipt.BaseService
}

func baseAddress(address string) (string, error) {
	normalized, err := chainassets.NormalizeBaseAddress(address)
	if err != nil || common.HexToAddress(normalized) == (common.Address{}) {
		return "", fmt.Errorf("%w: invalid Base payment address", ErrInvalidRequest)
	}
	return normalized, nil
}
func NewBase(conn sqlx.SqlConn, sender string, node BaseNode, receipts *chainreceipt.BaseService) (*Base, error) {
	sender, err := baseAddress(sender)
	if err != nil || conn == nil || node == nil || receipts == nil {
		return nil, ErrInvalidRequest
	}
	return &Base{conn, model.NewBasePayoutModel(conn), sender, node, receipts}, nil
}
func (s *Base) Find(ctx context.Context, key string) (*model.BasePayout, error) {
	if !requestKey.MatchString(key) {
		return nil, ErrInvalidRequest
	}
	row, err := s.rows.FindOneByRequestKey(ctx, key)
	if err == nil && row.Sender != s.sender {
		return nil, ErrConflict
	}
	return row, err
}
func baseUint(raw string) (*big.Int, error) {
	if len(raw) == 0 || len(raw) > 78 {
		return nil, ErrInvalidRequest
	}
	n, ok := new(big.Int).SetString(raw, 10)
	if !ok || n.Sign() < 0 || n.BitLen() > 256 || n.String() != raw {
		return nil, ErrInvalidRequest
	}
	return n, nil
}
func baseUnsigned(row *model.BasePayout) (*types.Transaction, error) {
	fee, err := baseUint(row.FeeCap)
	if err != nil || fee.Sign() == 0 {
		return nil, fmt.Errorf("%w: invalid max fee per gas", ErrInvalidRequest)
	}
	tip, err := baseUint(row.TipCap)
	if err != nil || tip.Cmp(fee) > 0 {
		return nil, fmt.Errorf("%w: invalid priority fee per gas", ErrInvalidRequest)
	}
	nonce, err := strconv.ParseUint(row.Nonce, 10, 64)
	if err != nil || nonce == math.MaxUint64 || strconv.FormatUint(nonce, 10) != row.Nonce || row.GasLimit < nativeTransferGas || row.ChainId != chainassets.BaseSepoliaChainID {
		return nil, ErrInvalidRequest
	}
	addr, err := baseAddress(row.Recipient)
	if err != nil || addr == row.Sender {
		return nil, ErrInvalidRequest
	}
	to := common.HexToAddress(addr)
	tx := &types.DynamicFeeTx{ChainID: big.NewInt(row.ChainId), Nonce: nonce, To: &to, Gas: uint64(row.GasLimit), GasFeeCap: fee, GasTipCap: tip}
	switch row.AssetId {
	case chainassets.BaseSepoliaETH:
		value, err := baseUint(row.WeiAmount)
		if err != nil || value.Sign() == 0 || row.TokenAtoms != "" {
			return nil, fmt.Errorf("%w: invalid ETH amount in wei", ErrInvalidRequest)
		}
		tx.Value = value
	case baseusdc.AssetID:
		amount, err := baseUint(row.TokenAtoms)
		if err != nil || amount.Sign() == 0 || row.WeiAmount != "0" {
			return nil, fmt.Errorf("%w: invalid USDC transfer amounts", ErrInvalidRequest)
		}
		contract := common.HexToAddress(baseusdc.SepoliaContract)
		tx.To = &contract
		tx.Data = baseusdc.TransferData(to, amount)
	default:
		return nil, fmt.Errorf("%w: unsupported Base payout asset", ErrInvalidRequest)
	}
	return types.NewTx(tx), nil
}

type baseTokenNode interface {
	SubmitBaseUSDC(context.Context, *types.Transaction) error
}

func (s *Base) broadcaster(assetID string) (func(context.Context, *types.Transaction) error, error) {
	switch assetID {
	case chainassets.BaseSepoliaETH:
		return s.node.SubmitBaseETH, nil
	case baseusdc.AssetID:
		token, ok := s.node.(baseTokenNode)
		if !ok {
			return nil, ErrUnsupportedBaseToken
		}
		return token.SubmitBaseUSDC, nil
	default:
		return nil, fmt.Errorf("%w: unsupported Base payout asset", ErrInvalidRequest)
	}
}

type BaseTransfer struct {
	AssetID    string
	TokenAtoms string
	Recipient  string
	WeiAmount  string
	GasLimit   int64
	FeeCap     string
	TipCap     string
}

// The caller explicitly approves the gas limit and both EIP-1559 caps. A new
// business key may reserve only the current pending nonce on this test wallet.
func (s *Base) Prepare(ctx context.Context, key string, payment BaseTransfer) (*model.BasePayout, error) {
	if payment.AssetID == "" {
		payment.AssetID = chainassets.BaseSepoliaETH
	}
	recipient, err := baseAddress(payment.Recipient)
	if err != nil {
		return nil, err
	}
	existing := func() (*model.BasePayout, error) {
		row, err := s.Find(ctx, key)
		if err == nil && (row.Recipient != recipient || row.AssetId != payment.AssetID || row.TokenAtoms != payment.TokenAtoms || row.WeiAmount != payment.WeiAmount || row.GasLimit != payment.GasLimit || row.FeeCap != payment.FeeCap || row.TipCap != payment.TipCap) {
			return nil, ErrConflict
		}
		return row, err
	}
	if row, err := existing(); !errors.Is(err, model.ErrNotFound) {
		return row, err
	}
	if _, err := s.broadcaster(payment.AssetID); err != nil {
		return nil, err
	}
	nonce, err := s.node.PendingBaseNonce(ctx, s.sender)
	if err != nil {
		return nil, err
	}
	row := &model.BasePayout{
		RequestKey: key, ChainId: chainassets.BaseSepoliaChainID, Sender: s.sender, Recipient: recipient,
		AssetId: payment.AssetID, WeiAmount: payment.WeiAmount, TokenAtoms: payment.TokenAtoms,
		Nonce: strconv.FormatUint(nonce, 10), GasLimit: payment.GasLimit, FeeCap: payment.FeeCap, TipCap: payment.TipCap, State: Prepared,
	}
	tx, err := baseUnsigned(row)
	if err != nil {
		return nil, err
	}
	row.SigningHash = baseSigner.Hash(tx).Hex()
	// Let MySQL initialize the schedule and its UTC clock.
	_, err = s.conn.ExecCtx(ctx, "INSERT INTO base_payout(request_key,chain_id,sender,recipient,asset_id,wei_amount,token_atoms,nonce,gas_limit,fee_cap,tip_cap,signing_hash,state) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)", key, row.ChainId, row.Sender, row.Recipient, row.AssetId, row.WeiAmount, row.TokenAtoms, row.Nonce, row.GasLimit, row.FeeCap, row.TipCap, row.SigningHash, Prepared)
	var duplicate *mysql.MySQLError
	if errors.As(err, &duplicate) && duplicate.Number == 1062 {
		row, err = existing()
		if errors.Is(err, model.ErrNotFound) {
			return nil, ErrBaseNonceReserved
		}
		return row, err
	}
	if err != nil {
		return nil, err
	}
	return s.Find(ctx, key)
}
func baseSigned(row *model.BasePayout, wire string) (*types.Transaction, error) {
	unsigned, err := baseUnsigned(row)
	if err != nil {
		return nil, err
	}
	if row.SigningHash != baseSigner.Hash(unsigned).Hex() {
		return nil, ErrConflict
	}
	if len(wire) > 512 {
		return nil, ErrInvalidSignature
	}
	raw, err := hexutil.Decode(wire)
	var tx types.Transaction
	if err != nil || tx.UnmarshalBinary(raw) != nil || tx.Type() != types.DynamicFeeTxType || tx.ChainId().Cmp(unsigned.ChainId()) != 0 || baseSigner.Hash(&tx) != baseSigner.Hash(unsigned) {
		return nil, ErrInvalidSignature
	}
	sender, err := types.Sender(baseSigner, &tx)
	if err != nil || sender != common.HexToAddress(row.Sender) {
		return nil, ErrInvalidSignature
	}
	return &tx, nil
}
func (s *Base) Bind(ctx context.Context, key, wire string) (*model.BasePayout, error) {
	row, err := s.Find(ctx, key)
	if err != nil {
		return nil, err
	}
	tx, err := baseSigned(row, wire)
	if err != nil {
		return nil, err
	}
	wire = strings.ToLower(wire)
	if row.State == Prepared {
		_, err = s.conn.ExecCtx(ctx, "UPDATE base_payout SET signed_tx=?,tx_hash=?,state=?,updated_at=UTC_TIMESTAMP(6) WHERE id=? AND state=?", wire, tx.Hash().Hex(), Signed, row.Id, Prepared)
		if err != nil {
			return nil, err
		}
		row, err = s.Find(ctx, key)
		if err != nil {
			return nil, err
		}
	}
	if row.SignedTx != wire || row.TxHash != tx.Hash().Hex() {
		return nil, ErrConflict
	}
	return row, nil
}
func (s *Base) Step(ctx context.Context, key string) (*model.BasePayout, error) {
	return s.step(ctx, key, nil)
}

func (s *Base) step(ctx context.Context, key string, claim *recoveryClaim) (*model.BasePayout, error) {
	row, err := s.Find(ctx, key)
	if err != nil {
		return nil, err
	}
	if row.State == Prepared {
		return row, ErrInvalidSignature
	}
	tx, err := baseSigned(row, row.SignedTx)
	if err != nil {
		return row, err
	}
	if tx.Hash().Hex() != row.TxHash {
		return row, ErrConflict
	}
	if row.State == BaseConfirmed {
		return row, nil
	}
	var logIndex sql.NullInt64
	if row.AssetId == baseusdc.AssetID {
		var stored chainreceipt.Stored
		stored, err = s.receipts.RecordUSDCPayout(ctx, row.TxHash, s.sender, row.Recipient, row.TokenAtoms)
		if err == nil {
			logIndex = sql.NullInt64{Valid: true, Int64: int64(stored.Receipt.Event.Instruction)}
		}
	} else {
		_, err = s.receipts.RecordNativePayout(ctx, row.TxHash, s.sender, row.Recipient, row.WeiAmount)
	}
	if err == nil {
		_, err = s.conn.ExecCtx(ctx, "UPDATE base_payout SET state=?,transfer_log_index=?,recovery_state=?,recovery_lease_until=NULL,recovery_checked_at=UTC_TIMESTAMP(6),recovery_error='',updated_at=UTC_TIMESTAMP(6) WHERE id=? AND state IN (?,?)", BaseConfirmed, logIndex, RecoveryConfirmed, row.Id, Signed, Submitted)
		if err != nil {
			return row, err
		}
		return s.Find(ctx, key)
	}
	if !errors.Is(err, chainreceipt.ErrBaseHashAbsent) {
		return row, err
	}
	nonce, err := s.node.PendingBaseNonce(ctx, s.sender)
	if err != nil {
		return row, err
	}
	if nonce != tx.Nonce() {
		return row, fmt.Errorf("%w: original=%d pending=%d", ErrBaseNonceChanged, tx.Nonce(), nonce)
	}
	submit, err := s.broadcaster(row.AssetId)
	if err != nil {
		return row, err
	}
	// Reserve one attempt before the network call. After a lost response the
	// only possible replay has exactly the same signed bytes, nonce and hash.
	update := "UPDATE base_payout SET attempts=attempts+1,updated_at=UTC_TIMESTAMP(6) WHERE id=? AND state=? AND attempts=? AND recovery_state=?"
	args := []any{row.Id, row.State, row.Attempts}
	if claim == nil {
		args = append(args, RecoveryReady)
	} else {
		update += " AND recovery_generation=? AND recovery_lease_until>UTC_TIMESTAMP(6)"
		args = append(args, RecoveryRunning, claim.Generation)
	}
	res, err := s.conn.ExecCtx(ctx, update, args...)
	if err != nil {
		return row, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return row, err
	}
	if n == 0 {
		current, err := s.Find(ctx, key)
		if err == nil && current.State != BaseConfirmed {
			if claim != nil && (current.RecoveryState != RecoveryRunning || current.RecoveryGeneration != claim.Generation) {
				return current, ErrRecoveryLeaseLost
			}
			if claim == nil && current.RecoveryState != RecoveryReady {
				return current, fmt.Errorf("Base replay unavailable while recovery is %s", current.RecoveryState)
			}
		}
		return current, err
	}
	row.Attempts++
	if err = submit(ctx, tx); err != nil {
		return row, fmt.Errorf("%w: %w", ErrOutcomeUnknown, err)
	}
	_, err = s.conn.ExecCtx(ctx, "UPDATE base_payout SET state=?,updated_at=UTC_TIMESTAMP(6) WHERE id=? AND state=?", Submitted, row.Id, Signed)
	if err != nil {
		return row, err
	}
	return s.Find(ctx, key)
}
