//go:build integration

package integration

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/config"
	"github.com/0xabcd-k/DexWallet/backend/internal/migration"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainreceipt"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainsend"
	"github.com/0xabcd-k/DexWallet/backend/svc"
	"github.com/0xabcd-k/DexWallet/backend/transaction"
	"github.com/mr-tron/base58"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

func restoreSolanaPayout(t *testing.T, db *sql.DB, c payoutCapture, state string) {
	t.Helper()
	p := c.Intent
	// Business 23 native fixtures predate the asset column added by migration 24.
	if p.AssetId == "" {
		p.AssetId = chainassets.SolanaDevnet + ":sol"
	}
	sig := p.Signature
	if state == chainsend.Prepared {
		sig = ""
	}
	mustExec(t, db, "INSERT INTO solana_payout(request_key,sender,recipient,lamports,asset_id,mint,token_atoms,create_ata,blockhash,last_valid_height,message_hash,signature,state) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)", p.RequestKey, p.Sender, p.Recipient, p.Lamports, p.AssetId, p.Mint, p.TokenAtoms, p.CreateAta, p.Blockhash, p.LastValidHeight, p.MessageHash, sig, state)
}
func solanaScheduler(t *testing.T, db *sql.DB, node chainsend.SolanaNode, source chainreceipt.Source, sender string, attempts int64) *chainsend.SolanaRecovery {
	t.Helper()
	policy := chainsend.SolanaRecoveryConfig{Sender: sender, RecoveryPolicy: chainsend.DefaultRecoveryPolicy()}
	policy.MaxAttempts = attempts
	receipts := chainreceipt.New(sqlx.NewSqlConnFromDB(db), transaction.New(db), source)
	r, err := chainsend.NewSolanaRecovery(db, node, receipts, policy)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func noSolanaBroadcast(t *testing.T, height int64) *payoutRPC {
	t.Helper()
	return &payoutRPC{height: height, submit: func([]byte) error { t.Error("unexpected broadcast"); return errors.New("broadcast disabled") }}
}
func TestSolanaRecoveryHistorical(t *testing.T) { verifySolanaRecoveryHistory(t, false) }
func TestLiveSolanaRecoveryHistorical(t *testing.T) {
	if os.Getenv("DEXWALLET_TEST_SOLANA_RECOVERY_LIVE") != "1" {
		t.Skip("read-only Devnet verification requires explicit opt-in")
	}
	verifySolanaRecoveryHistory(t, true)
}
func verifySolanaRecoveryHistory(t *testing.T, live bool) {
	db, _ := userDatabase(t, "sol_recovery_history")
	var liveSource *chainassets.Service
	if live {
		endpoint := os.Getenv("DEXWALLET_CHAIN_SOLANA_DEVNET_RPC_URL")
		if endpoint == "" {
			endpoint = "https://api.devnet.solana.com"
		}
		assets, err := chainassets.New(chainassets.Config{Timeout: 30 * time.Second, Networks: []chainassets.Endpoint{{Network: chainassets.SolanaDevnet, URL: endpoint, TestUSDTContract: "2ydN2Tr1FhRmJJcdqATsFCq5Q1t43bfRQD5tKfSxL7d5"}}}, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer assets.Close()
		liveSource = assets
	}
	for _, name := range []string{"sol-payout", "spl-payout-usdc-existing", "spl-payout-usdc-new", "spl-payout-usdt-fixture-existing", "spl-payout-usdt-fixture-new"} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			raw, err := os.ReadFile(filepath.Join("../../logic/chainreceipt/testdata", name+".json"))
			if err != nil {
				t.Fatal(err)
			}
			var c payoutCapture
			if err = json.Unmarshal(raw, &c); err != nil {
				t.Fatal(err)
			}
			restoreSolanaPayout(t, db, c, chainsend.Submitted)
			_, fixture := receiptFixtureSource(t, "sol")
			fixture.proof = c.Evidence
			var source chainreceipt.Source = fixture
			if liveSource != nil {
				source = liveSource
			}
			r := solanaScheduler(t, db, noSolanaBroadcast(t, c.Intent.LastValidHeight+1), source, c.Intent.Sender, 3)
			if worked, err := r.RunOne(ctx); err != nil || !worked {
				t.Fatal(worked, err)
			}
			rows, err := r.List(ctx, chainsend.RecoveryConfirmed, 10)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, row := range rows {
				if row.Key == c.Intent.RequestKey {
					found = row.Signature == c.Intent.Signature && row.Attempts == 1 && row.SendState == chainsend.SolanaConfirmed && !row.LeaseUntil.Valid
				}
			}
			if !found {
				t.Fatal("missing confirmation", rows)
			}
			var funding int64
			if err = db.QueryRow("SELECT ata_funding_lamports FROM solana_payout WHERE request_key=?", c.Intent.RequestKey).Scan(&funding); err != nil || funding != c.Intent.AtaFundingLamports {
				t.Fatal(funding, err)
			}
			got, err := chainreceipt.New(sqlx.NewSqlConnFromDB(db), transaction.New(db), source).Find(ctx, chainassets.SolanaDevnet, c.Intent.Signature, int(c.Intent.CreateAta))
			if err != nil || got.Transaction != c.Receipt.Transaction || got.Event != c.Receipt.Event {
				t.Fatal("historical receipt mismatch", got, err)
			}
			if worked, err := r.RunOne(ctx); err != nil || worked {
				t.Fatal("confirmed payment claimed twice", worked, err)
			}
			t.Logf("%s signature=%s fee=%s ATA=%d", got.Event.AssetID, c.Intent.Signature, got.Transaction.FeeAtoms, funding)
		})
	}
	for table, want := range map[string]int{"chain_transfer_event": 5, "ledger_voucher": 0, "event_outbox": 0} {
		var n int
		if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil || n != want {
			t.Fatal(table, n, err)
		}
	}
}

type delayedSolanaSource struct {
	*receiptSource
	entered chan struct{}
	release chan struct{}
}

func (s *delayedSolanaSource) ReadSolanaEvidence(ctx context.Context, network, signature string) (chainassets.SolanaEvidence, error) {
	close(s.entered)
	select {
	case <-s.release:
		return s.receiptSource.ReadSolanaEvidence(ctx, network, signature)
	case <-ctx.Done():
		return chainassets.SolanaEvidence{}, ctx.Err()
	}
}

func TestSolanaRecoveryLateObserverAndReview(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	db, _ := userDatabase(t, "sol_recovery_fence")
	c := solPayoutCapture(t)
	restoreSolanaPayout(t, db, c, chainsend.Signed)
	_, src := receiptFixtureSource(t, "sol")
	src.proof = chainassets.SolanaEvidence{Transaction: json.RawMessage("null"), Status: json.RawMessage(`{"context":{"slot":50},"value":[null]}`)}
	slow := &delayedSolanaSource{src, make(chan struct{}), make(chan struct{})}
	late := solanaScheduler(t, db, noSolanaBroadcast(t, c.Intent.LastValidHeight), slow, c.Intent.Sender, 2)
	restarted := solanaScheduler(t, db, noSolanaBroadcast(t, c.Intent.LastValidHeight+1), src, c.Intent.Sender, 2)
	other := solanaScheduler(t, db, noSolanaBroadcast(t, c.Intent.LastValidHeight), src, c.Intent.Recipient, 2)
	if worked, err := other.RunOne(ctx); worked || err != nil {
		t.Fatal("wrong sender", worked, err)
	}
	done := make(chan error, 1)
	go func() { _, err := late.RunOne(ctx); done <- err }()
	select {
	case <-slow.entered:
	case err := <-done:
		t.Fatal(err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if worked, err := restarted.RunOne(ctx); worked || err != nil {
		t.Fatal("active lease claimed twice", worked, err)
	}
	mustExec(t, db, "UPDATE solana_payout SET recovery_lease_until=TIMESTAMPADD(SECOND,-1,UTC_TIMESTAMP(6)) WHERE request_key=?", c.Intent.RequestKey)
	if _, err := restarted.RunOne(ctx); !errors.Is(err, chainsend.ErrRecoveryBudget) || !errors.Is(err, chainsend.ErrSolanaExpired) {
		t.Fatal(err)
	}
	close(slow.release)
	if err := <-done; !errors.Is(err, chainsend.ErrRecoveryLeaseLost) {
		t.Fatal("stale worker changed review", err)
	}
	rows, err := restarted.List(ctx, chainsend.RecoveryReview, 1)
	if err != nil || len(rows) != 1 || rows[0].Generation != 2 || rows[0].Attempts != 2 || rows[0].LeaseUntil.Valid {
		t.Fatal(rows, err)
	}
	if err = other.Retry(ctx, c.Intent.RequestKey, "operator", "查原签名"); !errors.Is(err, chainsend.ErrRecoveryRetryDenied) {
		t.Fatal(err)
	}
	if err = restarted.Retry(ctx, c.Intent.RequestKey, "operator", "查原签名"); err != nil {
		t.Fatal(err)
	}
	src.proof = c.Evidence
	if worked, err := restarted.RunOne(ctx); !worked || err != nil {
		t.Fatal(worked, err)
	}
	rows, err = restarted.List(ctx, chainsend.RecoveryConfirmed, 1)
	if err != nil || len(rows) != 1 || rows[0].Signature != c.Intent.Signature || !strings.Contains(rows[0].RetryNote, "查原签名") || rows[0].Generation != 4 {
		t.Fatal(rows, err)
	}
	if err = restarted.Retry(ctx, c.Intent.RequestKey, "operator", "repeat"); !errors.Is(err, chainsend.ErrRecoveryRetryDenied) {
		t.Fatal(err)
	}
}

func TestSolanaRecoveryMigration(t *testing.T) {
	ctx := context.Background()
	name, dsn, db, err := testRun.NewDatabase(ctx, "sol_recovery_upgrade")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	m, err := migration.Open(dsn, name, "../../manifest/sql/diff/business", "business")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	for _, v := range []uint{24, 25, 24} {
		if err = m.Migrate(v); err != nil {
			t.Fatal(err)
		}
	}
	c := solPayoutCapture(t)
	restoreSolanaPayout(t, db, c, chainsend.SolanaConfirmed)
	c.Intent.RequestKey = "pending-migration"
	c.Intent.MessageHash = strings.Repeat("a", 64)
	restoreSolanaPayout(t, db, c, chainsend.Signed)
	c.Intent.RequestKey = "unsigned-migration"
	c.Intent.MessageHash = strings.Repeat("b", 64)
	restoreSolanaPayout(t, db, c, chainsend.Prepared)
	_, src := receiptFixtureSource(t, "sol")
	r := solanaScheduler(t, db, noSolanaBroadcast(t, c.Intent.LastValidHeight), src, c.Intent.Sender, 3)
	runtime := chainsend.NewRecoveryRuntime(r)
	if err = runtime.Start(ctx); err == nil || !strings.Contains(err.Error(), "schema") {
		t.Fatal("old schema accepted", err)
	}
	runtime.Stop()
	if err = runtime.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	if err = m.Migrate(25); err != nil {
		t.Fatal(err)
	}
	rows, err := r.List(ctx, chainsend.RecoveryConfirmed, 10)
	if err != nil || len(rows) != 1 || rows[0].Attempts != 0 || rows[0].Signature != c.Intent.Signature {
		t.Fatal(rows, err)
	}
	if worked, err := r.RunOne(ctx); !worked || !errors.Is(err, chainsend.ErrConflict) {
		t.Fatal("corrupt intent must stop for review", worked, err)
	}
	if worked, err := r.RunOne(ctx); worked || err != nil {
		t.Fatal("unsigned intent was claimed", worked, err)
	}
	if err = m.Migrate(24); err == nil {
		t.Fatal("downgrade discarded recovery history")
	}
	rows, err = r.List(ctx, chainsend.RecoveryReview, 10)
	if err != nil || len(rows) != 1 || rows[0].Key != "pending-migration" {
		t.Fatal(rows, err)
	}
}

func TestSolanaRecoveryAcknowledgementAndRuntimeRestart(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	db, _ := userDatabase(t, "sol_recovery_restart")
	conn := sqlx.NewSqlConnFromDB(db)
	c := solPayoutCapture(t)
	restoreSolanaPayout(t, db, c, chainsend.Signed)
	_, src := receiptFixtureSource(t, "sol")
	src.proof = chainassets.SolanaEvidence{Transaction: json.RawMessage("null"), Status: json.RawMessage(`{"context":{"slot":50},"value":[null]}`)}
	msg, err := chainsend.SolanaPayoutMessage(c.Intent.Sender, c.Intent.Recipient, c.Intent.Lamports, c.Intent.Blockhash)
	if err != nil {
		t.Fatal(err)
	}
	sig, err := base58.Decode(c.Intent.Signature)
	if err != nil {
		t.Fatal(err)
	}
	want := append(append([]byte{1}, sig...), msg...)
	sends := 0
	node := &payoutRPC{height: c.Intent.LastValidHeight, submit: func(wire []byte) error {
		sends++
		if !bytes.Equal(wire, want) {
			t.Error("wire differs from original authorization")
		}
		return nil
	}}
	r := solanaScheduler(t, db, node, src, c.Intent.Sender, 5)
	if worked, err := r.RunOne(ctx); !worked || !errors.Is(err, chainreceipt.ErrUnconfirmed) {
		t.Fatal(worked, err)
	}
	rows, err := r.List(ctx, chainsend.RecoveryReady, 1)
	if err != nil || len(rows) != 1 || rows[0].SendState != chainsend.Submitted {
		t.Fatal(rows, err)
	}
	if worked, err := r.RunOne(ctx); worked || err != nil {
		t.Fatal("retry interval ignored", worked, err)
	}
	src.proof = c.Evidence
	due := func() {
		mustExec(t, db, "UPDATE solana_payout SET recovery_next_at=UTC_TIMESTAMP(6) WHERE request_key=?", c.Intent.RequestKey)
	}
	due()
	mustExec(t, db, "ALTER TABLE solana_payout ADD CONSTRAINT sol_recovery_fault CHECK (state<>'confirmed')")
	if _, err := r.RunOne(ctx); err == nil || !strings.Contains(err.Error(), "sol_recovery_fault") {
		t.Fatal("expected local persistence failure", err)
	}
	mustExec(t, db, "ALTER TABLE solana_payout DROP CHECK sol_recovery_fault")
	due()
	// Start the application worker with a disconnected RPC. It must reuse the
	// committed receipt after the failed local update, including on shutdown.
	offline, err := chainassets.New(chainassets.Config{Networks: []chainassets.Endpoint{{Network: chainassets.SolanaDevnet, URL: "http://127.0.0.1:1"}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer offline.Close()
	policy := chainsend.SolanaRecoveryConfig{Sender: c.Intent.Sender, RecoveryPolicy: chainsend.DefaultRecoveryPolicy()}
	shared := &svc.ServiceContext{Config: config.Config{SolanaRecovery: &policy}, DB: db, ChainAssets: offline, ChainReceipts: chainreceipt.New(conn, transaction.New(db), offline)}
	if err = shared.StartBackground(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() {
		shared.StopBackground()
		if err := shared.WaitBackground(ctx); err != nil {
			t.Error(err)
		}
	}()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		rows, err = r.List(ctx, chainsend.RecoveryConfirmed, 1)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) == 1 {
			break
		}
		select {
		case <-tick.C:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	if rows[0].Attempts != 3 || rows[0].Signature != c.Intent.Signature || sends != 1 {
		t.Fatal(rows, sends)
	}
	var events int
	if err = db.QueryRow("SELECT COUNT(*) FROM chain_transfer_event").Scan(&events); err != nil || events != 1 {
		t.Fatal(events, err)
	}
}
