package chainsend

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"

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
	value, err := baseUint(row.WeiAmount)
	if err != nil || value.Sign() == 0 {
		return nil, fmt.Errorf("%w: invalid ETH amount in wei", ErrInvalidRequest)
	}
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
	return types.NewTx(&types.DynamicFeeTx{ChainID: big.NewInt(row.ChainId), Nonce: nonce, To: &to, Value: value, Gas: uint64(row.GasLimit), GasFeeCap: fee, GasTipCap: tip}), nil
}

type BaseTransfer struct {
	Recipient string
	WeiAmount string
	GasLimit  int64
	FeeCap    string
	TipCap    string
}

// The caller explicitly approves the gas limit and both EIP-1559 caps. A new
// business key may reserve only the current pending nonce on this test wallet.
func (s *Base) Prepare(ctx context.Context, key string, payment BaseTransfer) (*model.BasePayout, error) {
	recipient, err := baseAddress(payment.Recipient)
	if err != nil {
		return nil, err
	}
	existing := func() (*model.BasePayout, error) {
		row, err := s.Find(ctx, key)
		if err == nil && (row.Recipient != recipient || row.WeiAmount != payment.WeiAmount || row.GasLimit != payment.GasLimit || row.FeeCap != payment.FeeCap || row.TipCap != payment.TipCap) {
			return nil, ErrConflict
		}
		return row, err
	}
	if row, err := existing(); !errors.Is(err, model.ErrNotFound) {
		return row, err
	}
	nonce, err := s.node.PendingBaseNonce(ctx, s.sender)
	if err != nil {
		return nil, err
	}
	row := &model.BasePayout{RequestKey: key, ChainId: chainassets.BaseSepoliaChainID, Sender: s.sender, Recipient: recipient, WeiAmount: payment.WeiAmount, Nonce: strconv.FormatUint(nonce, 10), GasLimit: payment.GasLimit, FeeCap: payment.FeeCap, TipCap: payment.TipCap, State: Prepared}
	tx, err := baseUnsigned(row)
	if err != nil {
		return nil, err
	}
	row.SigningHash = baseSigner.Hash(tx).Hex()
	_, err = s.rows.Insert(ctx, row)
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
	_, err = s.receipts.RecordNativePayout(ctx, row.TxHash, s.sender, row.Recipient, row.WeiAmount)
	if err == nil {
		_, err = s.conn.ExecCtx(ctx, "UPDATE base_payout SET state=?,updated_at=UTC_TIMESTAMP(6) WHERE id=? AND state IN (?,?)", BaseConfirmed, row.Id, Signed, Submitted)
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
	// Reserve one attempt before the network call. After a lost response the
	// only possible replay has exactly the same signed bytes, nonce and hash.
	res, err := s.conn.ExecCtx(ctx, "UPDATE base_payout SET attempts=attempts+1,updated_at=UTC_TIMESTAMP(6) WHERE id=? AND state=? AND attempts=?", row.Id, row.State, row.Attempts)
	if err != nil {
		return row, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return row, err
	}
	if n == 0 {
		return s.Find(ctx, key)
	}
	row.Attempts++
	if err = s.node.SubmitBaseETH(ctx, tx); err != nil {
		return row, fmt.Errorf("%w: %w", ErrOutcomeUnknown, err)
	}
	_, err = s.conn.ExecCtx(ctx, "UPDATE base_payout SET state=?,updated_at=UTC_TIMESTAMP(6) WHERE id=? AND state=?", Submitted, row.Id, Signed)
	if err != nil {
		return row, err
	}
	return s.Find(ctx, key)
}
