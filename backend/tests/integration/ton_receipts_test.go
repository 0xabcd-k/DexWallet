//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/0xabcd-k/DexWallet/backend/internal/migration"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainreceipt"
	"github.com/0xabcd-k/DexWallet/backend/transaction"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

type tonReceiptSource struct {
	catalog []chainassets.Network
	proof   chainassets.TONEvidence
}

func (s *tonReceiptSource) Catalog() []chainassets.Network { return s.catalog }
func (s *tonReceiptSource) ReadTONEvidence(context.Context, string, string, string, string) (chainassets.TONEvidence, error) {
	return s.proof, nil
}
func tonReceiptFixture(t *testing.T, name string) (chainreceipt.TONRequest, *tonReceiptSource) {
	t.Helper()
	b, err := os.ReadFile("../../logic/chainreceipt/testdata/" + name + ".json")
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Request  chainreceipt.TONRequest
		Evidence chainassets.TONEvidence
	}
	if err = json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	a, err := chainassets.New(chainassets.Config{Networks: []chainassets.Endpoint{{Network: chainassets.TONTestnet, URL: "http://127.0.0.1", TestUSDTContract: "kQD0GKBM8ZbryVk2aESmzfU6b9b_8era_IkvBSELujFZPsyy"}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	return f.Request, &tonReceiptSource{a.Catalog(), f.Evidence}
}
func TestTONReceiptMigrationPersistenceAndRecovery(t *testing.T) {
	ctx := context.Background()
	name, dsn, db, err := testRun.NewDatabase(ctx, "ton_receipts")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	m, err := migration.Open(dsn, name, "../../manifest/sql/diff/business", "business")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if err = m.Migrate(17); err != nil {
		t.Fatal(err)
	}
	conn := sqlx.NewSqlConnFromDB(db)
	runner := transaction.New(db)
	sol, solSource := receiptFixtureSource(t, "sol")
	base, baseSource := baseReceiptFixture(t, "base-usdc")
	tron, tronSource := tronReceiptFixture(t, "tron-usdt")
	records := []func() (chainreceipt.Stored, error){func() (chainreceipt.Stored, error) { return chainreceipt.New(conn, runner, solSource).Record(ctx, sol) }, func() (chainreceipt.Stored, error) {
		return chainreceipt.NewBase(conn, runner, baseSource).Record(ctx, base)
	}, func() (chainreceipt.Stored, error) {
		return chainreceipt.NewTRON(conn, runner, tronSource).Record(ctx, tron)
	}}
	var originals []chainreceipt.Receipt
	for _, record := range records {
		s, err := record()
		if err != nil || s.Reused {
			t.Fatal(s, err)
		}
		originals = append(originals, s.Receipt)
	}
	for _, version := range []uint{18, 17, 18} {
		if err = m.Migrate(version); err != nil {
			t.Fatal(err)
		}
		for i, record := range records {
			s, err := record()
			if err != nil || !s.Reused || s.Receipt.Transaction != originals[i].Transaction || s.Receipt.Event != originals[i].Event {
				t.Fatal("legacy identity changed", version, err)
			}
		}
	}
	request, source := tonReceiptFixture(t, "ton-jetton")
	service := chainreceipt.NewTON(conn, runner, source)
	var wait sync.WaitGroup
	var created atomic.Int64
	failures := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			s, err := service.Record(ctx, request)
			if err != nil {
				failures <- err
			} else if !s.Reused {
				created.Add(1)
			}
		}()
	}
	wait.Wait()
	close(failures)
	for err := range failures {
		t.Error(err)
	}
	if t.Failed() {
		return
	}
	if created.Load() != 1 {
		t.Fatal(created.Load())
	}
	var txs, events int
	var fee, original string
	if err = db.QueryRow("SELECT COUNT(*),MAX(fee_atoms),MAX(evidence_json) FROM chain_observed_transaction WHERE network='ton-testnet'").Scan(&txs, &fee, &original); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow("SELECT COUNT(*) FROM chain_transfer_event WHERE event_kind='ton_jetton'").Scan(&events); err != nil || txs != 1 || events != 1 || fee != "849136" {
		t.Fatal(txs, events, fee, err)
	}
	found, err := service.Find(ctx, request.Network, strings.ToUpper(request.Hash), chainreceipt.TONJetton)
	if err != nil || len(found.Event.RecipientAccount) != 66 || len(found.Event.Mint) != 66 || found.Event.Atoms != "1000000" {
		t.Fatal(found, err)
	}
	editBaseJSON(t, &source.proof.Masterchain, func(m map[string]any) { m["result"].(map[string]any)["last"].(map[string]any)["seqno"] = 999999999 })
	reused, err := service.Record(ctx, request)
	if err != nil || !reused.Reused {
		t.Fatal(reused, err)
	}
	var after string
	if err = db.QueryRow("SELECT evidence_json FROM chain_observed_transaction WHERE network=? AND signature=?", request.Network, request.Hash).Scan(&after); err != nil || after != original {
		t.Fatal("evidence replaced", err)
	}
	editBaseJSON(t, &source.proof.Transaction, func(m map[string]any) { m["transactions"].([]any)[0].(map[string]any)["total_fees"] = "738378" })
	if _, err = service.Record(ctx, request); !errors.Is(err, chainreceipt.ErrConflict) {
		t.Fatal(err)
	}
	native, nativeSource := tonReceiptFixture(t, "ton-native")
	if _, err = chainreceipt.NewTON(conn, receiptCommitLost{runner, false}, nativeSource).Record(ctx, native); !errors.Is(err, chainreceipt.ErrOutcomeUnknown) {
		t.Fatal(err)
	}
	recovered, err := chainreceipt.NewTON(conn, receiptCommitLost{runner, true}, nativeSource).Record(ctx, native)
	if err != nil || !recovered.Reused || recovered.Receipt.Event.Kind != chainreceipt.TONIncoming {
		t.Fatal(recovered, err)
	}
	inactive, inactiveSource := tonReceiptFixture(t, "ton-no-state")
	if _, err = chainreceipt.NewTON(conn, runner, inactiveSource).Record(ctx, inactive); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"user_account", "ledger_voucher", "ledger_entry", "account_hold", "transfer_order", "event_outbox"} {
		var count int
		if err = db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil || count != 0 {
			t.Fatal(table, count, err)
		}
	}
	if err = m.Migrate(17); err == nil || !strings.Contains(err.Error(), "ton_receipt_rollback_guard") {
		t.Fatal("TON evidence rollback allowed", err)
	}
}
