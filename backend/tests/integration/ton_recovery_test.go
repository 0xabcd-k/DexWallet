//go:build integration

package integration

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/config"
	"github.com/0xabcd-k/DexWallet/backend/internal/migration"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainreceipt"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainsend"
	sendmodel "github.com/0xabcd-k/DexWallet/backend/model/chainsend"
	"github.com/0xabcd-k/DexWallet/backend/svc"
	"github.com/0xabcd-k/DexWallet/backend/transaction"
	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

type recoveryFixture struct {
	Intent   chainsend.View
	Wallet   chainsend.Wallet
	Receipt  chainreceipt.Receipt
	Evidence chainassets.TONEvidence
}
type recoverySource struct {
	*tonReceiptSource
	lookup func(context.Context) (chainassets.TONEvidence, error)
	calls  atomic.Int64
}

func (s *recoverySource) ReadTONSendEvidence(ctx context.Context, _ string, _ string) (chainassets.TONEvidence, error) {
	s.calls.Add(1)
	if s.lookup != nil {
		return s.lookup(ctx)
	}
	return s.proof, nil
}
func (s *recoverySource) ReadTONJettonSendEvidence(ctx context.Context, _, _, _, _ string) (chainassets.TONEvidence, error) {
	return s.ReadTONSendEvidence(ctx, "", "")
}
func recoverySample(t *testing.T, name string) recoveryFixture {
	t.Helper()
	raw, err := os.ReadFile("../../logic/chainreceipt/testdata/" + name + ".json")
	if err != nil {
		t.Fatal(err)
	}
	var f recoveryFixture
	if err = json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	return f
}
func recoveryConfig(w chainsend.Wallet) chainsend.RecoveryConfig {
	c := chainsend.RecoveryDefaults()
	c.PublicKey = base64.StdEncoding.EncodeToString(w.PublicKey)
	c.SubwalletID = w.SubwalletID
	c.JettonMaster = w.JettonMaster
	return c
}
func recoveryFixtureSource(t *testing.T, f recoveryFixture) *recoverySource {
	t.Helper()
	a, err := chainassets.New(chainassets.Config{Networks: []chainassets.Endpoint{{Network: chainassets.TONTestnet, URL: "http://127.0.0.1:1", TestUSDTContract: f.Wallet.JettonMaster}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	return &recoverySource{tonReceiptSource: &tonReceiptSource{catalog: a.Catalog(), proof: f.Evidence}}
}
func restoreRecoveryFixture(t *testing.T, db *sql.DB, f recoveryFixture) {
	t.Helper()
	raw := f.Evidence.Parent
	if f.Intent.Jetton.Master != "" {
		raw = f.Evidence.Dispatch
	}
	var dispatch struct {
		Transactions []struct {
			In struct {
				Content struct{ Body string } `json:"message_content"`
			} `json:"in_msg"`
		}
	}
	if err := json.Unmarshal(raw, &dispatch); err != nil {
		t.Fatal(err)
	}
	b, err := base64.StdEncoding.DecodeString(dispatch.Transactions[0].In.Content.Body)
	if err != nil {
		t.Fatal(err)
	}
	body, err := cell.FromBOC(b)
	if err != nil {
		t.Fatal(err)
	}
	sender, _ := address.ParseRawAddr(f.Intent.Sender)
	ext, err := tlb.ToCell(&tlb.ExternalMessage{DstAddr: sender, Body: body})
	if err != nil {
		t.Fatal(err)
	}
	j := ""
	if f.Intent.Jetton.Master != "" {
		b, _ := json.Marshal(f.Intent.Jetton)
		j = string(b)
	}
	row := &sendmodel.ChainSendIntent{Network: f.Intent.Network, RequestKey: f.Intent.Key, Sender: f.Intent.Sender, Recipient: f.Intent.Recipient, AssetId: f.Intent.AssetID, AmountAtoms: f.Intent.Atoms, JettonJson: j, PublicKey: hex.EncodeToString(f.Wallet.PublicKey), SubwalletId: int64(f.Wallet.SubwalletID), Seqno: int64(f.Intent.Seqno), ValidUntil: int64(f.Intent.ValidUntil), PayloadHash: f.Intent.PayloadHash, SignedBoc: sql.NullString{String: base64.StdEncoding.EncodeToString(ext.ToBOC()), Valid: true}, MessageHash: f.Intent.MessageHash, BodyHash: f.Intent.BodyHash, State: chainsend.BroadcastUnknown, Attempts: 1, RecoveryState: chainsend.RecoveryReady, RecoveryNextAt: time.Now().Add(-time.Minute)}
	if _, err = sendmodel.NewChainSendIntentModel(sqlx.NewSqlConnFromDB(db)).Insert(context.Background(), row); err != nil {
		t.Fatal(err)
	}
}
func newRecovery(t *testing.T, db *sql.DB, w chainsend.Wallet, source chainreceipt.TONSource, broadcast chainsend.Broadcaster, c chainsend.RecoveryConfig) *chainsend.Recovery {
	t.Helper()
	executor := tonSender(t, db, transaction.New(db), w, nil, broadcast)
	r, err := chainsend.NewRecovery(db, executor, chainreceipt.NewTON(sqlx.NewSqlConnFromDB(db), transaction.New(db), source), c)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func recoveryRow(t *testing.T, r *chainsend.Recovery, key string) chainsend.RecoveryView {
	t.Helper()
	rows, err := r.List(context.Background(), "", 1000)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.Key == key {
			return row
		}
	}
	t.Fatal("missing recovery", key)
	return chainsend.RecoveryView{}
}
func dueRecovery(t *testing.T, db *sql.DB, key string) {
	t.Helper()
	mustExec(t, db, "UPDATE chain_send_intent SET recovery_next_at=TIMESTAMPADD(SECOND,-1,UTC_TIMESTAMP(6)) WHERE request_key=?", key)
}
func emptyRecoveryProof() chainassets.TONEvidence {
	return chainassets.TONEvidence{Parent: json.RawMessage(`{"transactions":[]}`)}
}
func TestTONRecoveryClaimsAndFencesLateObservers(t *testing.T) {
	for _, name := range []string{"ton-v4-send", "ton-jetton-send"} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			db, _ := userDatabase(t, "fence_"+strings.ReplaceAll(name, "-", "_"))
			f := recoverySample(t, name)
			restoreRecoveryFixture(t, db, f)
			blocked, release := make(chan struct{}), make(chan struct{})
			defer func() {
				select {
				case <-release:
				default:
					close(release)
				}
			}()
			firstSource := recoveryFixtureSource(t, f)
			firstSource.lookup = func(ctx context.Context) (chainassets.TONEvidence, error) {
				close(blocked)
				select {
				case <-release:
					return f.Evidence, nil
				case <-ctx.Done():
					return chainassets.TONEvidence{}, ctx.Err()
				}
			}
			first := newRecovery(t, db, f.Wallet, firstSource, tonSendBroadcast(tonNoBroadcast), recoveryConfig(f.Wallet))
			done := make(chan error, 1)
			go func() { _, err := first.RunOne(ctx); done <- err }()
			select {
			case <-blocked:
			case err := <-done:
				t.Fatal(err)
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			secondSource := recoveryFixtureSource(t, f)
			second := newRecovery(t, db, f.Wallet, secondSource, tonSendBroadcast(tonNoBroadcast), recoveryConfig(f.Wallet))
			if worked, err := second.RunOne(ctx); err != nil || worked {
				t.Fatal("active lease was stolen", worked, err)
			}
			// Network observation holds no SQL row lock. Expire a lost process's lease
			// directly in the isolated DB instead of changing the host clock or sleeping.
			mustExec(t, db, "UPDATE chain_send_intent SET recovery_lease_until=TIMESTAMPADD(SECOND,-1,UTC_TIMESTAMP(6)) WHERE request_key=?", f.Intent.Key)
			if worked, err := second.RunOne(ctx); err != nil || !worked {
				t.Fatal(worked, err)
			}
			close(release)
			if err := <-done; !errors.Is(err, chainsend.ErrRecoveryLeaseLost) {
				t.Fatal("stale completion accepted", err)
			}
			row := recoveryRow(t, second, f.Intent.Key)
			if row.State != chainsend.RecoveryConfirmed || row.Generation != 2 || row.Attempts != 2 || row.ReceiptHash != f.Receipt.Transaction.Signature || row.LeaseUntil.Valid {
				t.Fatal(row)
			}
			if worked, err := second.RunOne(ctx); err != nil || worked || secondSource.calls.Load() != 1 {
				t.Fatal("confirmed work polled again", worked, err)
			}
			if err := second.Retry(ctx, f.Intent.Key, "tester", "should not retry confirmed payment"); !errors.Is(err, chainsend.ErrRecoveryRetryDenied) {
				t.Fatal(err)
			}
			for table, want := range map[string]int{"chain_transfer_event": 1, "chain_observed_transaction": 1, "ledger_voucher": 0, "ledger_entry": 0, "user_account": 0, "account_hold": 0, "transfer_order": 0} {
				var count int
				if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil || count != want {
					t.Fatal(table, count, err)
				}
			}
		})
	}
}
func TestTONRecoveryExpiredUnknownBudgetAndOriginalRetry(t *testing.T) {
	ctx := context.Background()
	db, _ := userDatabase(t, "recovery_budget")
	f := recoverySample(t, "ton-v4-send")
	restoreRecoveryFixture(t, db, f)
	source := recoveryFixtureSource(t, f)
	source.proof = emptyRecoveryProof()
	c := recoveryConfig(f.Wallet)
	c.MaxAttempts = 2
	r := newRecovery(t, db, f.Wallet, source, tonSendBroadcast(tonNoBroadcast), c)
	before := tonFind(t, db, f.Intent.Key)
	if work, err := r.RunOne(ctx); !work || !errors.Is(err, chainsend.ErrExpired) {
		t.Fatal(work, err)
	}
	if work, err := r.RunOne(ctx); work || err != nil {
		t.Fatal("retry delay ignored", work, err)
	}
	dueRecovery(t, db, f.Intent.Key)
	if _, err := r.RunOne(ctx); !errors.Is(err, chainsend.ErrRecoveryBudget) {
		t.Fatal(err)
	}
	row := recoveryRow(t, r, f.Intent.Key)
	if row.State != chainsend.RecoveryReview || row.Attempts != 2 || row.SendState != chainsend.BroadcastUnknown {
		t.Fatal(row)
	}
	if got := tonFind(t, db, f.Intent.Key); got != before {
		t.Fatal("unknown payment was replaced or released", got)
	}
	if err := r.Retry(ctx, f.Intent.Key, "tester", ""); err == nil {
		t.Fatal("retry without reason")
	}
	if err := r.Retry(ctx, f.Intent.Key, "tester", "indexer recovered; inspect original message"); err != nil {
		t.Fatal(err)
	}
	source.proof = f.Evidence
	if _, err := r.RunOne(ctx); err != nil {
		t.Fatal(err)
	}
	row = recoveryRow(t, r, f.Intent.Key)
	if row.State != chainsend.RecoveryConfirmed || row.Attempts != 1 || row.Generation != 4 || !strings.Contains(row.RetryNote, "tester") {
		t.Fatal(row)
	}
}
func TestTONRecoveryNeverSignsPreparedAndRejectsStoredCorruption(t *testing.T) {
	ctx := context.Background()
	db, _ := userDatabase(t, "recovery_corrupt")
	w, _ := tonSendKey(t)
	executor := tonSender(t, db, transaction.New(db), w, nil, tonSendBroadcast(tonNoBroadcast))
	p := tonProposal("unsigned", 1)
	if _, err := executor.Prepare(ctx, p); err != nil {
		t.Fatal(err)
	}
	source := recoveryFixtureSource(t, recoverySample(t, "ton-v4-send"))
	r := newRecovery(t, db, w, source, tonSendBroadcast(tonNoBroadcast), recoveryConfig(w))
	if work, err := r.RunOne(ctx); work || err != nil || source.calls.Load() != 0 {
		t.Fatal("prepared intent processed", work, err)
	}
	if _, err := executor.Execute(ctx, p.Key); !errors.Is(err, chainsend.ErrSigningUnavailable) {
		t.Fatal(err)
	}
	f := recoverySample(t, "ton-v4-send")
	restoreRecoveryFixture(t, db, f)
	mustExec(t, db, "UPDATE chain_send_intent SET signed_boc='corrupt' WHERE request_key=?", f.Intent.Key)
	r = newRecovery(t, db, f.Wallet, source, tonSendBroadcast(tonNoBroadcast), recoveryConfig(f.Wallet))
	if _, err := r.RunOne(ctx); !errors.Is(err, chainsend.ErrConflict) {
		t.Fatal(err)
	}
	if row := recoveryRow(t, r, f.Intent.Key); row.State != chainsend.RecoveryReview || source.calls.Load() != 0 {
		t.Fatal(row)
	}
}

func TestTONRecoveryQueriesBeforeReplayingExactAuthorization(t *testing.T) {
	ctx := context.Background()
	db, _ := userDatabase(t, "recovery_replay")
	w, sign := tonSendKey(t)
	p := tonProposal("recover-original", 7)
	_, sample := tonReceiptFixture(t, "ton-no-state")
	var template struct{ Request chainreceipt.TONRequest }
	raw, err := os.ReadFile("../../logic/chainreceipt/testdata/ton-no-state.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &template); err != nil {
		t.Fatal(err)
	}
	p.Recipient, p.Atoms = template.Request.Recipient, template.Request.Atoms
	var original []byte
	var broadcasts atomic.Int64
	b := tonSendBroadcast(func(_ context.Context, _ string, raw []byte) error {
		broadcasts.Add(1)
		original = append([]byte(nil), raw...)
		return io.ErrUnexpectedEOF
	})
	sender := tonSender(t, db, transaction.New(db), w, sign, b)
	if _, err = sender.Prepare(ctx, p); err != nil {
		t.Fatal(err)
	}
	if _, err = sender.Execute(ctx, p.Key); !errors.Is(err, chainsend.ErrOutcomeUnknown) {
		t.Fatal(err)
	}
	source := &recoverySource{tonReceiptSource: sample, lookup: func(context.Context) (chainassets.TONEvidence, error) {
		return chainassets.TONEvidence{}, io.ErrUnexpectedEOF
	}}
	r := newRecovery(t, db, w, source, tonSendBroadcast(func(_ context.Context, _ string, raw []byte) error {
		broadcasts.Add(1)
		if !bytes.Equal(raw, original) {
			t.Error("recovery replaced signed bytes")
		}
		return nil
	}), recoveryConfig(w))
	if _, err = r.RunOne(ctx); !errors.Is(err, io.ErrUnexpectedEOF) || broadcasts.Load() != 1 {
		t.Fatal("RPC failure triggered replay", err, broadcasts.Load())
	}
	// An unrelated finalized payment must stop automation instead of being
	// borrowed as proof or treated as permission to replay this request.
	source.lookup = func(context.Context) (chainassets.TONEvidence, error) { return source.proof, nil }
	dueRecovery(t, db, p.Key)
	if _, err = r.RunOne(ctx); !errors.Is(err, chainreceipt.ErrConflict) || broadcasts.Load() != 1 {
		t.Fatal("unrelated receipt triggered replay", err)
	}
	if row := recoveryRow(t, r, p.Key); row.State != chainsend.RecoveryReview {
		t.Fatal(row)
	}
	if err = r.Retry(ctx, p.Key, "tester", "source corrected; resume original authorization"); err != nil {
		t.Fatal(err)
	}
	source.lookup = func(context.Context) (chainassets.TONEvidence, error) { return emptyRecoveryProof(), nil }
	dueRecovery(t, db, p.Key)
	if _, err = r.RunOne(ctx); !errors.Is(err, chainreceipt.ErrUnconfirmed) || broadcasts.Load() != 2 {
		t.Fatal(err, broadcasts.Load())
	}
	if v := tonFind(t, db, p.Key); v.State != chainsend.Submitted || v.Attempts != 2 {
		t.Fatal(v)
	}
	// Acceptance cannot settle the payment, and an accepted request is only observed.
	dueRecovery(t, db, p.Key)
	if _, err = r.RunOne(ctx); !errors.Is(err, chainreceipt.ErrUnconfirmed) || broadcasts.Load() != 2 {
		t.Fatal("submitted request replayed", err)
	}
	// Derive deterministic chain evidence for this ephemeral authorization. This is
	// a fake observation, not a claim of a new real chain transfer.
	source.lookup = nil
	root, err := cell.FromBOC(original)
	if err != nil {
		t.Fatal(err)
	}
	var external tlb.ExternalMessage
	if err = tlb.LoadFromCell(&external, root.BeginParse()); err != nil {
		t.Fatal(err)
	}
	edit := func(raw *json.RawMessage, fn func(map[string]any)) {
		var v map[string]any
		if err := json.Unmarshal(*raw, &v); err != nil {
			t.Fatal(err)
		}
		fn(v["transactions"].([]any)[0].(map[string]any))
		*raw, _ = json.Marshal(v)
	}
	owner := external.DstAddr.StringRaw()
	edit(&source.proof.Parent, func(tx map[string]any) {
		tx["account"] = owner
		in := tx["in_msg"].(map[string]any)
		in["destination"], in["init_state"] = owner, nil
		in["message_content"] = map[string]any{"hash": base64.StdEncoding.EncodeToString(external.Body.Hash()), "body": base64.StdEncoding.EncodeToString(external.Body.ToBOC())}
		tx["out_msgs"].([]any)[0].(map[string]any)["source"] = owner
	})
	edit(&source.proof.Transaction, func(tx map[string]any) { tx["in_msg"].(map[string]any)["source"] = owner })
	dueRecovery(t, db, p.Key)
	if _, err = r.RunOne(ctx); err != nil {
		t.Fatal(err)
	}
	if row := recoveryRow(t, r, p.Key); row.State != chainsend.RecoveryConfirmed || broadcasts.Load() != 2 {
		t.Fatal(row, broadcasts.Load())
	}
}
func TestTONRecoveryShutdownDrainsAndRetainsDependenciesOnTimeout(t *testing.T) {
	for _, timeout := range []bool{false, true} {
		t.Run(map[bool]string{false: "drain", true: "timeout"}[timeout], func(t *testing.T) {
			db, _ := userDatabase(t, "recovery_shutdown_"+map[bool]string{false: "drain", true: "timeout"}[timeout])
			f := recoverySample(t, "ton-v4-send")
			restoreRecoveryFixture(t, db, f)
			c := recoveryConfig(f.Wallet)
			c.OperationTimeout = time.Second
			c.LeaseDuration = 2 * time.Second
			c.PollInterval = time.Millisecond
			started, release := make(chan struct{}), make(chan struct{})
			source := recoveryFixtureSource(t, f)
			source.lookup = func(ctx context.Context) (chainassets.TONEvidence, error) {
				close(started)
				select {
				case <-release:
					return f.Evidence, nil
				case <-ctx.Done():
					return chainassets.TONEvidence{}, ctx.Err()
				}
			}
			assets, err := chainassets.New(chainassets.Config{Networks: []chainassets.Endpoint{{Network: chainassets.TONTestnet, URL: "http://127.0.0.1:1"}}}, nil)
			if err != nil {
				t.Fatal(err)
			}
			shared := &svc.ServiceContext{DB: db, Config: config.Config{TONRecovery: &c, ShutdownTimeout: time.Second}, TONSendIntents: chainsend.NewStore(sqlx.NewSqlConnFromDB(db), transaction.New(db)), TONReceipts: chainreceipt.NewTON(sqlx.NewSqlConnFromDB(db), transaction.New(db), source), ChainAssets: assets}
			if timeout {
				shared.Config.ShutdownTimeout = 20 * time.Millisecond
			}
			if err = shared.StartBackground(context.Background()); err != nil {
				t.Fatal(err)
			}
			defer shared.StopBackground()
			select {
			case <-started:
			case <-time.After(5 * time.Second):
				t.Fatal("recovery did not start")
			}
			shared.StopBackground()
			if timeout {
				if err = shared.Close(); !errors.Is(err, context.DeadlineExceeded) {
					t.Fatal("drain timeout hidden", err)
				}
				wait, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				if err = shared.WaitBackground(wait); err != nil {
					t.Fatal(err)
				}
				if err = db.Ping(); err != nil {
					t.Fatal("dependencies closed before drain was confirmed", err)
				}
				var state string
				if err = db.QueryRow("SELECT recovery_state FROM chain_send_intent WHERE request_key=?", f.Intent.Key).Scan(&state); err != nil || state != chainsend.RecoveryRunning {
					t.Fatal("canceled work erased lease", state, err)
				}
				// A fresh process recovers the canceled claim and commits the original fact.
				mustExec(t, db, "UPDATE chain_send_intent SET recovery_lease_until=TIMESTAMPADD(SECOND,-1,UTC_TIMESTAMP(6)) WHERE request_key=?", f.Intent.Key)
				recovered := newRecovery(t, db, f.Wallet, recoveryFixtureSource(t, f), tonSendBroadcast(tonNoBroadcast), c)
				if _, err = recovered.RunOne(context.Background()); err != nil {
					t.Fatal(err)
				}
				assets.Close()
			} else {
				close(release)
				wait, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				if err = shared.WaitBackground(wait); err != nil {
					t.Fatal(err)
				}
				var state string
				if err = db.QueryRow("SELECT recovery_state FROM chain_send_intent WHERE request_key=?", f.Intent.Key).Scan(&state); err != nil || state != chainsend.RecoveryConfirmed {
					t.Fatal(state, err)
				}
				if err = shared.Close(); err != nil {
					t.Fatal(err)
				}
				if err = db.Ping(); err == nil {
					t.Fatal("DB left open after successful drain")
				}
			}
			if source.calls.Load() != 1 {
				t.Fatal("new work admitted after Stop", source.calls.Load())
			}
		})
	}
}

// Optional real read-only verification. Restored public authorizations have
// already been consumed and expired; the executor has no signer or broadcaster.
func TestLiveTONRecoveryHistoricalMessages(t *testing.T) {
	if os.Getenv("DEXWALLET_TEST_TON_RECOVERY_LIVE") != "1" {
		t.Skip("explicit live read-only opt-in required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	db, _ := userDatabase(t, "recovery_live")
	for _, name := range []string{"ton-v4-send", "ton-jetton-send"} {
		f := recoverySample(t, name)
		if int64(f.Intent.ValidUntil) >= time.Now().Unix() {
			t.Fatal("historical authorization has not expired")
		}
		restoreRecoveryFixture(t, db, f)
		assets, err := chainassets.New(chainassets.Config{Timeout: 30 * time.Second, Networks: []chainassets.Endpoint{{Network: chainassets.TONTestnet, URL: "https://testnet.toncenter.com", TestUSDTContract: f.Wallet.JettonMaster, APIKey: os.Getenv("DEXWALLET_CHAIN_TON_TESTNET_API_KEY")}}}, nil)
		if err != nil {
			t.Fatal(err)
		}
		r := newRecovery(t, db, f.Wallet, assets, tonSendBroadcast(tonNoBroadcast), recoveryConfig(f.Wallet))
		if _, err = r.RunOne(ctx); err != nil {
			assets.Close()
			t.Fatal(err)
		}
		rows, err := r.List(ctx, chainsend.RecoveryConfirmed, 10)
		assets.Close()
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, row := range rows {
			if row.Key == f.Intent.Key {
				found = true
				if row.ReceiptHash != f.Receipt.Transaction.Signature || row.Attempts != 1 {
					t.Fatal(row)
				}
				t.Logf("%s receipt=%s recovery=%s attempts=%d", name, row.ReceiptHash, row.State, row.Attempts)
			}
		}
		if !found {
			t.Fatal("live receipt not persisted")
		}
		if dir := os.Getenv("DEXWALLET_TEST_TON_RECOVERY_EVIDENCE"); dir != "" {
			b, err := json.MarshalIndent(rows, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(dir, name+"-recovery.json"), append(b, '\n'), 0600); err != nil {
				t.Fatal(err)
			}
		}

	}
}

func TestTONRecoveryRequiresMigrationAndProtectsHistory(t *testing.T) {
	ctx := context.Background()
	name, dsn, db, err := testRun.NewDatabase(ctx, "recovery_schema")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	m, err := migration.Open(dsn, name, "../../manifest/sql/diff/business", "business")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if err = m.Migrate(21); err != nil {
		t.Fatal(err)
	}
	f := recoverySample(t, "ton-v4-send")
	source := recoveryFixtureSource(t, f)
	r := newRecovery(t, db, f.Wallet, source, tonSendBroadcast(tonNoBroadcast), recoveryConfig(f.Wallet))
	runtime := chainsend.NewRecoveryRuntime(r)
	if err = runtime.Start(ctx); err == nil || !strings.Contains(err.Error(), "schema") {
		t.Fatal("missing migration accepted", err)
	}
	runtime.Stop()
	if err = runtime.Wait(ctx); err != nil || source.calls.Load() != 0 {
		t.Fatal(err)
	}
	if err = m.Migrate(22); err != nil {
		t.Fatal(err)
	}
	restoreRecoveryFixture(t, db, f)
	if _, err = r.RunOne(ctx); err != nil {
		t.Fatal(err)
	}
	if err = m.Migrate(21); err == nil {
		t.Fatal("recovery history discarded")
	}
	if row := recoveryRow(t, r, f.Intent.Key); row.State != chainsend.RecoveryConfirmed || row.ReceiptHash != f.Receipt.Transaction.Signature {
		t.Fatal(row)
	}
}

func TestTONRecoveryReceiptSurvivesScheduleWriteFailure(t *testing.T) {
	ctx := context.Background()
	db, _ := userDatabase(t, "recover_finish")
	f := recoverySample(t, "ton-v4-send")
	restoreRecoveryFixture(t, db, f)
	source := recoveryFixtureSource(t, f)
	r := newRecovery(t, db, f.Wallet, source, tonSendBroadcast(tonNoBroadcast), recoveryConfig(f.Wallet))
	mustExec(t, db, "ALTER TABLE chain_send_intent ADD CONSTRAINT test_recovery_finish CHECK (recovery_state <> 'confirmed')")
	if _, err := r.RunOne(ctx); err == nil || !strings.Contains(err.Error(), "test_recovery_finish") {
		t.Fatal(err)
	}
	row := recoveryRow(t, r, f.Intent.Key)
	if row.State != chainsend.RecoveryRunning || row.SendState != chainsend.Submitted {
		t.Fatal(row)
	}
	var facts int
	if err := db.QueryRow("SELECT COUNT(*) FROM chain_transfer_event").Scan(&facts); err != nil || facts != 1 {
		t.Fatal("receipt was lost", facts, err)
	}
	mustExec(t, db, "ALTER TABLE chain_send_intent DROP CHECK test_recovery_finish")
	mustExec(t, db, "UPDATE chain_send_intent SET recovery_lease_until=TIMESTAMPADD(SECOND,-1,UTC_TIMESTAMP(6)) WHERE request_key=?", f.Intent.Key)
	restarted := newRecovery(t, db, f.Wallet, source, tonSendBroadcast(tonNoBroadcast), recoveryConfig(f.Wallet))
	if _, err := restarted.RunOne(ctx); err != nil {
		t.Fatal(err)
	}
	row = recoveryRow(t, restarted, f.Intent.Key)
	if row.State != chainsend.RecoveryConfirmed || row.Attempts != 2 || row.Generation != 2 {
		t.Fatal(row)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM chain_transfer_event").Scan(&facts); err != nil || facts != 1 {
		t.Fatal("receipt duplicated", facts, err)
	}
}
