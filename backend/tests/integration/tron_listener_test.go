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
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/config"
	"github.com/0xabcd-k/DexWallet/backend/internal/migration"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainreceipt"
	"github.com/0xabcd-k/DexWallet/backend/svc"
)

func nileListenerOrder(t *testing.T, db *sql.DB, name string) (*chainreceipt.Attribution, chainreceipt.EventKey, chainreceipt.ReceiveTerms, int64, *tronReceiptSource) {
	t.Helper()
	req, src := tronReceiptFixture(t, name)
	ctx := context.Background()
	r, err := chainreceipt.NewTRONVerifier(src).Verify(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	var info struct{ BlockTimeStamp int64 }
	if err = json.Unmarshal(src.proof.Info, &info); err != nil {
		t.Fatal(err)
	}
	a, terms, key := collectionSetup(db, r)
	if err = a.Register(ctx, terms.Network, terms.Recipient, chainreceipt.SingleUseAddress); err != nil {
		t.Fatal(err)
	}
	if _, err = a.Open(ctx, terms); err != nil {
		t.Fatal(err)
	}
	// Restore the order window only. No chain receipt or event position is seeded.
	at := info.BlockTimeStamp / 1000
	mustExec(t, db, "UPDATE chain_receive_request SET opens_at=?, expires_at=? WHERE request_key=?", at, at+2, terms.Key)
	return a, key, terms, r.Transaction.Slot, src
}

func TestLiveTRONReceiptListener(t *testing.T) {
	if os.Getenv("DEXWALLET_TEST_TRON_LISTENER_LIVE") != "1" {
		t.Skip("opt-in read-only Nile solid history")
	}
	for i, name := range []string{"tron-trx", "tron-usdt", "tron-usdt-fixture"} {
		t.Run(name, func(t *testing.T) {
			db, _ := userDatabase(t, fmt.Sprintf("nile_listen_live_%d", i))
			a, expected, terms, height, _ := nileListenerOrder(t, db, name)
			cfg := chainassets.Config{Timeout: 18 * time.Second, Networks: []chainassets.Endpoint{{Network: chainassets.TRONNile, URL: "https://nile.trongrid.io"}}}
			cfg.LoadEnvironment()
			if parts := strings.Split(terms.AssetID, ":"); len(parts) == 3 {
				cfg.Networks[0].TestUSDTContract = parts[2]
			}
			source, err := chainassets.New(cfg, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer source.Close()
			policy := chainreceipt.TRONListenerConfig{AnchorHeight: height - 2, PollInterval: 2 * time.Second, OperationTimeout: 25 * time.Second}
			shared := svc.ServiceContext{DB: db, ChainAssets: source, Config: config.Config{TRONReceiptListener: &policy}}
			ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
			defer cancel()
			if err = shared.StartBackground(ctx); err != nil {
				t.Fatal(err)
			}
			defer func() {
				shared.StopBackground()
				wait, stop := context.WithTimeout(context.Background(), 5*time.Second)
				defer stop()
				if err := shared.WaitBackground(wait); err != nil {
					t.Error(err)
				}
			}()
			for {
				order, e := a.Find(ctx, terms.Key)
				if e != nil {
					t.Fatal(e)
				}
				if order.Event != nil {
					if *order.Event != expected || order.Atoms != terms.Atoms {
						t.Fatal(order)
					}
					t.Logf("matched %s atoms=%s height=%d tx=%s %s/%d", terms.AssetID, order.Atoms, height, expected.Signature, expected.Kind, expected.Index)
					break
				}
				select {
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				case <-time.After(time.Second):
				}
			}
		})
	}
}

type nileHistoryFixture struct {
	*tronReceiptSource
	block       chainassets.NileHistory
	anchor      json.RawMessage
	height      int64
	beforeBlock func(context.Context) error
}

func (f *nileHistoryFixture) ReadNileHistory(ctx context.Context, height int64, receipts bool) (chainassets.NileHistory, error) {
	h := f.block
	switch height {
	case f.height:
		if receipts && f.beforeBlock != nil {
			if err := f.beforeBlock(ctx); err != nil {
				return h, err
			}
		}
	case f.height - 1:
		h.Block = f.anchor
	case f.height + 1:
		h.Block = nil
	default:
		return h, fmt.Errorf("unexpected height %d", height)
	}
	if !receipts {
		h.Infos = nil
	}
	return h, nil
}

func TestTRONReceiptListenerAtomicRecovery(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	db, _ := userDatabase(t, "nile_listen_atomic")
	a, key, terms, height, src := nileListenerOrder(t, db, "tron-usdt")
	var ref struct {
		ParentHash string
		Timestamp  int64
	}
	var header struct {
		Header struct {
			Raw json.RawMessage `json:"raw_data"`
		} `json:"block_header"`
	}
	if err := json.Unmarshal(src.proof.Block, &header); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(header.Header.Raw, &ref); err != nil {
		t.Fatal(err)
	}
	anchor, err := json.Marshal(map[string]any{"blockID": ref.ParentHash, "block_header": map[string]any{"raw_data": map[string]any{"number": height - 1, "timestamp": ref.Timestamp - 3000, "parentHash": fmt.Sprintf("%016x%048x", height-2, 1)}}})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("../../logic/chainreceipt/testdata/tron-usdt-block-infos.json")
	if err != nil {
		t.Fatal(err)
	}
	h := chainassets.NileHistory{Genesis: src.proof.Genesis, SolidHead: src.proof.Block, Block: src.proof.Block}
	if err = json.Unmarshal(raw, &h.Infos); err != nil {
		t.Fatal(err)
	}
	slices.Reverse(h.Infos)
	// Add a log from a different, reverted trigger to the same recipient. It
	// must not become an observed receipt even when the RPC supplies a log.
	var reverted string
	editBaseJSON(t, &h.Block, func(m map[string]any) {
		for _, member := range m["transactions"].([]any) {
			tx := member.(map[string]any)
			contract := tx["raw_data"].(map[string]any)["contract"].([]any)[0].(map[string]any)
			if tx["txID"] != key.Signature && contract["type"] == "TriggerSmartContract" {
				reverted = tx["txID"].(string)
				tx["ret"].([]any)[0].(map[string]any)["contractRet"] = "REVERT"
				return
			}
		}
	})
	h.SolidHead = h.Block
	if reverted == "" {
		t.Fatal("missing trigger for revert case")
	}
	var target struct{ Log json.RawMessage }
	if err = json.Unmarshal(src.proof.Info, &target); err != nil {
		t.Fatal(err)
	}

	for i := range h.Infos {
		var id struct{ ID string }
		if err = json.Unmarshal(h.Infos[i], &id); err != nil {
			t.Fatal(err)
		}
		if id.ID == reverted {
			editBaseJSON(t, &h.Infos[i], func(m map[string]any) {
				m["result"] = "FAILED"
				m["receipt"].(map[string]any)["result"] = "REVERT"
				m["log"] = target.Log
			})
		}
		if id.ID == key.Signature {
			editBaseJSON(t, &h.Infos[i], func(m map[string]any) {
				transfer := m["log"].([]any)[0].(map[string]any)
				other := maps.Clone(transfer)
				other["address"] = strings.Repeat("ab", 20)
				short := maps.Clone(transfer)
				short["data"] = fmt.Sprintf("%064x", 1)
				m["log"] = []any{other, transfer, transfer, short}
			})
		}
	}
	fake := &nileHistoryFixture{tronReceiptSource: src, block: h, anchor: anchor, height: height}
	scan := func(anchor int64) (bool, error) {
		return chainreceipt.NewTRONListener(db, fake).Scan(ctx, terms.AssetID, anchor)
	}
	if _, err = scan(height); err == nil {
		t.Fatal("late anchor accepted")
	}
	if busy, e := scan(height - 1); e != nil || !busy {
		t.Fatal(busy, e)
	}
	count := func(query string) int64 {
		t.Helper()
		var n int64
		if err := db.QueryRow(query).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	assertPending := func() {
		t.Helper()
		r, e := a.Find(ctx, terms.Key)
		if e != nil || r.Event != nil || count("SELECT height FROM tron_receive_cursor") != height-1 || count("SELECT COUNT(*) FROM chain_transfer_event") != 0 || count("SELECT COUNT(*) FROM chain_observed_transaction") != 0 {
			t.Fatalf("partial block committed: %+v %v", r, e)
		}
	}
	fake.beforeBlock = func(context.Context) error { return errListenerOffline }
	if _, err = scan(height + 8); !errors.Is(err, errListenerOffline) {
		t.Fatal("changed anchor after restart or swallowed RPC error", err)
	}
	fake.beforeBlock = nil
	for _, breakHistory := range []func(*chainassets.NileHistory){
		func(h *chainassets.NileHistory) { h.Genesis = []byte("{}") },
		func(h *chainassets.NileHistory) { h.Infos = h.Infos[1:] },
		func(h *chainassets.NileHistory) { h.Infos = slices.Clone(h.Infos); h.Infos[1] = h.Infos[0] },
		func(h *chainassets.NileHistory) {
			editBaseJSON(t, &h.Block, func(m map[string]any) {
				m["block_header"].(map[string]any)["raw_data"].(map[string]any)["parentHash"] = strings.Repeat("00", 32)
			})
		},
	} {
		fake.block = h
		breakHistory(&fake.block)
		if _, err = scan(height + 8); !errors.Is(err, chainreceipt.ErrInvalidEvidence) {
			t.Fatal(err)
		}
		assertPending()
	}
	fake.block = h
	mustExec(t, db, "ALTER TABLE chain_transfer_event ADD CONSTRAINT ck_simulate_second_log_failure CHECK (instruction_index<>2)")
	if _, err = scan(height + 8); err == nil || !strings.Contains(err.Error(), "ck_simulate_second_log_failure") {
		t.Fatal(err)
	}
	assertPending()
	mustExec(t, db, "ALTER TABLE chain_transfer_event DROP CHECK ck_simulate_second_log_failure")

	arrived := make(chan struct{}, 2)
	release := make(chan struct{})
	fake.beforeBlock = func(ctx context.Context) error {
		arrived <- struct{}{}
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	type result struct {
		busy bool
		err  error
	}
	results := make(chan result, 2)
	for i := 0; i < 2; i++ {
		go func() { busy, e := scan(height + 8); results <- result{busy, e} }()
	}
	for i := 0; i < 2; i++ {
		select {
		case <-arrived:
		case <-ctx.Done():
			close(release)
			t.Fatal(ctx.Err())
		}
	}
	close(release)
	first, second := <-results, <-results
	if first.err != nil || second.err != nil || first.busy == second.busy {
		t.Fatal(first, second)
	}
	key.Index = 1
	order, err := a.Find(ctx, terms.Key)
	if err != nil || order.Event == nil || *order.Event != key || count("SELECT height FROM tron_receive_cursor") != height || count("SELECT COUNT(*) FROM chain_transfer_event") != 3 || count("SELECT COUNT(*) FROM chain_observed_transaction") != 1 {
		t.Fatal(order, err)
	}
	if busy, e := scan(height - 1); e != nil || busy {
		t.Fatal("head replay", busy, e)
	}
	for _, table := range []string{"event_outbox", "ledger_voucher", "user_account"} {
		if count("SELECT COUNT(*) FROM "+table) != 0 {
			t.Fatal(table)
		}
	}
}

func TestTRONReceiptListenerMigration(t *testing.T) {
	name, dsn, db, err := testRun.NewDatabase(context.Background(), "nile_listen_migrate")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	m, err := migration.Open(dsn, name, "../../manifest/sql/diff/business", "business")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if err = m.Migrate(35); err != nil {
		t.Fatal(err)
	}
	old := recordCollectionFixture(t, db, "tron-trx")
	for _, v := range []uint{36, 35, 36} {
		if err = m.Migrate(v); err != nil {
			t.Fatal(err)
		}
	}
	var hash string
	if err = db.QueryRow("SELECT signature FROM chain_observed_transaction").Scan(&hash); err != nil || hash != old.Transaction.Signature {
		t.Fatal(hash, err)
	}
	mustExec(t, db, "INSERT INTO tron_receive_cursor(asset_id,height,block_hash) VALUES(?,?,?)", old.Event.AssetID, old.Transaction.Slot, old.Transaction.BlockHash)
	if err = m.Migrate(35); err == nil || !strings.Contains(err.Error(), "tron_receive_rollback_guard") {
		t.Fatal("unsafe downgrade", err)
	}
}
