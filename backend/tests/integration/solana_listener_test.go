//go:build integration

package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/config"
	"github.com/0xabcd-k/DexWallet/backend/internal/migration"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainreceipt"
	"github.com/0xabcd-k/DexWallet/backend/svc"
	"github.com/0xabcd-k/DexWallet/backend/transaction"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

type historicalSolBlock struct {
	Slot  int64
	Block json.RawMessage
}

func solHistory(t *testing.T, name string) []historicalSolBlock {
	t.Helper()
	b, e := os.ReadFile("../../logic/chainreceipt/testdata/solana-collection-blocks.json")
	if e != nil {
		t.Fatal(e)
	}
	var all map[string][]historicalSolBlock
	if e = json.Unmarshal(b, &all); e != nil {
		t.Fatal(e)
	}
	if len(all[name]) < 2 {
		t.Fatal(name)
	}
	return all[name]
}

func listenerTerms(t *testing.T, db *sql.DB, name string, a *chainreceipt.Attribution) chainreceipt.Request {
	t.Helper()
	req, src := receiptFixtureSource(t, name)
	r, err := chainreceipt.NewVerifier(src).Verify(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	var at struct{ BlockTime int64 }
	if err = json.Unmarshal(src.proof.Transaction, &at); err != nil {
		t.Fatal(err)
	}
	_, terms, _ := collectionSetup(db, r)
	ctx := context.Background()
	if err = a.Register(ctx, terms.Network, terms.Recipient, chainreceipt.SingleUseAddress); err != nil {
		t.Fatal(err)
	}
	if _, err = a.Open(ctx, terms); err != nil {
		t.Fatal(err)
	}
	// Restore a simulated historical order window in the isolated DB. No SQL
	// seed contains a transaction signature, instruction index or matched event.
	mustExec(t, db, "UPDATE chain_receive_request SET opens_at=?,expires_at=? WHERE request_key=?", at.BlockTime, at.BlockTime+2, terms.Key)
	return req
}

func TestLiveSolanaReceiptListener(t *testing.T) {
	if os.Getenv("DEXWALLET_TEST_SOLANA_LISTENER_LIVE") != "1" {
		t.Skip("opt-in read-only Solana Devnet")
	}
	for i, n := range []string{"sol", "usdc", "usdt-fixture"} {
		t.Run(n, func(t *testing.T) {
			db, _ := userDatabase(t, fmt.Sprintf("sol_listen_live_%d", i))
			cfg := chainassets.Config{Timeout: 18 * time.Second, Networks: []chainassets.Endpoint{{Network: chainassets.SolanaDevnet, URL: "https://api.devnet.solana.com", TestUSDTContract: "2ydN2Tr1FhRmJJcdqATsFCq5Q1t43bfRQD5tKfSxL7d5"}}}
			cfg.LoadEnvironment()
			assets, err := chainassets.New(cfg, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer assets.Close()
			conn := sqlx.NewSqlConnFromDB(db)
			a := chainreceipt.NewAttribution(conn, assets.Catalog())
			req := listenerTerms(t, db, n, a)
			policy := chainreceipt.SolanaListenerConfig{AnchorSlot: solHistory(t, n)[0].Slot, ListenerConfig: chainreceipt.ListenerConfig{PollInterval: 2 * time.Second, OperationTimeout: 25 * time.Second, RetryInterval: 35 * time.Second}}
			shared := svc.ServiceContext{DB: db, ChainAssets: assets, Config: config.Config{SolanaReceiptListener: &policy}, ReceiveAttribution: a, ChainReceipts: chainreceipt.New(conn, transaction.New(db), assets)}
			ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
			defer cancel()
			if err = shared.StartBackground(ctx); err != nil {
				t.Fatal(err)
			}
			defer func() {
				shared.StopBackground()
				drain, stop := context.WithTimeout(context.Background(), 5*time.Second)
				defer stop()
				if err := shared.WaitBackground(drain); err != nil {
					t.Error(err)
				}
			}()
			for {
				var total int
				err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM solana_receive_candidate WHERE status='matched'").Scan(&total)
				if err != nil {
					t.Fatal(err)
				}
				if total == 1 {
					break
				}
				select {
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				case <-time.After(time.Second):
				}
			}
			found, err := a.Find(ctx, "attempt-a")
			if err != nil || found.Event == nil || found.Event.Signature != req.Signature || found.Event.Index != req.Instruction {
				t.Fatal(found, err)
			}
			t.Logf("found %s: %s atoms, signature %s, instruction %d", found.AssetID, found.Atoms, found.Event.Signature, found.Event.Index)
		})
	}
}

type solHistoryFake struct {
	*receiptSource
	blocks      []historicalSolBlock
	beforeBlock func(int64)
	offline     bool
}

func (s *solHistoryFake) NextSolanaHistorySlots(_ context.Context, after int64) ([]int64, error) {
	slots := []int64{}
	for _, b := range s.blocks {
		if b.Slot > after {
			slots = append(slots, b.Slot)
		}
	}
	return slots, nil
}
func (s *solHistoryFake) ReadSolanaHistoryBlock(_ context.Context, slot int64, _ bool) (json.RawMessage, error) {
	if s.beforeBlock != nil {
		s.beforeBlock(slot)
	}
	for _, b := range s.blocks {
		if b.Slot == slot {
			return b.Block, nil
		}
	}
	return nil, errors.New("unknown slot")
}
func (s *solHistoryFake) ReadSolanaEvidence(ctx context.Context, n, sig string) (chainassets.SolanaEvidence, error) {
	if s.offline {
		return chainassets.SolanaEvidence{}, errListenerOffline
	}
	return s.receiptSource.ReadSolanaEvidence(ctx, n, sig)
}

func TestSolanaReceiptListenerCatchup(t *testing.T) {
	ctx := context.Background()
	db, _ := userDatabase(t, "sol_listen_recover")
	_, src := receiptFixtureSource(t, "usdc")
	fake := &solHistoryFake{receiptSource: src, blocks: solHistory(t, "usdc")}
	conn := sqlx.NewSqlConnFromDB(db)
	a := chainreceipt.NewAttribution(conn, src.catalog)
	req := listenerTerms(t, db, "usdc", a)
	receipts := chainreceipt.New(conn, transaction.New(db), fake)
	newListener := func() *chainreceipt.SolanaListener { return chainreceipt.NewSolanaListener(db, fake, receipts, a) }
	l := newListener()
	count := func(query string) int64 {
		t.Helper()
		var n int64
		if e := db.QueryRow(query).Scan(&n); e != nil {
			t.Fatal(e)
		}
		return n
	}
	target := fake.blocks[len(fake.blocks)-1]
	all := fake.blocks
	policy := chainreceipt.SolanaListenerConfig{AnchorSlot: target.Slot, ListenerConfig: chainreceipt.ListenerConfig{PollInterval: time.Second, OperationTimeout: 5 * time.Second, RetryInterval: time.Minute}}
	if _, err := l.Scan(ctx, policy.AnchorSlot); err == nil || !strings.Contains(err.Error(), "earliest issued request") {
		t.Fatal("late anchor", err)
	}
	fake.blocks = all[:1]
	policy.AnchorSlot = all[0].Slot
	_, err := l.Scan(ctx, policy.AnchorSlot)
	if err != nil {
		t.Fatal(err)
	}
	fake.blocks = []historicalSolBlock{all[0], target}
	if _, err = l.Scan(ctx, policy.AnchorSlot); !errors.Is(err, chainreceipt.ErrInvalidEvidence) {
		t.Fatal("missing block was skipped", err)
	}
	if count("SELECT last_slot FROM solana_receive_cursor") != all[0].Slot {
		t.Fatal("cursor crossed gap")
	}
	fake.blocks = all
	mustExec(t, db, "ALTER TABLE solana_receive_candidate ADD CONSTRAINT sol_test_insert CHECK (slot<>"+fmt.Sprint(target.Slot)+")")
	if _, err = l.Scan(ctx, policy.AnchorSlot); err == nil || !strings.Contains(err.Error(), "sol_test_insert") {
		t.Fatal("candidate insert failure hidden", err)
	}
	if count("SELECT last_slot FROM solana_receive_cursor") != all[len(all)-2].Slot {
		t.Fatal("cursor lost partial page")
	}
	mustExec(t, db, "ALTER TABLE solana_receive_candidate DROP CHECK sol_test_insert")
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	fake.beforeBlock = func(slot int64) {
		if slot == target.Slot && calls.Add(1) == 1 {
			close(entered)
			<-release
		}
	}
	first := make(chan error, 1)
	go func() { _, e := l.Scan(ctx, policy.AnchorSlot); first <- e }()
	select {
	case <-entered:
	case <-time.After(time.Second * 5):
		close(release)
		t.Fatal("stalled")
	}
	_, second := newListener().Scan(ctx, policy.AnchorSlot)
	close(release)
	if err = <-first; err != nil || second != nil {
		t.Fatal(err, second)
	}
	if count("SELECT COUNT(*) FROM solana_receive_candidate") != 1 {
		t.Fatal("duplicate or missing candidate")
	}
	fake.offline = true
	if _, err = l.Process(ctx, time.Minute); !errors.Is(err, errListenerOffline) {
		t.Fatal(err)
	}
	fake.offline = false
	// A second matching transfer instruction in the same transaction competes
	// for the original request. A crash after saving the first proof must still
	// allow discovery of the second instruction without a working RPC.
	editReceipt(t, src, func(m map[string]any) {
		msg := m["transaction"].(map[string]any)["message"].(map[string]any)
		ix := msg["instructions"].([]any)
		msg["instructions"] = append(ix, ix[req.Instruction])
	})
	if _, err = receipts.Record(ctx, req); err != nil {
		t.Fatal(err)
	}
	fake.offline = true
	mustExec(t, db, "UPDATE solana_receive_candidate SET next_try=UTC_TIMESTAMP(6)")
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := newListener().Process(ctx, time.Minute); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if count("SELECT COUNT(*) FROM solana_receive_candidate WHERE status='matched' AND matched_events=1 AND unmatched_events=1") != 1 || count("SELECT COUNT(*) FROM chain_transfer_event") != 2 {
		t.Fatal("incomplete multi-instruction replay")
	}
	for _, name := range []string{"ledger_voucher", "user_account", "event_outbox"} {
		if count("SELECT COUNT(*) FROM "+name) != 0 {
			t.Fatal(name)
		}
	}
}
func TestSolanaReceiptListenerUpgrade(t *testing.T) {
	ctx := context.Background()
	name, dsn, db, err := testRun.NewDatabase(ctx, "sol_listen_migrate")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	m, err := migration.Open(dsn, name, "../../manifest/sql/diff/business", "business")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if err = m.Migrate(33); err != nil {
		t.Fatal(err)
	}
	r := recordCollectionFixture(t, db, "sol")
	for _, v := range []uint{34, 33, 34} {
		if err = m.Migrate(v); err != nil {
			t.Fatal(v, err)
		}
	}
	var sig string
	if err = db.QueryRow("SELECT signature FROM chain_observed_transaction").Scan(&sig); err != nil || sig != r.Transaction.Signature {
		t.Fatal(sig, err)
	}
	mustExec(t, db, "INSERT INTO solana_receive_cursor(id,last_slot,last_hash) VALUES(1,?,?)", r.Transaction.Slot, "test-checkpoint")
	if err = m.Migrate(33); err == nil || !strings.Contains(err.Error(), "sol_receive_rollback_guard") {
		t.Fatal("unsafe downgrade", err)
	}
}
