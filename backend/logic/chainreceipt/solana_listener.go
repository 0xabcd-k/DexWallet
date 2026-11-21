package chainreceipt

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
)

type SolanaHistorySource interface {
	Source
	NextSolanaHistorySlots(context.Context, int64) ([]int64, error)
	ReadSolanaHistoryBlock(context.Context, int64, bool) (json.RawMessage, error)
}
type SolanaListener struct {
	db          *sql.DB
	source      SolanaHistorySource
	receipts    *Service
	attribution *Attribution
}

func NewSolanaListener(db *sql.DB, source SolanaHistorySource, receipts *Service, attribution *Attribution) *SolanaListener {
	return &SolanaListener{db, source, receipts, attribution}
}

type solPosition struct {
	slot int64
	hash string
}
type solBlock struct {
	Blockhash, PreviousBlockhash string
	ParentSlot, BlockTime        int64
	Transactions                 *[]struct {
		Version json.RawMessage
		Meta    *struct {
			Err               json.RawMessage
			PostTokenBalances *[]struct{ Owner, Mint, ProgramID string }
		}
		Transaction struct {
			Signatures  []string
			AccountKeys []account
		}
	}
}

func (s *SolanaListener) checkpoint(ctx context.Context) (solPosition, error) {
	var p solPosition
	err := s.db.QueryRowContext(ctx, "SELECT last_slot,last_hash FROM solana_receive_cursor WHERE id=1").Scan(&p.slot, &p.hash)
	return p, err
}

// The operator selects an already-finalized block strictly before the first
// request window. Subsequent starts always use the committed database position.
func (s *SolanaListener) initialize(ctx context.Context, anchor int64) error {
	raw, err := s.source.ReadSolanaHistoryBlock(ctx, anchor, true)
	if err != nil {
		return err
	}
	var b solBlock
	if json.Unmarshal(raw, &b) != nil || b.BlockTime <= 0 || !key(b.Blockhash, 32) {
		return bad("incomplete Solana anchor block")
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO solana_receive_cursor(id,last_slot,last_hash)
		SELECT 1,?,? WHERE ?<=UNIX_TIMESTAMP() AND NOT EXISTS (SELECT 1 FROM chain_receive_request r JOIN chain_receive_address a
		ON a.id=r.address_id WHERE a.network=? AND r.opens_at<=?)
		ON DUPLICATE KEY UPDATE id=solana_receive_cursor.id`, anchor, b.Blockhash, b.BlockTime, chainassets.SolanaDevnet, b.BlockTime)
	if err != nil {
		return err
	}
	_, err = s.checkpoint(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("Solana anchor slot %d must precede the earliest issued request", anchor)
	}
	return err
}

// Discover account owners present in a finalized block. Token owners may be
// absent from accountKeys; postTokenBalances binds them to transaction-time holdings.
func solanaBlockOwners(b solBlock) (map[string]map[string]bool, error) {
	byOwner := make(map[string]map[string]bool)
	add := func(owner, signature string) {
		if byOwner[owner] == nil {
			byOwner[owner] = make(map[string]bool)
		}
		byOwner[owner][signature] = true
	}
	for _, tx := range *b.Transactions {
		if tx.Meta == nil || len(tx.Meta.Err) == 0 || len(tx.Transaction.Signatures) == 0 || !key(tx.Transaction.Signatures[0], 64) {
			return nil, bad("missing Solana transaction summary")
		}
		if string(tx.Version) != `"legacy"` && string(tx.Version) != "0" {
			var v uint64
			if len(tx.Version) == 0 || null(tx.Version) || json.Unmarshal(tx.Version, &v) != nil {
				return nil, bad("missing Solana summary version")
			}
			continue // only legacy and version 0 currently have a receipt verifier
		}
		if !null(tx.Meta.Err) {
			if v := strings.TrimSpace(string(tx.Meta.Err)); v == "" || (v[0] != '{' && v[0] != '"') {
				return nil, bad("malformed Solana execution result")
			}
			continue
		}
		if tx.Meta.PostTokenBalances == nil || len(tx.Transaction.AccountKeys) == 0 {
			return nil, bad("incomplete Solana account summary")
		}
		sig := tx.Transaction.Signatures[0]
		for _, k := range tx.Transaction.AccountKeys {
			if !key(k.Pubkey, 32) {
				return nil, bad("invalid Solana summary account")
			}
			add(k.Pubkey, sig)
		}
		for _, v := range *tx.Meta.PostTokenBalances {
			if !key(v.ProgramID, 32) {
				return nil, bad("missing SPL balance program in block")
			}
			if v.ProgramID != TokenProgram {
				continue
			}
			if !key(v.Owner, 32) {
				return nil, bad("missing SPL balance owner in block")
			}
			add(v.Owner, sig)
		}
	}
	return byOwner, nil
}

func (s *SolanaListener) saveBlock(ctx context.Context, previous solPosition, slot int64, b solBlock) (bool, error) {
	if b.ParentSlot != previous.slot || b.PreviousBlockhash != previous.hash || !key(b.Blockhash, 32) || b.BlockTime <= 0 || b.Transactions == nil {
		return false, bad("Solana finalized block missing or disconnected")
	}
	owners, err := solanaBlockOwners(b)
	if err != nil {
		return false, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE solana_receive_cursor SET last_slot=?,last_hash=? WHERE id=1 AND last_slot=? AND last_hash=?`, slot, b.Blockhash, previous.slot, previous.hash)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil || n == 0 {
		return false, err
	}
	if len(owners) > 0 {
		args := []any{chainassets.SolanaDevnet, b.BlockTime}
		for owner := range owners {
			args = append(args, owner)
		}
		query := `SELECT r.id,a.owner FROM chain_receive_address a JOIN chain_receive_request r ON r.address_id=a.id
			WHERE a.network=? AND r.opens_at<=? AND a.owner IN (` + strings.TrimSuffix(strings.Repeat("?,", len(owners)), ",") + ")"
		rows, err := tx.QueryContext(ctx, query, args...)
		if err != nil {
			return false, err
		}
		type target struct {
			id    int64
			owner string
		}
		var targets []target
		for rows.Next() {
			var dest target
			if err = rows.Scan(&dest.id, &dest.owner); err != nil {
				rows.Close()
				return false, err
			}
			targets = append(targets, dest)
		}
		if err = rows.Err(); err != nil {
			rows.Close()
			return false, err
		}
		if err = rows.Close(); err != nil {
			return false, err
		}
		for _, dest := range targets {
			for sig := range owners[dest.owner] {
				_, err = tx.ExecContext(ctx, `INSERT INTO solana_receive_candidate(request_id,signature,slot,block_time,next_try) VALUES (?,?,?,?,UTC_TIMESTAMP(6))`, dest.id, sig, slot, b.BlockTime)
				if err != nil {
					return false, err
				}
			}
		}
	}
	err = tx.Commit()
	return err == nil, err
}

func (s *SolanaListener) Scan(ctx context.Context, anchor int64) (bool, error) {
	p, err := s.checkpoint(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		err = s.initialize(ctx, anchor)
		return err == nil, err
	}
	if err != nil {
		return false, err
	}
	slots, err := s.source.NextSolanaHistorySlots(ctx, p.slot)
	if err != nil {
		return false, err
	}
	if slots == nil || len(slots) > chainassets.SolanaHistoryBatch {
		return false, bad("invalid Solana finalized slot list")
	}
	for _, slot := range slots {
		if slot <= p.slot {
			return false, bad("unordered Solana finalized slots")
		}
		raw, e := s.source.ReadSolanaHistoryBlock(ctx, slot, false)
		if e != nil {
			return false, fmt.Errorf("read Solana block %d: %w", slot, e)
		}
		var b solBlock
		if json.Unmarshal(raw, &b) != nil {
			return false, bad("malformed Solana block")
		}
		saved, e := s.saveBlock(ctx, p, slot, b)
		if e != nil {
			return false, fmt.Errorf("scan Solana block %d after %d: %w", slot, p.slot, e)
		}
		if !saved {
			return false, nil
		}
		p = solPosition{slot, b.Blockhash}
	}
	return len(slots) == chainassets.SolanaHistoryBatch, nil
}

type solWork struct {
	id, attempt, slot, blockTime int64
	signature, owner, assetID    string
}

func (s *SolanaListener) Process(ctx context.Context, retry time.Duration) (bool, error) {
	var w solWork
	err := s.db.QueryRowContext(ctx, `SELECT p.id,p.attempts,p.signature,p.slot,p.block_time,a.owner,r.asset_id
		FROM solana_receive_candidate p JOIN chain_receive_request r ON p.request_id=r.id JOIN chain_receive_address a ON r.address_id=a.id
		WHERE p.status=? AND p.next_try<=UTC_TIMESTAMP(6) ORDER BY p.next_try,p.id LIMIT 1`, listenerPending).Scan(&w.id, &w.attempt, &w.signature, &w.slot, &w.blockTime, &w.owner, &w.assetID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	res, err := s.db.ExecContext(ctx, `UPDATE solana_receive_candidate SET attempts=attempts+1,next_try=TIMESTAMPADD(MICROSECOND,?,UTC_TIMESTAMP(6))
		WHERE id=? AND status=? AND attempts=? AND next_try<=UTC_TIMESTAMP(6)`, retry.Microseconds(), w.id, listenerPending, w.attempt)
	if err != nil {
		return false, err
	}
	if n, e := res.RowsAffected(); n == 0 || e != nil {
		return false, e
	}
	matched, unmatched, reason, workErr := s.processTransfer(ctx, w)
	status := listenerIgnored
	switch {
	case workErr != nil:
		status = listenerPending
		reason = workErr.Error()
	case matched > 0:
		status = listenerMatched
	case unmatched > 0:
		status = listenerUnmatched
	}
	detail := []rune(reason)
	if len(detail) > 512 {
		detail = detail[:512]
	}
	_, err = s.db.ExecContext(ctx, `UPDATE solana_receive_candidate SET status=?,matched_events=?,unmatched_events=?,detail=? WHERE id=? AND attempts=? AND status=?`, status, matched, unmatched, string(detail), w.id, w.attempt+1, listenerPending)
	if workErr != nil {
		workErr = fmt.Errorf("Solana receipt %s at slot %d: %w", w.signature, w.slot, workErr)
	}
	return true, errors.Join(workErr, err)
}

func (s *SolanaListener) processTransfer(ctx context.Context, w solWork) (matched, unmatched int, reason string, err error) {
	var proof chainassets.SolanaEvidence
	var stored string
	err = s.db.QueryRowContext(ctx, "SELECT evidence_json FROM chain_observed_transaction WHERE network=? AND signature=?", chainassets.SolanaDevnet, w.signature).Scan(&stored)
	switch {
	case err == nil:
		err = json.Unmarshal([]byte(stored), &proof)
	case errors.Is(err, sql.ErrNoRows):
		proof, err = s.source.ReadSolanaEvidence(ctx, chainassets.SolanaDevnet, w.signature)
	}
	if err != nil {
		return 0, 0, "", err
	}
	template := Request{Network: chainassets.SolanaDevnet, Signature: w.signature, Recipient: w.owner, AssetID: w.assetID}
	asset, err := s.receipts.verifier.solanaAsset(template)
	if err != nil {
		return 0, 0, "", err
	}
	tx, err := confirmedSolanaTransaction(w.signature, proof)
	if errors.Is(err, ErrUnsupported) {
		return 0, 0, "", bad("receipt version differs from supported block summary")
	}
	if err != nil {
		return 0, 0, "", err
	}
	var clock struct{ BlockTime int64 }
	if json.Unmarshal(proof.Transaction, &clock) != nil || clock.BlockTime != w.blockTime || tx.Slot != w.slot {
		return 0, 0, "", bad("Solana block and transaction position differ")
	}
	ixs := tx.Transaction.Message.Instructions
	if ixs == nil {
		return 0, 0, "", bad("missing Solana instruction list")
	}
	for i, ix := range ixs {
		amount, e := solanaIncomingAmount(ix, tx, asset, w.owner)
		if e != nil {
			return matched, unmatched, "", e
		}
		if amount == "" {
			continue
		}
		req := template
		req.Instruction = i
		req.Atoms = amount
		v, e := verify(req, asset, proof)
		if e != nil {
			return matched, unmatched, "", e
		}
		if _, e = s.receipts.record(ctx, v); e != nil {
			return matched, unmatched, "", e
		}
		_, e = s.attribution.Match(ctx, EventKey{Network: req.Network, Signature: req.Signature, Kind: SolanaInstruction, Index: i})
		switch {
		case e == nil:
			matched++
		case errors.Is(e, ErrNoReceiveRequest), errors.Is(e, ErrReceiveMismatch), errors.Is(e, ErrReceiveConflict), errors.Is(e, ErrReceiveWindow):
			unmatched++
			reason = fmt.Sprintf("instruction %d: %s", i, e)
		default:
			return matched, unmatched, "", e
		}
	}
	return matched, unmatched, reason, nil
}

func solanaIncomingAmount(ix instruction, tx transactionResponse, asset chainassets.Asset, owner string) (string, error) {
	if !key(ix.ProgramID, 32) {
		return "", bad("missing Solana instruction program")
	}
	native := asset.Origin == chainassets.Native
	if (native && ix.ProgramID != SystemProgram) || (!native && ix.ProgramID != TokenProgram) {
		return "", nil
	}
	var p parsedTransfer
	if json.Unmarshal(ix.Parsed, &p) != nil || p.Type == "" {
		return "", bad("malformed parsed Solana instruction")
	}
	info := p.Info
	if native {
		if p.Type != "transfer" || info.Destination != owner || info.Source == owner {
			return "", nil
		}
		if info.Lamports == nil {
			return "", bad("missing native transfer amount")
		}
		if *info.Lamports == 0 {
			return "", nil
		}
		return strconv.FormatUint(*info.Lamports, 10), nil
	}
	if p.Type != "transferChecked" || info.Mint != asset.Contract || info.TokenAmount.Amount == "0" {
		return "", nil
	}
	keys := tx.Transaction.Message.AccountKeys
	destIndex := slices.IndexFunc(keys, func(k account) bool { return k.Pubkey == info.Destination })
	if destIndex < 0 {
		return "", bad("missing SPL destination account")
	}
	dest, err := balanceOwner(tx.Meta.PostTokenBalances, destIndex, len(keys), asset)
	if err != nil || dest != owner {
		return "", err
	}
	sourceIndex := slices.IndexFunc(keys, func(k account) bool { return k.Pubkey == info.Source })
	if sourceIndex < 0 {
		return "", bad("missing SPL source account")
	}
	payer, err := balanceOwner(tx.Meta.PreTokenBalances, sourceIndex, len(keys), asset)
	if err != nil || payer == dest {
		return "", err
	}
	if !atoms(info.TokenAmount.Amount) {
		return "", bad("invalid SPL transfer amount")
	}
	return info.TokenAmount.Amount, nil
}
