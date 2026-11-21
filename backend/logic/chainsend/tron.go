package chainsend

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/internal/tronnative"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainreceipt"
	model "github.com/0xabcd-k/DexWallet/backend/model/chainsend"
	"github.com/go-sql-driver/mysql"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

const TRONConfirmed = "confirmed"

var ErrTRONExpired = errors.New("original TRON authorization expired; continue reconciling its txID")

type TRONNode interface {
	NileHead(context.Context) (tronnative.Head, error)
	NileTransactionSeen(context.Context, string) (bool, error)
	SubmitNileTRX(context.Context, tronnative.Signed) error
}
type tronTokenNode interface {
	NileUSDTContract() string
	SubmitNileUSDT(context.Context, tronnative.Signed) error
}

func (s *TRON) tokenNode(contract string) (tronTokenNode, error) {
	n, ok := s.node.(tronTokenNode)
	if !ok || contract == "" || n.NileUSDTContract() != contract {
		return nil, fmt.Errorf("%w: Nile USDT contract differs from configured asset", ErrInvalidRequest)
	}
	return n, nil
}

type TRON struct {
	conn     sqlx.SqlConn
	rows     model.TronPayoutModel
	sender   string
	node     TRONNode
	receipts *chainreceipt.TRONService
}

func NewTRON(conn sqlx.SqlConn, sender string, node TRONNode, receipts *chainreceipt.TRONService) (*TRON, error) {
	if _, err := tronnative.AddressBytes(sender); err != nil || conn == nil || receipts == nil || node == nil {
		return nil, ErrInvalidRequest
	}
	return &TRON{conn, model.NewTronPayoutModel(conn), sender, node, receipts}, nil
}
func (s *TRON) Find(ctx context.Context, key string) (*model.TronPayout, error) {
	if !requestKey.MatchString(key) {
		return nil, ErrInvalidRequest
	}
	row, err := s.rows.FindOneByRequestKey(ctx, key)
	if err == nil && (row.Network != chainassets.TRONNile || row.Sender != s.sender) {
		return nil, ErrConflict
	}
	return row, err
}
func tronTransfer(p *model.TronPayout) tronnative.Transfer {
	return tronnative.Transfer{
		Sender: p.Sender, Recipient: p.Recipient, Sun: p.Sun,
		TokenContract: p.TokenContract, TokenAtoms: p.TokenAtoms, FeeLimitSun: p.FeeLimitSun,
		Reference:  tronnative.Head{ID: p.RefBlockId, Height: p.RefHeight, Timestamp: p.TimestampMs},
		Expiration: p.ExpirationMs,
	}
}
func (s *TRON) Prepare(ctx context.Context, key, recipient string, sun int64) (*model.TronPayout, error) {
	if sun <= 0 {
		return nil, ErrInvalidRequest
	}
	return s.prepare(ctx, key, model.TronPayout{Recipient: recipient, AssetId: chainassets.TRONNile + ":trx", Sun: sun})
}

func (s *TRON) PrepareUSDT(ctx context.Context, key, recipient, contract, atoms string, feeLimitSun int64) (*model.TronPayout, error) {
	if _, err := tronnative.AddressBytes(contract); err != nil || feeLimitSun <= 0 {
		return nil, ErrInvalidRequest
	}
	if _, err := tronnative.TransferData(recipient, atoms); err != nil {
		return nil, ErrInvalidRequest
	}
	return s.prepare(ctx, key, model.TronPayout{Recipient: recipient, AssetId: chainassets.NileUSDT(contract).ID, TokenContract: contract, TokenAtoms: atoms, FeeLimitSun: feeLimitSun})
}

func (s *TRON) prepare(ctx context.Context, key string, payment model.TronPayout) (*model.TronPayout, error) {
	if _, err := tronnative.AddressBytes(payment.Recipient); err != nil || payment.Recipient == s.sender {
		return nil, ErrInvalidRequest
	}
	existing := func() (*model.TronPayout, error) {
		row, err := s.Find(ctx, key)
		if err == nil && (row.Recipient != payment.Recipient || row.Sun != payment.Sun ||
			row.AssetId != payment.AssetId || row.TokenContract != payment.TokenContract ||
			row.TokenAtoms != payment.TokenAtoms || row.FeeLimitSun != payment.FeeLimitSun) {
			return nil, ErrConflict
		}
		return row, err
	}
	if row, err := existing(); !errors.Is(err, model.ErrNotFound) {
		return row, err
	}
	if payment.TokenContract != "" {
		if _, err := s.tokenNode(payment.TokenContract); err != nil {
			return nil, err
		}
	}
	ref, err := s.node.NileHead(ctx)
	if err != nil {
		return nil, err
	}
	row := &payment
	row.RequestKey, row.Network, row.Sender, row.State = key, chainassets.TRONNile, s.sender, Prepared
	row.RefBlockId, row.RefHeight, row.TimestampMs = ref.ID, ref.Height, ref.Timestamp
	row.ExpirationMs = ref.Timestamp + tronnative.Lifetime.Milliseconds()
	raw, err := tronTransfer(row).Raw()
	if err != nil {
		return nil, err
	}
	row.RawHex, row.TxId = hex.EncodeToString(raw), tronnative.TxID(raw)
	_, err = s.rows.Insert(ctx, row)
	var duplicate *mysql.MySQLError
	if errors.As(err, &duplicate) && duplicate.Number == 1062 {
		row, err = existing()
		if errors.Is(err, model.ErrNotFound) {
			return nil, fmt.Errorf("%w: TRON transaction already reserved by another request", ErrConflict)
		}
		return row, err
	}
	if err != nil {
		return nil, err
	}
	return s.Find(ctx, key)
}
func tronSigned(row *model.TronPayout, signature string) (tronnative.Signed, error) {
	signed, err := tronTransfer(row).Bind(row.RawHex, row.TxId, signature)
	if err != nil {
		return signed, fmt.Errorf("%w: %w", ErrInvalidSignature, err)
	}
	return signed, nil
}
func (s *TRON) Bind(ctx context.Context, key, signature string) (*model.TronPayout, error) {
	row, err := s.Find(ctx, key)
	if err != nil {
		return nil, err
	}
	if _, err = tronSigned(row, signature); err != nil {
		return nil, err
	}
	signature = strings.ToLower(signature)
	if row.State == Prepared {
		if row.TokenContract != "" {
			if _, err = s.tokenNode(row.TokenContract); err != nil {
				return nil, err
			}
		}
		_, err = s.conn.ExecCtx(ctx, "UPDATE tron_payout SET signature=?,state=?,updated_at=UTC_TIMESTAMP(6) WHERE id=? AND state=?", signature, Signed, row.Id, Prepared)
		if err != nil {
			return nil, err
		}
		row, err = s.Find(ctx, key)
		if err != nil {
			return nil, err
		}
	}
	if row.Signature != signature {
		return nil, ErrConflict
	}
	return row, nil
}

// The event index is only meaningful for token transfers.
func (s *TRON) recordPayout(ctx context.Context, row *model.TronPayout) (sql.NullInt64, error) {
	if row.TokenContract == "" {
		_, err := s.receipts.RecordNativePayout(ctx, row.TxId, row.Sender, row.Recipient, row.Sun, row.Signature)
		return sql.NullInt64{}, err
	}
	stored, err := s.receipts.RecordUSDTPayout(ctx, row.TxId, row.Sender, row.Recipient, row.TokenContract, row.TokenAtoms, row.Signature)
	if err != nil {
		return sql.NullInt64{}, err
	}
	return sql.NullInt64{Int64: int64(stored.Receipt.Event.Instruction), Valid: true}, nil
}

func (s *TRON) Step(ctx context.Context, key string) (*model.TronPayout, error) {
	row, err := s.Find(ctx, key)
	if err != nil {
		return nil, err
	}
	if row.State == Prepared {
		return row, ErrInvalidSignature
	}
	signed, err := tronSigned(row, row.Signature)
	if err != nil {
		return row, err
	}
	if row.State == TRONConfirmed {
		return row, nil
	}
	logIndex, err := s.recordPayout(ctx, row)
	if err == nil {
		_, err = s.conn.ExecCtx(ctx, "UPDATE tron_payout SET state=?,transfer_log_index=?,updated_at=UTC_TIMESTAMP(6) WHERE id=? AND state IN (?,?)", TRONConfirmed, logIndex, row.Id, Signed, Submitted)
		if err != nil {
			return row, err
		}
		return s.Find(ctx, key)
	}
	if !errors.Is(err, chainreceipt.ErrTRONSolidAbsent) {
		return row, err
	}
	seen, err := s.node.NileTransactionSeen(ctx, row.TxId)
	if err != nil {
		return row, err
	}
	if seen {
		return row, chainreceipt.ErrUnconfirmed
	}
	if signed.Expired(time.Now()) {
		return row, ErrTRONExpired
	}
	head, err := s.node.NileHead(ctx)
	if err != nil {
		return row, err
	}
	if !head.Valid() || head.Height < row.RefHeight || head.Timestamp < row.TimestampMs {
		return row, fmt.Errorf("TRON head behind original transaction reference")
	}
	if head.Height == row.RefHeight && head.ID != row.RefBlockId {
		return row, fmt.Errorf("Nile head conflicts with original reference block")
	}
	if head.Timestamp >= row.ExpirationMs || signed.Expired(time.Now()) {
		return row, ErrTRONExpired
	}
	if row.State == Submitted {
		return row, chainreceipt.ErrUnconfirmed
	}
	submit := s.node.SubmitNileTRX
	if row.TokenContract != "" {
		node, err := s.tokenNode(row.TokenContract)
		if err != nil {
			return row, err
		}
		submit = node.SubmitNileUSDT
	}
	// Record admission before network I/O. Only the exact same signed bytes may
	// be replayed after an ambiguous acknowledgement.
	res, err := s.conn.ExecCtx(ctx, "UPDATE tron_payout SET attempts=attempts+1,updated_at=UTC_TIMESTAMP(6) WHERE id=? AND state=? AND attempts=?", row.Id, Signed, row.Attempts)
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
	if err = submit(ctx, signed); err != nil {
		return row, fmt.Errorf("%w: %w", ErrOutcomeUnknown, err)
	}
	_, err = s.conn.ExecCtx(ctx, "UPDATE tron_payout SET state=?,updated_at=UTC_TIMESTAMP(6) WHERE id=? AND state=?", Submitted, row.Id, Signed)
	if err != nil {
		return row, err
	}
	return s.Find(ctx, key)
}
