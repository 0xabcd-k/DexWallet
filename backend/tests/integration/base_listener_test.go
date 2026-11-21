//go:build integration

package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"strconv"
	"strings"
	"sync"
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

func baseListenerOrder(t *testing.T, db *sql.DB, name string) (*chainreceipt.Attribution, chainreceipt.EventKey, int64) {
	t.Helper()
	req, snapshot := baseReceiptFixture(t, name)
	r, err := chainreceipt.NewBaseVerifier(snapshot).Verify(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	var b struct{ Timestamp string }
	if err = json.Unmarshal(snapshot.proof.Block, &b); err != nil {
		t.Fatal(err)
	}
	at, err := strconv.ParseInt(b.Timestamp, 0, 64)
	if err != nil {
		t.Fatal(err)
	}
	a, terms, key := collectionSetup(db, r)
	ctx := context.Background()
	if err = a.Register(ctx, terms.Network, terms.Recipient, chainreceipt.SingleUseAddress); err != nil {
		t.Fatal(err)
	}
	if _, err = a.Open(ctx, terms); err != nil {
		t.Fatal(err)
	}
	// Only the original order terms and the historical order window are seeded.
	mustExec(t, db, "UPDATE chain_receive_request SET opens_at=?,expires_at=? WHERE request_key=?", at, at+2, terms.Key)
	return a, key, r.Transaction.Slot
}
func TestLiveBaseReceiptListener(t *testing.T) {
	if os.Getenv("DEXWALLET_TEST_BASE_LISTENER_LIVE") != "1" {
		t.Skip("opt-in read-only historical Base Sepolia RPC")
	}
	// Multiple Base blocks may share one Unix second. Each anchor is the
	// last archived block strictly before its request window.
	for i, sample := range []struct {
		name   string
		anchor int64
	}{
		{"base-eth", 53792340}, {"base-usdc", 53543600}, {"base-internal-eth", 53792625},
	} {
		t.Run(sample.name, func(t *testing.T) {
			db, _ := userDatabase(t, fmt.Sprintf("base_listen_live_%d", i))
			a, key, height := baseListenerOrder(t, db, sample.name)
			cfg := chainassets.Config{Timeout: 25 * time.Second, Networks: []chainassets.Endpoint{{Network: chainassets.BaseSepolia, URL: "https://base-testnet.api.pocket.network"}}}
			cfg.LoadEnvironment()
			source, err := chainassets.New(cfg, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer source.Close()
			policy := chainreceipt.BaseListenerConfig{AnchorHeight: sample.anchor, ListenerConfig: chainreceipt.ListenerConfig{PollInterval: 2 * time.Second, OperationTimeout: 45 * time.Second, RetryInterval: 50 * time.Second}}
			conn := sqlx.NewSqlConnFromDB(db)
			shared := svc.ServiceContext{DB: db, ChainAssets: source, Config: config.Config{BaseReceiptListener: &policy}, ReceiveAttribution: a, BaseReceipts: chainreceipt.NewBase(conn, transaction.New(db), source)}
			ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
			defer cancel()
			if err = shared.StartBackground(ctx); err != nil {
				t.Fatal(err)
			}
			defer func() {
				shared.StopBackground()
				drain, stop := context.WithTimeout(context.Background(), 6*time.Second)
				defer stop()
				if err := shared.WaitBackground(drain); err != nil {
					t.Error(err)
				}
			}()
			for {
				var found int
				err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM base_receive_candidate WHERE status='matched'").Scan(&found)
				if err != nil {
					t.Fatal(err)
				}
				if found > 0 {
					break
				}
				select {
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				case <-time.After(time.Second):
				}
			}
			order, err := a.Find(ctx, "attempt-a")
			if err != nil || order.Event == nil || *order.Event != key {
				t.Fatal(order, err)
			}
			t.Logf("matched %s %s atoms at block %d, %s %s/%d", order.AssetID, order.Atoms, height, key.Signature, key.Kind, key.Index)
		})
	}
}

type baseHistoryFake struct {
	*baseReceiptSource
	anchor, block, receipts json.RawMessage
	height                  int64
	offline                 bool
}

func (s *baseHistoryFake) ReadBaseHistoryHead(context.Context) (json.RawMessage, error) {
	return s.block, nil
}
func (s *baseHistoryFake) ReadBaseHistoryBlock(_ context.Context, height int64) (json.RawMessage, error) {
	if height == s.height-1 {
		return s.anchor, nil
	}
	return nil, fmt.Errorf("unexpected height %d", height)
}
func (s *baseHistoryFake) ReadBaseBlockReceipts(context.Context, int64) (json.RawMessage, error) {
	return s.receipts, nil
}
func (s *baseHistoryFake) ReadBaseBlockCalls(context.Context, string) (json.RawMessage, error) {
	return nil, errListenerOffline
}
func (s *baseHistoryFake) ReadBaseEvidence(ctx context.Context, n, hash string) (chainassets.BaseEvidence, error) {
	if s.offline {
		return chainassets.BaseEvidence{}, errListenerOffline
	}
	return s.baseReceiptSource.ReadBaseEvidence(ctx, n, hash)
}

func TestBaseReceiptListenerRecovery(t *testing.T) {
	ctx := context.Background()
	db, _ := userDatabase(t, "base_listen_recover")
	a, key, height := baseListenerOrder(t, db, "base-usdc")
	req, src := baseReceiptFixture(t, "base-usdc")
	var b struct {
		Hash, ParentHash, Number, Timestamp string
		Transactions                        []string
	}
	if err := json.Unmarshal(src.proof.Block, &b); err != nil {
		t.Fatal(err)
	}
	sec, err := strconv.ParseInt(b.Timestamp, 0, 64)
	if err != nil {
		t.Fatal(err)
	}
	anchor, _ := json.Marshal(map[string]any{"number": fmt.Sprintf("0x%x", height-1), "hash": b.ParentHash, "parentHash": b.Hash, "timestamp": fmt.Sprintf("0x%x", sec-1), "transactions": []string{}})
	// Place two supported Transfer logs into the same real receipt. Other
	// transactions here are synthetic failed receipts, not additional payments.
	editBaseJSON(t, &src.proof.Receipt, func(r map[string]any) {
		logs := r["logs"].([]any)
		second := maps.Clone(logs[1].(map[string]any))
		second["logIndex"] = "0x6"
		r["logs"] = append(logs, second)
	})
	var receipts []any
	for i, hash := range b.Transactions {
		if hash == req.Hash {
			receipts = append(receipts, src.proof.Receipt)
			continue
		}
		receipts = append(receipts, map[string]string{"transactionIndex": fmt.Sprintf("0x%x", i), "transactionHash": hash, "blockHash": b.Hash, "blockNumber": b.Number, "status": "0x0", "type": "0x2"})
	}
	raw, err := json.Marshal(receipts)
	if err != nil {
		t.Fatal(err)
	}
	fake := &baseHistoryFake{baseReceiptSource: src, anchor: anchor, block: src.proof.Block, receipts: raw, height: height}
	conn := sqlx.NewSqlConnFromDB(db)
	store := chainreceipt.NewBase(conn, transaction.New(db), fake)
	listener := func() *chainreceipt.BaseListener { return chainreceipt.NewBaseListener(db, fake, store, a) }
	count := func(query string, args ...any) int64 {
		t.Helper()
		var n int64
		if e := db.QueryRow(query, args...).Scan(&n); e != nil {
			t.Fatal(e)
		}
		return n
	}
	if _, err = listener().Scan(ctx, req.AssetID, height); err == nil {
		t.Fatal("late anchor accepted")
	}
	if _, err = listener().Scan(ctx, req.AssetID, height-1); err != nil {
		t.Fatal(err)
	}

	ethOrder := chainreceipt.ReceiveTerms{Key: "eth-stream", Purpose: chainreceipt.ReceiveDeposit, BeneficiaryID: 19, Network: chainassets.BaseSepolia, AssetID: chainassets.BaseSepoliaETH, Atoms: "1", Payer: "0x0000000000000000000000000000000000000001", Recipient: "0x0000000000000000000000000000000000000002", ExpiresAt: time.Now().Add(time.Hour).Unix()}
	ethA := chainreceipt.NewAttribution(conn, []chainassets.Network{{ID: ethOrder.Network, Enabled: true, Testnet: true, Assets: []chainassets.Asset{{ID: ethOrder.AssetID}}}})
	if err = ethA.Register(ctx, ethOrder.Network, ethOrder.Recipient, chainreceipt.SingleUseAddress); err != nil {
		t.Fatal(err)
	}
	if _, err = ethA.Open(ctx, ethOrder); err != nil {
		t.Fatal(err)
	}
	mustExec(t, db, "UPDATE chain_receive_request SET opens_at=?,expires_at=? WHERE request_key=?", sec, sec+1, ethOrder.Key)
	if _, err = listener().Scan(ctx, ethOrder.AssetID, height-1); err != nil {
		t.Fatal(err)
	}
	if _, err = listener().Scan(ctx, ethOrder.AssetID, height-1); !errors.Is(err, errListenerOffline) {
		t.Fatal(err)
	}

	editBaseJSON(t, &fake.block, func(m map[string]any) { m["parentHash"] = b.Hash })
	if _, err = listener().Scan(ctx, req.AssetID, height-1); !errors.Is(err, chainreceipt.ErrInvalidEvidence) {
		t.Fatal("disconnected block accepted", err)
	}
	fake.block = src.proof.Block
	mustExec(t, db, "ALTER TABLE base_receive_candidate ADD CONSTRAINT base_test_insert CHECK (event_index<>6)")
	if _, err = listener().Scan(ctx, req.AssetID, height-1); err == nil {
		t.Fatal("partial candidate commit")
	}
	if count("SELECT COUNT(*) FROM base_receive_candidate") != 0 || count("SELECT MAX(height) FROM base_receive_cursor") != height-1 {
		t.Fatal("cursor/first candidate survived rollback")
	}
	mustExec(t, db, "ALTER TABLE base_receive_candidate DROP CHECK base_test_insert")

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, e := listener().Scan(ctx, req.AssetID, height+1); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	if count("SELECT COUNT(*) FROM base_receive_candidate") != 2 || count("SELECT height FROM base_receive_cursor WHERE asset_id=?", ethOrder.AssetID) != height-1 {
		t.Fatal("independent scans or event deduplication failed")
	}
	fake.offline = true
	if _, err = listener().Process(ctx, time.Minute); !errors.Is(err, errListenerOffline) {
		t.Fatal(err)
	}
	if count("SELECT COUNT(*) FROM base_receive_candidate WHERE status='pending'") != 2 {
		t.Fatal("lost candidate on outage")
	}
	fake.offline = false
	// Simulate shutdown after observing one event but before attributing it.
	if _, err = store.Record(ctx, req); err != nil {
		t.Fatal(err)
	}
	fake.offline = true
	mustExec(t, db, "UPDATE base_receive_candidate SET next_try=UTC_TIMESTAMP(6)")
	if _, err = listener().Process(ctx, time.Minute); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, e := listener().Process(ctx, time.Minute); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	order, err := a.Find(ctx, "attempt-a")
	if err != nil || order.Event == nil || *order.Event != key || count("SELECT COUNT(*) FROM base_receive_candidate WHERE status='matched'") != 1 || count("SELECT COUNT(*) FROM base_receive_candidate WHERE status='unmatched'") != 1 || count("SELECT COUNT(*) FROM chain_transfer_event") != 2 {
		t.Fatal(order, err)
	}
	for _, table := range []string{"ledger_voucher", "event_outbox", "user_account"} {
		if count("SELECT COUNT(*) FROM "+table) != 0 {
			t.Fatal(table)
		}
	}
}

func TestBaseReceiptListenerMigration(t *testing.T) {
	name, dsn, db, err := testRun.NewDatabase(context.Background(), "base_listen_migrate")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	m, err := migration.Open(dsn, name, "../../manifest/sql/diff/business", "business")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if err = m.Migrate(34); err != nil {
		t.Fatal(err)
	}
	old := recordCollectionFixture(t, db, "base-internal-eth")
	for _, v := range []uint{35, 34, 35} {
		if err = m.Migrate(v); err != nil {
			t.Fatal(err)
		}
	}
	var hash string
	if err = db.QueryRow("SELECT signature FROM chain_observed_transaction WHERE call_trace_json IS NOT NULL").Scan(&hash); err != nil || hash != old.Transaction.Signature {
		t.Fatal(hash, err)
	}
	mustExec(t, db, "INSERT INTO base_receive_cursor(asset_id,height,block_hash) VALUES(?,?,?)", old.Event.AssetID, old.Transaction.Slot, old.Transaction.BlockHash)
	if err = m.Migrate(34); err == nil || !strings.Contains(err.Error(), "base_receive_rollback_guard") {
		t.Fatal("unsafe downgrade", err)
	}
}
