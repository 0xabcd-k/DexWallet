package chainreceipt

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
)

const (
	tonHistoryPage            = 8
	tonInternalTransferOpcode = 0x178d4519
	listenerPending           = "pending"
	listenerIgnored           = "ignored"
	listenerUnmatched         = "unmatched"
	listenerMatched           = "matched"
)

type TONHistorySource interface {
	TONSource
	ResolveTONJettonWallet(context.Context, string, string) (string, error)
	ReadTONHistory(context.Context, string, int64, int) (json.RawMessage, error)
}

type TONListener struct {
	db          *sql.DB
	source      TONHistorySource
	receipts    *TONService
	attribution *Attribution
}

func NewTONListener(db *sql.DB, source TONHistorySource, receipts *TONService, attribution *Attribution) *TONListener {
	return &TONListener{db: db, source: source, receipts: receipts, attribution: attribution}
}

func (s *TONListener) asset(id string) (chainassets.Asset, error) {
	for _, n := range s.source.Catalog() {
		if n.ID != chainassets.TONTestnet || !n.Enabled {
			continue
		}
		for _, a := range n.Assets {
			if a.ID == id {
				if a.Contract != "" {
					raw, err := tonAddress(n.ID, a.Contract)
					if err != nil {
						return a, err
					}
					a.Contract = raw
				}
				return a, nil
			}
		}
	}
	return chainassets.Asset{}, fmt.Errorf("TON listener asset not configured: %s", id)
}

type tonHistoryEntry struct {
	LT, Hash string
	PrevLT   string `json:"prev_trans_lt"`
	PrevHash string `json:"prev_trans_hash"`
	Account  string
	Now      int64
	Finality string
	Emulated *bool
}

type tonPosition struct {
	lt   int64
	hash string
}
type tonCandidate struct {
	tonPosition
	at int64
}

func tonHistoryPageEntries(raw json.RawMessage, account string, last tonPosition) ([]tonCandidate, error) {
	var page struct{ Transactions *[]tonHistoryEntry }
	if err := json.Unmarshal(raw, &page); err != nil || page.Transactions == nil || len(*page.Transactions) > tonHistoryPage {
		return nil, bad("invalid TON history page")
	}
	if last.lt == 0 {
		last.hash = strings.Repeat("0", 64)
	}
	var entries []tonCandidate
	for _, r := range *page.Transactions {
		lt, err := strconv.ParseInt(r.LT, 10, 64)
		if err != nil || lt <= last.lt || r.Now <= 0 {
			return nil, bad("invalid TON history position")
		}
		hash, err := tonHash(r.Hash)
		if err != nil {
			return nil, err
		}
		owner, err := tonAddress(chainassets.TONTestnet, r.Account)
		if err != nil || owner != account {
			return nil, bad("wrong TON history account")
		}
		prev, err := tonHash(r.PrevHash)
		if err != nil || prev != last.hash || r.PrevLT != strconv.FormatInt(last.lt, 10) {
			return nil, bad("gap in TON account history")
		}
		if !isFalse(r.Emulated) || r.Finality != Finalized {
			if len(entries) == 0 {
				return nil, ErrUnconfirmed
			}
			break
		}
		last = tonPosition{lt, hash}
		entries = append(entries, tonCandidate{last, r.Now})
	}
	return entries, nil
}

// Scan claims one due account/asset, discovers a page and commits all future
// verification work together with its cursor. No RPC runs inside the transaction.
func (s *TONListener) Scan(ctx context.Context, poll time.Duration) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO ton_receive_cursor (address_id,asset_id,starts_at,next_poll)
		SELECT r.address_id,r.asset_id,MIN(r.opens_at),UTC_TIMESTAMP(6) FROM chain_receive_request r
		JOIN chain_receive_address a ON a.id=r.address_id
		LEFT JOIN ton_receive_cursor c ON c.address_id=a.id AND c.asset_id=r.asset_id
		WHERE a.network=? AND c.id IS NULL GROUP BY r.address_id,r.asset_id ORDER BY r.address_id,r.asset_id LIMIT 1
		ON DUPLICATE KEY UPDATE id=ton_receive_cursor.id`, chainassets.TONTestnet)
	if err != nil {
		return err
	}
	var id, start int64
	var owner, account, assetID string
	var pos tonPosition
	err = s.db.QueryRowContext(ctx, `SELECT c.id,a.owner,c.asset_id,c.starts_at,c.scan_account,c.last_lt,c.last_hash
		FROM ton_receive_cursor c JOIN chain_receive_address a ON c.address_id=a.id
		WHERE c.next_poll<=UTC_TIMESTAMP(6) ORDER BY c.next_poll,c.id LIMIT 1`).Scan(&id, &owner, &assetID, &start, &account, &pos.lt, &pos.hash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, `UPDATE ton_receive_cursor SET next_poll=TIMESTAMPADD(MICROSECOND,?,UTC_TIMESTAMP(6))
		WHERE id=? AND last_lt=? AND next_poll<=UTC_TIMESTAMP(6)`, poll.Microseconds(), id, pos.lt)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil || n == 0 {
		return err
	}
	asset, err := s.asset(assetID)
	if err != nil {
		return err
	}
	if account == "" {
		account = owner
		if asset.Contract != "" {
			account, err = s.source.ResolveTONJettonWallet(ctx, asset.Contract, owner)
			if err != nil {
				return fmt.Errorf("resolve %s for %s: %w", assetID, owner, err)
			}
			_, err = s.db.ExecContext(ctx, "UPDATE ton_receive_cursor SET scan_account=? WHERE id=? AND scan_account=''", account, id)
			return err
		}
	}
	if pos.lt == math.MaxInt64 {
		return bad("TON history LT exhausted")
	}
	page, err := s.source.ReadTONHistory(ctx, account, pos.lt+1, tonHistoryPage)
	if err != nil {
		return fmt.Errorf("TON history %s after %d: %w", account, pos.lt, err)
	}
	entries, err := tonHistoryPageEntries(page, account, pos)
	if err != nil {
		return fmt.Errorf("TON history %s after %d: %w", account, pos.lt, err)
	}
	if len(entries) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	end := entries[len(entries)-1]
	res, err = tx.ExecContext(ctx, `UPDATE ton_receive_cursor SET scan_account=?,last_lt=?,last_hash=?
		WHERE id=? AND last_lt=? AND last_hash=?`, account, end.lt, end.hash, id, pos.lt, pos.hash)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil || n == 0 {
		return err
	}
	for _, entry := range entries {
		if entry.at < start {
			continue
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO ton_receive_candidate(cursor_id,tx_lt,tx_hash,tx_time,next_try) VALUES (?,?,?,?,UTC_TIMESTAMP(6))`, id, entry.lt, entry.hash, entry.at); err != nil {
			return err
		}
	}
	return tx.Commit()
}

type tonWork struct {
	id, attempt int64
	tonCandidate
	asset, owner, account string
}

// Process claims one due candidate. A worker that loses its time slot may replay
// an idempotent receipt but cannot overwrite the new worker's result.
func (s *TONListener) Process(ctx context.Context, retry time.Duration) error {
	var w tonWork
	err := s.db.QueryRowContext(ctx, `SELECT p.id,p.attempts,p.tx_lt,p.tx_hash,p.tx_time,c.asset_id,a.owner,c.scan_account
		FROM ton_receive_candidate p JOIN ton_receive_cursor c ON c.id=p.cursor_id JOIN chain_receive_address a ON a.id=c.address_id
		WHERE p.status=? AND p.next_try<=UTC_TIMESTAMP(6) ORDER BY p.next_try,p.id LIMIT 1`, listenerPending).Scan(&w.id, &w.attempt, &w.lt, &w.hash, &w.at, &w.asset, &w.owner, &w.account)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, `UPDATE ton_receive_candidate SET attempts=attempts+1,next_try=TIMESTAMPADD(MICROSECOND,?,UTC_TIMESTAMP(6))
		WHERE id=? AND status=? AND attempts=? AND next_try<=UTC_TIMESTAMP(6)`, retry.Microseconds(), w.id, listenerPending, w.attempt)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil || n == 0 {
		return err
	}
	status, reason := s.processTONReceipt(ctx, w)
	detail := ""
	if reason != nil {
		detail = reason.Error()
		if len(detail) > 512 {
			detail = detail[:512]
		}
		// Provider/driver diagnostics may contain non-ASCII text.
		detail = strings.Map(func(r rune) rune {
			if r < 32 || r > 126 {
				return '?'
			}
			return r
		}, detail)
	}
	_, err = s.db.ExecContext(ctx, `UPDATE ton_receive_candidate SET status=?,detail=? WHERE id=? AND status=? AND attempts=?`, status, detail, w.id, listenerPending, w.attempt+1)
	if status == listenerPending && reason != nil {
		return errors.Join(fmt.Errorf("TON candidate %s/%s: %w", w.asset, w.hash, reason), err)
	}
	return err
}

func (s *TONListener) processTONReceipt(ctx context.Context, w tonWork) (string, error) {
	asset, err := s.asset(w.asset)
	if err != nil {
		return listenerPending, err
	}
	kind := TONIncoming
	if asset.Contract != "" {
		kind = TONJetton
	}
	key := EventKey{Network: chainassets.TONTestnet, Signature: w.hash, Kind: kind}
	// If a previous attempt committed the receipt, an RPC outage cannot prevent attribution.
	stored, err := s.receipts.Find(ctx, key.Network, w.hash, kind)
	if errors.Is(err, sql.ErrNoRows) {
		proof, e := s.source.ReadTONEvidence(ctx, key.Network, w.hash, asset.Contract, w.owner)
		if e != nil {
			return listenerPending, e
		}
		r, e := discoveredTONRequest(w, kind, proof)
		if e == nil {
			var v Receipt
			v, e = verifyTON(r, asset, proof)
			if e == nil {
				_, e = s.receipts.store.record(ctx, v)
			}
		}
		switch {
		case errors.Is(e, ErrUnsupported), errors.Is(e, ErrExecutionFailed):
			return listenerIgnored, e
		case e != nil:
			return listenerPending, e
		}
	} else if err != nil {
		return listenerPending, err
	} else if stored.Event.AssetID != asset.ID || stored.Event.RecipientOwner != w.owner || stored.Event.RecipientAccount != w.account {
		return listenerPending, ErrConflict
	}
	_, err = s.attribution.Match(ctx, key)
	switch {
	case err == nil:
		return listenerMatched, nil
	case errors.Is(err, ErrNoReceiveRequest), errors.Is(err, ErrReceiveMismatch), errors.Is(err, ErrReceiveWindow), errors.Is(err, ErrReceiveConflict):
		return listenerUnmatched, err
	default:
		return listenerPending, err
	}
}

func discoveredTONRequest(w tonWork, kind string, e chainassets.TONEvidence) (TONRequest, error) {
	r := TONRequest{Network: chainassets.TONTestnet, Hash: w.hash, AssetID: w.asset, Recipient: w.owner}
	t, err := tonOneTransaction(e.Transaction)
	if err != nil {
		return r, err
	}
	hash, err := tonHash(t.Hash)
	if err != nil || hash != w.hash || t.LT != strconv.FormatInt(w.lt, 10) || t.Now != w.at {
		return r, bad("TON history and receipt position differ")
	}
	owner, err := tonAddress(r.Network, t.Account)
	if err != nil || owner != w.account {
		return r, bad("TON history and receipt account differ")
	}
	if !isFalse(t.Emulated) || t.Finality != Finalized {
		return r, ErrUnconfirmed
	}
	if t.In == nil {
		return r, bad("missing TON incoming message")
	}
	if t.In.Source == "" {
		if err := normalizeTONTransaction(r.Network, &t, true); err != nil {
			return r, err
		}
		return r, ErrUnsupported
	}
	if t.In.Bounced == nil {
		return r, bad("missing TON bounced flag")
	}
	if *t.In.Bounced {
		return r, ErrUnsupported
	}
	body, err := tonBody(t.In)
	if err != nil {
		return r, err
	}
	slice := body.BeginParse()
	if kind == TONIncoming {
		if slice.BitsLeft() != 0 || slice.RefsNum() != 0 {
			op, err := slice.LoadUInt(32)
			if err != nil || op != 0 {
				return r, ErrUnsupported
			}
		}
		r.Atoms = t.In.Value
	} else {
		op, err := slice.LoadUInt(32)
		if err != nil || op != tonInternalTransferOpcode {
			return r, ErrUnsupported
		}
		var transfer tonInternalTransfer
		if err = tonPayload(body, &transfer); err != nil {
			return r, err
		}
		r.Atoms = transfer.Amount.Nano().String()
	}
	if r.Atoms == "0" {
		return r, ErrUnsupported
	}
	return r, nil
}
