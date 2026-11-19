package chainsend

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"

	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainreceipt"
	model "github.com/0xabcd-k/DexWallet/backend/model/chainsend"
	"github.com/go-sql-driver/mysql"
	"github.com/mr-tron/base58"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

const SolanaConfirmed = "confirmed"

var ErrSolanaIntent = errors.New("invalid Solana payout intent")
var ErrSolanaExpired = errors.New("Solana blockhash expired; reconcile original signature")
var ErrSolanaSignature = errors.New("invalid Solana payout signature")

type SolanaNode interface {
	LatestSolanaBlock(context.Context) (chainassets.SolanaBlock, error)
	SolanaBlockHeight(context.Context) (int64, error)
	SubmitSolana(context.Context, []byte) error
}
type Solana struct {
	conn     sqlx.SqlConn
	rows     model.SolanaPayoutModel
	sender   string
	node     SolanaNode
	receipts *chainreceipt.Service
}

func solanaKey(s string) ([]byte, error) {
	if len(s) > 44 || s == chainreceipt.SystemProgram {
		return nil, ErrSolanaIntent
	}
	raw, err := base58.Decode(s)
	if err != nil || len(raw) != 32 {
		return nil, ErrSolanaIntent
	}
	return raw, nil
}
func NewSolana(conn sqlx.SqlConn, sender string, node SolanaNode, receipts *chainreceipt.Service) (*Solana, error) {
	if _, err := solanaKey(sender); err != nil {
		return nil, err
	}
	if conn == nil || node == nil || receipts == nil {
		return nil, ErrSolanaIntent
	}
	return &Solana{conn, model.NewSolanaPayoutModel(conn), sender, node, receipts}, nil
}

// SolanaPayoutMessage builds the single supported legacy SystemProgram transfer.
// All compact lengths here are fixed single-byte values. Amounts use little-endian u64.
func SolanaPayoutMessage(sender, recipient, lamports, blockhash string) ([]byte, error) {
	payer, err := solanaKey(sender)
	if err != nil {
		return nil, err
	}
	dest, err := solanaKey(recipient)
	if err != nil {
		return nil, err
	}
	hash, err := solanaKey(blockhash)
	if err != nil {
		return nil, err
	}
	n, err := strconv.ParseUint(lamports, 10, 64)
	if err != nil || n == 0 || strconv.FormatUint(n, 10) != lamports || sender == recipient {
		return nil, ErrSolanaIntent
	}
	// Header: one writable signer, one read-only unsigned program, three account keys.
	msg := []byte{1, 0, 1, 3}
	msg = append(msg, payer...)
	msg = append(msg, dest...)
	msg = append(msg, make([]byte, 32)...)
	msg = append(msg, hash...)
	// One instruction, program #2, accounts #0 and #1, 12 data bytes.
	msg = append(msg, 1, 2, 2, 0, 1, 12)
	msg = binary.LittleEndian.AppendUint32(msg, 2) // SystemInstruction::Transfer
	return binary.LittleEndian.AppendUint64(msg, n), nil
}
func solanaDigest(msg []byte) string { sum := sha256.Sum256(msg); return hex.EncodeToString(sum[:]) }

func (s *Solana) Find(ctx context.Context, key string) (*model.SolanaPayout, error) {
	if !requestKey.MatchString(key) {
		return nil, ErrSolanaIntent
	}
	row, err := s.rows.FindOneByRequestKey(ctx, key)
	if err != nil {
		return nil, err
	}
	if row.Sender != s.sender {
		return nil, ErrConflict
	}
	return row, nil
}
func (s *Solana) Prepare(ctx context.Context, key, recipient, lamports string) (*model.SolanaPayout, error) {
	existing := func() (*model.SolanaPayout, error) {
		row, err := s.Find(ctx, key)
		if err == nil && (row.Recipient != recipient || row.Lamports != lamports) {
			return nil, ErrConflict
		}
		return row, err
	}
	row, err := existing()
	if !errors.Is(err, model.ErrNotFound) {
		return row, err
	}
	block, err := s.node.LatestSolanaBlock(ctx)
	if err != nil {
		return nil, err
	}
	msg, err := SolanaPayoutMessage(s.sender, recipient, lamports, block.Blockhash)
	if err != nil {
		return nil, err
	}
	if block.LastValidBlockHeight <= 0 {
		return nil, ErrSolanaIntent
	}
	row = &model.SolanaPayout{RequestKey: key, Sender: s.sender, Recipient: recipient, Lamports: lamports, Blockhash: block.Blockhash, LastValidHeight: block.LastValidBlockHeight, MessageHash: solanaDigest(msg), State: Prepared}
	_, err = s.rows.Insert(ctx, row)
	var duplicate *mysql.MySQLError
	if errors.As(err, &duplicate) && duplicate.Number == 1062 {
		row, err = existing()
		if errors.Is(err, model.ErrNotFound) {
			return nil, fmt.Errorf("%w: same Solana message reserved for another key", ErrConflict)
		}
		return row, err
	}
	if err != nil {
		return nil, err
	}
	return s.Find(ctx, key)
}
func (s *Solana) signedWire(row *model.SolanaPayout, signature string) ([]byte, error) {
	msg, err := SolanaPayoutMessage(s.sender, row.Recipient, row.Lamports, row.Blockhash)
	if err != nil {
		return nil, err
	}
	if solanaDigest(msg) != row.MessageHash {
		return nil, ErrConflict
	}
	if len(signature) > 88 {
		return nil, ErrSolanaSignature
	}
	sig, err := base58.Decode(signature)
	payer, _ := solanaKey(s.sender)
	if err != nil || len(sig) != ed25519.SignatureSize || !ed25519.Verify(payer, msg, sig) {
		return nil, ErrSolanaSignature
	}
	return append(append([]byte{1}, sig...), msg...), nil
}

// Bind verifies an external signer's result. Only a committed authorization may enter Step.
func (s *Solana) Bind(ctx context.Context, key, signature string) (*model.SolanaPayout, error) {
	row, err := s.Find(ctx, key)
	if err != nil {
		return nil, err
	}
	if _, err = s.signedWire(row, signature); err != nil {
		return nil, err
	}
	if row.State == Prepared {
		_, err = s.conn.ExecCtx(ctx, "UPDATE solana_payout SET signature=?,state=?,updated_at=UTC_TIMESTAMP(6) WHERE id=? AND state=?", signature, Signed, row.Id, Prepared)
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

// Step always observes first. A receipt commit can be recovered independently
// of this row's terminal marker. RPC never runs inside a SQL transaction.
func (s *Solana) Step(ctx context.Context, key string) (*model.SolanaPayout, error) {
	row, err := s.Find(ctx, key)
	if err != nil {
		return nil, err
	}
	if row.State == Prepared {
		return row, ErrSolanaSignature
	}
	wire, err := s.signedWire(row, row.Signature)
	if err != nil {
		return row, err
	}
	if row.State == SolanaConfirmed {
		return row, nil
	}
	req := chainreceipt.Request{Network: chainassets.SolanaDevnet, AssetID: chainassets.SolanaDevnet + ":sol", Signature: row.Signature, Recipient: row.Recipient, Atoms: row.Lamports}
	_, err = s.receipts.RecordSolanaPayout(ctx, req, s.sender, row.Blockhash)
	if err == nil {
		_, err = s.conn.ExecCtx(ctx, "UPDATE solana_payout SET state=?,updated_at=UTC_TIMESTAMP(6) WHERE id=? AND state IN (?,?)", SolanaConfirmed, row.Id, Signed, Submitted)
		if err != nil {
			return row, err
		}
		return s.Find(ctx, key)
	}
	if !errors.Is(err, chainreceipt.ErrSolanaSignatureAbsent) {
		return row, err
	}
	if row.State == Submitted {
		return row, chainreceipt.ErrUnconfirmed
	}
	height, err := s.node.SolanaBlockHeight(ctx)
	if err != nil {
		return row, err
	}
	if height <= 0 {
		return row, ErrSolanaIntent
	}
	if height > row.LastValidHeight {
		return row, ErrSolanaExpired
	}
	res, err := s.conn.ExecCtx(ctx, "UPDATE solana_payout SET attempts=attempts+1,updated_at=UTC_TIMESTAMP(6) WHERE id=? AND state=? AND attempts=?", row.Id, Signed, row.Attempts)
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
	if err = s.node.SubmitSolana(ctx, wire); err != nil {
		return row, fmt.Errorf("%w: %w", ErrOutcomeUnknown, err)
	}
	_, err = s.conn.ExecCtx(ctx, "UPDATE solana_payout SET state=?,updated_at=UTC_TIMESTAMP(6) WHERE id=? AND state=?", Submitted, row.Id, Signed)
	if err != nil {
		return row, err
	}
	return s.Find(ctx, key)
}
