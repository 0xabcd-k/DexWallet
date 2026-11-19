//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainreceipt"
	"github.com/0xabcd-k/DexWallet/backend/transaction"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

type tronReceiptSource struct {
	catalog []chainassets.Network
	proof   chainassets.TRONEvidence
}

func (s *tronReceiptSource) Catalog() []chainassets.Network { return s.catalog }
func (s *tronReceiptSource) ReadTRONEvidence(context.Context, string, string) (chainassets.TRONEvidence, error) {
	return s.proof, nil
}
func tronReceiptFixture(t *testing.T, name string) (chainreceipt.TRONRequest, *tronReceiptSource) {
	t.Helper()
	b, err := os.ReadFile("../../logic/chainreceipt/testdata/" + name + ".json")
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Request  chainreceipt.TRONRequest
		Evidence chainassets.TRONEvidence
	}
	if err = json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	contract := ""
	if parts := strings.Split(f.Request.AssetID, ":"); len(parts) == 3 {
		contract = parts[2]
	}
	assets, err := chainassets.New(chainassets.Config{Networks: []chainassets.Endpoint{{Network: chainassets.TRONNile, URL: "http://127.0.0.1", TestUSDTContract: contract}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer assets.Close()
	return f.Request, &tronReceiptSource{assets.Catalog(), f.Evidence}
}
func TestTRONReceiptPersistenceAndRecovery(t *testing.T) {
	ctx := context.Background()
	db, _ := userDatabase(t, "tron_receipts")
	conn := sqlx.NewSqlConnFromDB(db)
	runner := transaction.New(db)
	r, source := tronReceiptFixture(t, "tron-usdt")
	// Synthetic two-log receipt tests identity and transaction-wide resources.
	// This fake is never broadcast or reported as historical chain evidence.
	editBaseJSON(t, &source.proof.Info, func(m map[string]any) {
		logs := m["log"].([]any)
		raw, _ := json.Marshal(logs[0])
		var next map[string]any
		_ = json.Unmarshal(raw, &next)
		next["data"] = fmt.Sprintf("%064x", 20000)
		m["log"] = append(logs, next)
		m["fee"] = 1200
		m["receipt"].(map[string]any)["net_fee"] = 1200
	})
	second := 1
	other := r
	other.LogIndex = &second
	other.Atoms = "20000"
	service := chainreceipt.NewTRON(conn, runner, source)
	var wait sync.WaitGroup
	var created atomic.Int64
	failures := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wait.Add(1)
		go func(i int) {
			defer wait.Done()
			request := r
			if i%2 != 0 {
				request = other
			}
			saved, err := service.Record(ctx, request)
			if err != nil {
				failures <- err
			} else if !saved.Reused {
				created.Add(1)
			}
		}(i)
	}
	wait.Wait()
	close(failures)
	for err := range failures {
		t.Error(err)
	}
	if t.Failed() {
		return
	}
	if created.Load() != 2 {
		t.Fatal(created.Load())
	}
	var txs, events int
	var fee, original string
	if err := db.QueryRow("SELECT COUNT(*),MAX(fee_atoms),MAX(evidence_json) FROM chain_observed_transaction WHERE network='tron-nile'").Scan(&txs, &fee, &original); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM chain_transfer_event WHERE event_kind='tron_log'").Scan(&events); err != nil {
		t.Fatal(err)
	}
	if txs != 1 || events != 2 || fee != "1200" {
		t.Fatal(txs, events, fee)
	}
	found, err := service.Find(ctx, r.Network, strings.ToUpper(r.Hash), chainreceipt.TRONLog, 1)
	if err != nil || found.Event.Atoms != "20000" || found.Transaction.FeeAtoms != "1200" {
		t.Fatal(found, err)
	}
	editBaseJSON(t, &source.proof.SolidHead, func(m map[string]any) {
		m["blockID"] = fmt.Sprintf("%016x%s", 100000000, strings.Repeat("a", 48))
		m["block_header"].(map[string]any)["raw_data"].(map[string]any)["number"] = 100000000
	})
	reused, err := service.Record(ctx, r)
	if err != nil || !reused.Reused {
		t.Fatal(reused, err)
	}
	var after string
	if err = db.QueryRow("SELECT evidence_json FROM chain_observed_transaction WHERE network=? AND signature=?", r.Network, r.Hash).Scan(&after); err != nil || after != original {
		t.Fatal("original evidence replaced", err)
	}
	// Another event's change is also a parent conflict, not a second fee/event.
	editBaseJSON(t, &source.proof.Info, func(m map[string]any) { m["log"].([]any)[1].(map[string]any)["data"] = fmt.Sprintf("%064x", 20001) })
	if _, err = service.Record(ctx, r); !errors.Is(err, chainreceipt.ErrConflict) {
		t.Fatal(err)
	}
	native, nativeSource := tronReceiptFixture(t, "tron-trx")
	if _, err = chainreceipt.NewTRON(conn, receiptCommitLost{runner, false}, nativeSource).Record(ctx, native); !errors.Is(err, chainreceipt.ErrOutcomeUnknown) {
		t.Fatal(err)
	}
	recovered, err := chainreceipt.NewTRON(conn, receiptCommitLost{runner, true}, nativeSource).Record(ctx, native)
	if err != nil || !recovered.Reused || recovered.Receipt.Event.Kind != chainreceipt.TRONTransfer {
		t.Fatal(recovered, err)
	}
	fixture, fixtureSource := tronReceiptFixture(t, "tron-usdt-fixture")
	if _, err = chainreceipt.NewTRON(conn, runner, fixtureSource).Record(ctx, fixture); err != nil {
		t.Fatal(err)
	}
	editBaseJSON(t, &fixtureSource.proof.Info, func(m map[string]any) { resource := m["receipt"].(map[string]any); resource["net_usage"] = 346 })
	if _, err = chainreceipt.NewTRON(conn, runner, fixtureSource).Record(ctx, fixture); !errors.Is(err, chainreceipt.ErrConflict) {
		t.Fatal("resource facts changed", err)
	}
	for _, table := range []string{"user_account", "ledger_voucher", "ledger_entry", "account_hold", "transfer_order", "event_outbox"} {
		var count int
		if err = db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil || count != 0 {
			t.Fatal(table, count, err)
		}
	}
}
