package chainreceipt

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
	"github.com/0xabcd-k/DexWallet/backend/transaction"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

type TRONHistorySource interface {
	Catalog() []chainassets.Network
	ReadNileHistory(context.Context, int64, bool) (chainassets.NileHistory, error)
}
type TRONListenerConfig struct {
	AnchorHeight     int64
	PollInterval     time.Duration `json:",default=5s"`
	OperationTimeout time.Duration `json:",default=20s"`
}

func (c TRONListenerConfig) Check() error {
	if c.AnchorHeight <= 0 || c.PollInterval < time.Second || c.OperationTimeout <= 0 {
		return fmt.Errorf("Nile listener requires a positive solid anchor height and operation timeout, poll >= 1s")
	}
	return nil
}

type TRONListener struct {
	db     *sql.DB
	tx     *transaction.Runner
	source TRONHistorySource
	assets map[string]chainassets.Asset
}

func NewTRONListener(db *sql.DB, source TRONHistorySource) *TRONListener {
	s := &TRONListener{db: db, tx: transaction.NewWithIsolation(db, sql.LevelReadCommitted), source: source, assets: make(map[string]chainassets.Asset)}
	for _, n := range source.Catalog() {
		if n.ID == chainassets.TRONNile && n.Enabled {
			for _, a := range n.Assets {
				s.assets[a.ID] = a
			}
		}
	}
	return s
}
func (s *TRONListener) Start(ctx context.Context, c TRONListenerConfig) (*ListenerRuntime, error) {
	if err := c.Check(); err != nil {
		return nil, err
	}
	if len(s.assets) == 0 {
		return nil, fmt.Errorf("Nile listener requires an enabled Nile network")
	}
	if err := listenerSchema(ctx, s.db, 36); err != nil {
		return nil, err
	}
	var polls []listenerPoll
	for id := range s.assets {
		polls = append(polls, func(ctx context.Context) (bool, error) {
			busy, err := s.Scan(ctx, id, c.AnchorHeight)
			if err != nil {
				return false, fmt.Errorf("%s: %w", id, err)
			}
			return busy, nil
		})
	}
	return runListener("TRON Nile", ListenerConfig{PollInterval: c.PollInterval, OperationTimeout: c.OperationTimeout}, polls...), nil
}

// All evidence is available from the solid block and its bulk receipt list.
// Verify before the SQL transaction, then commit the block's facts, matches
// and checkpoint together. An unknown commit resumes from the stored cursor.
func (s *TRONListener) Scan(ctx context.Context, id string, anchor int64) (bool, error) {
	asset, ok := s.assets[id]
	if !ok || anchor <= 0 {
		return false, ErrInvalidRequest
	}
	var first sql.NullInt64
	var now int64
	if err := s.db.QueryRowContext(ctx, "SELECT MIN(opens_at),UNIX_TIMESTAMP() FROM chain_receive_request WHERE asset_id=?", id).Scan(&first, &now); err != nil || !first.Valid {
		return false, err
	}
	var last int64
	var parent string
	err := s.db.QueryRowContext(ctx, "SELECT height,block_hash FROM tron_receive_cursor WHERE asset_id=?", id).Scan(&last, &parent)
	initial := errors.Is(err, sql.ErrNoRows)
	if err != nil && !initial {
		return false, err
	}
	next := last + 1
	if initial {
		next = anchor
	}
	h, err := s.source.ReadNileHistory(ctx, next, !initial)
	if err != nil {
		return false, fmt.Errorf("read Nile block %d: %w", next, err)
	}
	var genesis tronBlock
	if json.Unmarshal(h.Genesis, &genesis) != nil || !genesis.valid() || genesis.ID != chainassets.TRONNileGenesis {
		return false, bad("wrong Nile genesis")
	}
	head, err := nileHistoryBlock(h.SolidHead)
	if err != nil {
		return false, err
	}
	solid := int64(head.Header.Raw.Number)
	if next > solid {
		if initial {
			return false, fmt.Errorf("Nile anchor %d exceeds solid height %d", anchor, solid)
		}
		if last != solid || parent != head.ID {
			return false, bad("Nile solid head behind or inconsistent with cursor")
		}
		return false, nil
	}
	b, err := nileHistoryBlock(h.Block)
	if err != nil {
		return false, fmt.Errorf("Nile block %d: %w", next, err)
	}
	at := int64(b.Header.Raw.Timestamp) / 1000
	if int64(b.Header.Raw.Number) != next || next == solid && b.ID != head.ID || !initial && b.Header.Raw.ParentHash != parent || at > now {
		return false, fmt.Errorf("Nile height %d after %s, got %s with parent %s: %w", next, parent, b.ID, b.Header.Raw.ParentHash, bad("inclusion block differs from cursor, solid head or time"))
	}
	if initial {
		if at >= first.Int64 {
			return false, fmt.Errorf("Nile anchor %d must precede the first request for %s", anchor, id)
		}
		_, err = s.db.ExecContext(ctx, `INSERT INTO tron_receive_cursor(asset_id,height,block_hash) VALUES(?,?,?) ON DUPLICATE KEY UPDATE asset_id=tron_receive_cursor.asset_id`, id, next, b.ID)
		return err == nil, err
	}
	events, err := nileIncomingEvents(h, b, asset)
	if err != nil {
		return false, fmt.Errorf("discover Nile block %d: %w", next, err)
	}
	writes, err := s.verifyIssued(ctx, events, asset, at)
	if err != nil {
		return false, err
	}
	advanced, err := s.commit(ctx, asset.ID, last, parent, next, b.ID, writes)
	if err != nil {
		return false, fmt.Errorf("commit Nile block %d %s: %w", next, b.ID, err)
	}
	return advanced, nil
}

func (s *TRONListener) verifyIssued(ctx context.Context, events []nileIncoming, asset chainassets.Asset, at int64) ([]Receipt, error) {
	if len(events) == 0 {
		return nil, nil
	}
	owners := make(map[string]bool)
	for _, v := range events {
		owners[v.req.Recipient] = false
	}
	args := []any{chainassets.TRONNile, asset.ID, at}
	for owner := range owners {
		args = append(args, owner)
	}
	query := `SELECT a.owner FROM chain_receive_address a JOIN chain_receive_request r ON r.address_id=a.id
		WHERE a.network=? AND r.asset_id=? AND r.opens_at<=? AND a.owner IN (` + strings.TrimSuffix(strings.Repeat("?,", len(owners)), ",") + ")"
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var owner string
		if err = rows.Scan(&owner); err != nil {
			return nil, err
		}
		owners[owner] = true
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	var writes []Receipt
	for _, v := range events {
		if !owners[v.req.Recipient] {
			continue
		}
		r, err := verifyTRON(v.req, asset, v.proof)
		if errors.Is(err, ErrExecutionFailed) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("verify Nile tx %s: %w", v.req.Hash, err)
		}
		writes = append(writes, r)
	}
	return writes, nil
}
func (s *TRONListener) commit(ctx context.Context, asset string, last int64, parent string, next int64, hash string, writes []Receipt) (bool, error) {
	advanced := false
	err := s.tx.TransactCtx(ctx, func(ctx context.Context, session sqlx.Session) error {
		res, err := session.ExecCtx(ctx, "UPDATE tron_receive_cursor SET height=?,block_hash=? WHERE asset_id=? AND height=? AND block_hash=?", next, hash, asset, last, parent)
		if err != nil {
			return err
		}
		if n, e := res.RowsAffected(); e != nil || n == 0 {
			return e
		}
		conn := sqlx.NewSqlConnFromSession(session)
		attribution := NewAttribution(conn, nil)
		var previous string
		var raw []byte
		for _, r := range writes {
			if previous != r.Transaction.Signature {
				raw, err = json.Marshal(r.evidence)
				if err != nil {
					return err
				}
				previous = r.Transaction.Signature
			}
			if _, err = storeReceipt(ctx, conn, r, raw); err != nil {
				return err
			}
			key := EventKey{Network: chainassets.TRONNile, Signature: r.Transaction.Signature, Kind: r.Event.Kind, Index: r.Event.Instruction}
			if _, err = attribution.Match(ctx, key); err != nil && !errors.Is(err, ErrReceiveMismatch) && !errors.Is(err, ErrReceiveWindow) && !errors.Is(err, ErrReceiveConflict) {
				return err
			}
		}
		advanced = true
		return nil
	})
	return advanced && err == nil, err
}
