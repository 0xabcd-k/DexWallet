package chainreceipt

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/internal/baseusdc"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
)

type BaseHistorySource interface {
	BaseSource
	BaseCallSource
	ReadBaseHistoryHead(context.Context) (json.RawMessage, error)
	ReadBaseHistoryBlock(context.Context, int64) (json.RawMessage, error)
	ReadBaseBlockReceipts(context.Context, int64) (json.RawMessage, error)
	ReadBaseBlockCalls(context.Context, string) (json.RawMessage, error)
}
type BaseListenerConfig struct {
	ListenerConfig
	AnchorHeight int64
}

func (c BaseListenerConfig) Check() error {
	if c.AnchorHeight <= 0 {
		return fmt.Errorf("Base receipt listener requires a positive finalized anchor height")
	}
	return c.ListenerConfig.Check()
}

type BaseListener struct {
	db          *sql.DB
	source      BaseHistorySource
	receipts    *BaseService
	attribution *Attribution
	assets      map[string]chainassets.Asset
}

func NewBaseListener(db *sql.DB, source BaseHistorySource, receipts *BaseService, attribution *Attribution) *BaseListener {
	assets := map[string]chainassets.Asset{}
	for _, n := range source.Catalog() {
		if n.ID == chainassets.BaseSepolia {
			for _, a := range n.Assets {
				assets[a.ID] = a
			}
		}
	}
	return &BaseListener{db, source, receipts, attribution, assets}
}
func (s *BaseListener) Start(ctx context.Context, c BaseListenerConfig) (*ListenerRuntime, error) {
	if err := c.Check(); err != nil {
		return nil, err
	}
	if err := listenerSchema(ctx, s.db, 35); err != nil {
		return nil, err
	}
	return runListener("Base Sepolia", c.ListenerConfig,
		func(ctx context.Context) (bool, error) {
			return s.Scan(ctx, chainassets.BaseSepoliaETH, c.AnchorHeight)
		},
		func(ctx context.Context) (bool, error) { return s.Scan(ctx, baseusdc.AssetID, c.AnchorHeight) },
		func(ctx context.Context) (bool, error) { return s.Process(ctx, c.RetryInterval) }), nil
}

// Each asset has its own finalized chain of checkpoints. A node that cannot
// supply historical call traces leaves ETH behind without losing token logs.
func (s *BaseListener) Scan(ctx context.Context, asset string, anchor int64) (bool, error) {
	if (asset != chainassets.BaseSepoliaETH && asset != baseusdc.AssetID) || anchor <= 0 {
		return false, ErrInvalidRequest
	}
	var last int64
	var parent string
	err := s.db.QueryRowContext(ctx, "SELECT height,block_hash FROM base_receive_cursor WHERE asset_id=?", asset).Scan(&last, &parent)
	initial := errors.Is(err, sql.ErrNoRows)
	if err != nil && !initial {
		return false, err
	}
	if initial {
		var requested bool
		if err = s.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM chain_receive_request WHERE asset_id=?)", asset).Scan(&requested); err != nil || !requested {
			return false, err
		}
	}
	raw, err := s.source.ReadBaseHistoryHead(ctx)
	if err != nil {
		return false, err
	}
	head, err := historyBlock(raw)
	if err != nil {
		return false, err
	}
	next := last + 1
	if initial {
		next = anchor
	}
	if next > head.height {
		if initial {
			return false, fmt.Errorf("Base anchor %d exceeds finalized height %d", anchor, head.height)
		}
		if last > head.height || last == head.height && parent != head.Hash {
			return false, bad("Base finalized head behind or differs from saved cursor")
		}
		return false, nil
	}
	b := head
	if next != head.height {
		raw, err = s.source.ReadBaseHistoryBlock(ctx, next)
		if err != nil {
			return false, err
		}
		b, err = historyBlock(raw)
		if err != nil {
			return false, err
		}
	}
	if b.height != next || !initial && b.ParentHash != parent {
		return false, bad("disconnected Base finalized block")
	}
	if initial {
		_, err = s.db.ExecContext(ctx, `INSERT INTO base_receive_cursor(asset_id,height,block_hash)
			SELECT ?,?,? WHERE ?<=UNIX_TIMESTAMP() AND NOT EXISTS(SELECT 1 FROM chain_receive_request WHERE asset_id=? AND opens_at<=?)
			ON DUPLICATE KEY UPDATE asset_id=base_receive_cursor.asset_id`, asset, next, b.Hash, b.at, asset, b.at)
		if err != nil {
			return false, err
		}
		if err = s.db.QueryRowContext(ctx, "SELECT height FROM base_receive_cursor WHERE asset_id=?", asset).Scan(&last); errors.Is(err, sql.ErrNoRows) {
			return false, fmt.Errorf("Base anchor %d must precede the first %s request and cannot be in the future", anchor, asset)
		}
		return err == nil, err
	}
	var issued, past bool
	err = s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM chain_receive_request WHERE asset_id=? AND opens_at<=?), ?<=UNIX_TIMESTAMP()`, asset, b.at, b.at).Scan(&issued, &past)
	if err != nil {
		return false, err
	}
	if !past {
		return false, bad("Base finalized timestamp in the future")
	}
	var events []baseIncoming
	if issued {
		events, err = s.discover(ctx, asset, b)
		if err != nil {
			return false, fmt.Errorf("discover %s at block %d: %w", asset, next, err)
		}
	}
	return s.save(ctx, asset, last, parent, b, events)
}
func (s *BaseListener) discover(ctx context.Context, asset string, b baseHistoryBlock) ([]baseIncoming, error) {
	receipts, err := s.source.ReadBaseBlockReceipts(ctx, b.height)
	if err != nil {
		return nil, err
	}
	var calls json.RawMessage
	if asset == chainassets.BaseSepoliaETH {
		calls, err = s.source.ReadBaseBlockCalls(ctx, b.Hash)
		if err != nil {
			return nil, err
		}
	}
	return baseIncomingEvents(b, asset, receipts, calls)
}
func (s *BaseListener) save(ctx context.Context, asset string, last int64, parent string, b baseHistoryBlock, events []baseIncoming) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, "UPDATE base_receive_cursor SET height=?,block_hash=? WHERE asset_id=? AND height=? AND block_hash=?", b.height, b.Hash, asset, last, parent)
	if err != nil {
		return false, err
	}
	if n, e := res.RowsAffected(); e != nil || n == 0 {
		return false, e
	}
	for _, v := range events {
		_, err = tx.ExecContext(ctx, `INSERT INTO base_receive_candidate(request_id,tx_hash,event_kind,event_index,amount_atoms,height,block_hash,block_time)
			SELECT r.id,?,?,?,?,?,?,? FROM chain_receive_address a JOIN chain_receive_request r ON r.address_id=a.id
			WHERE a.network=? AND a.owner=? AND r.asset_id=? AND r.opens_at<=?`, v.hash, v.kind, v.index, v.atoms, b.height, b.Hash, b.at, chainassets.BaseSepolia, v.owner, asset, b.at)
		if err != nil {
			return false, err
		}
	}
	err = tx.Commit()
	return err == nil, err
}

type baseWork struct {
	id, attempt, height, at int64
	blockHash, kind         string
	index                   int
	req                     BaseRequest
}

func (s *BaseListener) Process(ctx context.Context, retry time.Duration) (bool, error) {
	var w baseWork
	w.req.Network = chainassets.BaseSepolia
	err := s.db.QueryRowContext(ctx, `SELECT c.id,c.attempts,c.tx_hash,c.event_kind,c.event_index,c.amount_atoms,c.height,c.block_hash,c.block_time,a.owner,r.asset_id
		FROM base_receive_candidate c JOIN chain_receive_request r ON r.id=c.request_id JOIN chain_receive_address a ON a.id=r.address_id
		WHERE c.status=? AND c.next_try<=UTC_TIMESTAMP(6) ORDER BY c.next_try,c.id LIMIT 1`, listenerPending).Scan(&w.id, &w.attempt, &w.req.Hash, &w.kind, &w.index, &w.req.Atoms, &w.height, &w.blockHash, &w.at, &w.req.Recipient, &w.req.AssetID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	res, err := s.db.ExecContext(ctx, `UPDATE base_receive_candidate SET attempts=attempts+1,next_try=TIMESTAMPADD(MICROSECOND,?,UTC_TIMESTAMP(6))
		WHERE id=? AND status=? AND attempts=? AND next_try<=UTC_TIMESTAMP(6)`, retry.Microseconds(), w.id, listenerPending, w.attempt)
	if err != nil {
		return false, err
	}
	if n, e := res.RowsAffected(); e != nil || n == 0 {
		return false, e
	}
	status, workErr := s.checkCandidate(ctx, w)
	detail := ""
	if workErr != nil {
		msg := []rune(workErr.Error())
		if len(msg) > 512 {
			msg = msg[:512]
		}
		detail = string(msg)
	}
	_, err = s.db.ExecContext(ctx, `UPDATE base_receive_candidate SET status=?,detail=? WHERE id=? AND attempts=? AND status=?`, status, detail, w.id, w.attempt+1, listenerPending)
	if status != listenerPending {
		workErr = nil
	}
	if workErr != nil {
		workErr = fmt.Errorf("Base receipt %s %s/%d: %w", w.req.Hash, w.kind, w.index, workErr)
	}
	return true, errors.Join(workErr, err)
}

func (s *BaseListener) checkCandidate(ctx context.Context, w baseWork) (string, error) {
	switch w.kind {
	case EVMLog:
		w.req.LogIndex = &w.index
	case EVMCall:
		w.req.CallIndex = &w.index
	case EVMValue:
	default:
		return listenerPending, ErrInvalidRequest
	}
	proof, err := s.evidence(ctx, w.req)
	if err != nil {
		return listenerPending, err
	}
	asset, ok := s.assets[w.req.AssetID]
	if !ok {
		return listenerPending, ErrInvalidRequest
	}
	found, err := verifyBase(w.req, asset, proof)
	if err != nil {
		return listenerPending, err
	}
	b, err := historyBlock(proof.Block)
	if err != nil {
		return listenerPending, err
	}
	if found.Transaction.BlockHash != w.blockHash || found.Transaction.Slot != w.height || b.at != w.at {
		return listenerPending, bad("Base evidence and discovery block differ")
	}
	if _, err = s.receipts.store.record(ctx, found); err != nil {
		return listenerPending, err
	}
	_, err = s.attribution.Match(ctx, EventKey{Network: w.req.Network, Signature: w.req.Hash, Kind: w.kind, Index: w.index})
	switch {
	case err == nil:
		return listenerMatched, nil
	case errors.Is(err, ErrNoReceiveRequest), errors.Is(err, ErrReceiveMismatch), errors.Is(err, ErrReceiveConflict), errors.Is(err, ErrReceiveWindow):
		return listenerUnmatched, err
	default:
		return listenerPending, err
	}
}
func (s *BaseListener) evidence(ctx context.Context, r BaseRequest) (chainassets.BaseEvidence, error) {
	var proof chainassets.BaseEvidence
	var raw string
	var trace sql.NullString
	err := s.db.QueryRowContext(ctx, "SELECT evidence_json,call_trace_json FROM chain_observed_transaction WHERE network=? AND signature=?", r.Network, r.Hash).Scan(&raw, &trace)
	if err == nil && (r.CallIndex == nil || trace.Valid) {
		err = json.Unmarshal([]byte(raw), &proof)
		if trace.Valid {
			proof.CallTrace = json.RawMessage(trace.String)
		}
		return proof, err
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return proof, err
	}
	if r.CallIndex != nil {
		return s.source.ReadBaseCallEvidence(ctx, r.Network, r.Hash)
	}
	return s.source.ReadBaseEvidence(ctx, r.Network, r.Hash)
}
