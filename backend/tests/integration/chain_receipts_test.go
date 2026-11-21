//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainreceipt"
	"github.com/0xabcd-k/DexWallet/backend/transaction"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

type receiptFixture struct {
	Request  chainreceipt.Request
	Evidence chainassets.SolanaEvidence
}
type receiptSource struct {
	catalog []chainassets.Network
	proof   chainassets.SolanaEvidence
}

func (s *receiptSource) Catalog() []chainassets.Network { return s.catalog }
func (s *receiptSource) ReadSolanaEvidence(context.Context, string, string) (chainassets.SolanaEvidence, error) {
	return s.proof, nil
}
func receiptFixtureSource(t *testing.T, name string) (chainreceipt.Request, *receiptSource) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("../../logic/chainreceipt/testdata", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var f receiptFixture
	if err = json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	assets, err := chainassets.New(chainassets.Config{Networks: []chainassets.Endpoint{{Network: chainassets.SolanaDevnet, URL: "http://127.0.0.1", TestUSDTContract: "2ydN2Tr1FhRmJJcdqATsFCq5Q1t43bfRQD5tKfSxL7d5"}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer assets.Close()
	return f.Request, &receiptSource{assets.Catalog(), f.Evidence}
}
func editReceipt(t *testing.T, s *receiptSource, edit func(map[string]any)) {
	t.Helper()
	var m map[string]any
	d := json.NewDecoder(bytes.NewReader(s.proof.Transaction))
	d.UseNumber()
	if err := d.Decode(&m); err != nil {
		t.Fatal(err)
	}
	edit(m)
	s.proof.Transaction, _ = json.Marshal(m)
}

type receiptCommitLost struct {
	base    *transaction.Runner
	persist bool
}

func (tx receiptCommitLost) TransactCtx(ctx context.Context, fn func(context.Context, sqlx.Session) error) error {
	if tx.persist {
		if err := tx.base.TransactCtx(ctx, fn); err != nil {
			return err
		}
	}
	return &transaction.Failure{Phase: "commit", Cause: io.ErrUnexpectedEOF}
}
func TestChainReceiptSchemaAndPersistence(t *testing.T) {
	ctx := context.Background()
	db, _ := userDatabase(t, "chain_receipts")
	conn := sqlx.NewSqlConnFromDB(db)
	runner := transaction.New(db)
	_, _, full, err := testRun.NewDatabase(ctx, "chain_receipts_full")
	if err != nil {
		t.Fatal(err)
	}
	defer full.Close()
	for _, name := range []string{"tables.sql", "constraints.sql"} {
		b, e := os.ReadFile(filepath.Join("../../manifest/sql/ddl/chainreceipt", name))
		if e != nil {
			t.Fatal(e)
		}
		mustExec(t, full, string(b))
	}
	for _, name := range []string{"chain_observed_transaction", "chain_transfer_event", "chain_receive_address", "chain_receive_request"} {
		var table, a, b string
		if err = db.QueryRow("SHOW CREATE TABLE "+name).Scan(&table, &a); err != nil {
			t.Fatal(err)
		}
		if err = full.QueryRow("SHOW CREATE TABLE "+name).Scan(&table, &b); err != nil {
			t.Fatal(err)
		}
		if a != b {
			t.Fatalf("DDL differs %s\n%s\n%s", name, a, b)
		}
	}
	funds := []string{"user_account", "ledger_voucher", "ledger_entry", "account_hold", "transfer_order", "event_outbox"}
	before := map[string]int{}
	for _, table := range funds {
		var count int
		if err = db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		before[table] = count
	}
	request, source := receiptFixtureSource(t, "sol")
	// A synthetic two-transfer message tests event identity, using public historical
	// fixtures. This fake is never submitted to a chain or presented as a live tx.
	editReceipt(t, source, func(m map[string]any) {
		msg := m["transaction"].(map[string]any)["message"].(map[string]any)
		instructions := msg["instructions"].([]any)
		raw, _ := json.Marshal(instructions[0])
		var next map[string]any
		_ = json.Unmarshal(raw, &next)
		next["parsed"].(map[string]any)["info"].(map[string]any)["lamports"] = 2000
		msg["instructions"] = append(instructions, next)
	})
	service := chainreceipt.New(conn, runner, source)
	var wait sync.WaitGroup
	var created atomic.Int64
	failures := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wait.Add(1)
		go func(i int) {
			defer wait.Done()
			r := request
			if i%2 == 1 {
				r.Instruction = 1
				r.Atoms = "2000"
			}
			saved, e := service.Record(ctx, r)
			if e != nil {
				failures <- e
				return
			}
			if !saved.Reused {
				created.Add(1)
			}
		}(i)
	}
	wait.Wait()
	close(failures)
	for e := range failures {
		t.Error(e)
	}
	if t.Failed() {
		return
	}
	if created.Load() != 2 {
		t.Fatalf("created %d", created.Load())
	}
	var txs, events int
	var fee string
	if err = db.QueryRow("SELECT COUNT(*),MAX(fee_atoms) FROM chain_observed_transaction").Scan(&txs, &fee); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow("SELECT COUNT(*) FROM chain_transfer_event").Scan(&events); err != nil {
		t.Fatal(err)
	}
	if txs != 1 || events != 2 || fee != "10000" {
		t.Fatal(txs, events, fee)
	}
	// JSON ordering and later observation context don't mutate persisted evidence.
	var original string
	if err = db.QueryRow("SELECT evidence_json FROM chain_observed_transaction").Scan(&original); err != nil {
		t.Fatal(err)
	}
	source.proof.Status = json.RawMessage(`{"value":[{"confirmationStatus":"finalized","err":null,"slot":517805174}],"context":{"slot":999999999}}`)
	saved, err := service.Record(ctx, request)
	if err != nil || !saved.Reused {
		t.Fatal(saved, err)
	}
	var after string
	_ = db.QueryRow("SELECT evidence_json FROM chain_observed_transaction").Scan(&after)
	if after != original {
		t.Fatal("first evidence replaced")
	}
	// A changed second instruction must conflict even when the selected first one matches.
	editReceipt(t, source, func(m map[string]any) {
		m["transaction"].(map[string]any)["message"].(map[string]any)["instructions"].([]any)[1].(map[string]any)["parsed"].(map[string]any)["info"].(map[string]any)["lamports"] = 2001
	})
	if _, err = service.Record(ctx, request); !errors.Is(err, chainreceipt.ErrConflict) {
		t.Fatal(err)
	}
	token, tokenSource := receiptFixtureSource(t, "usdc")
	recovered, err := chainreceipt.New(conn, receiptCommitLost{runner, true}, tokenSource).Record(ctx, token)
	if err != nil || !recovered.Reused || recovered.Receipt.Event.Atoms != "10000" {
		t.Fatal(recovered, err)
	}
	fixture, fixtureSource := receiptFixtureSource(t, "usdt-fixture")
	if _, err = chainreceipt.New(conn, receiptCommitLost{runner, false}, fixtureSource).Record(ctx, fixture); !errors.Is(err, chainreceipt.ErrOutcomeUnknown) {
		t.Fatal(err)
	}
	if _, err = chainreceipt.New(conn, runner, fixtureSource).Record(ctx, fixture); err != nil {
		t.Fatal(err)
	}
	for _, table := range funds {
		var count int
		if err = db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != before[table] {
			t.Fatalf("funds table %s changed", table)
		}
	}
	down, err := os.ReadFile("../../manifest/sql/diff/business/000016_chain_receipts.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, string(down)); err == nil {
		t.Fatal("removed stored receipt evidence")
	}
	if err = db.QueryRow("SELECT COUNT(*) FROM chain_observed_transaction").Scan(&txs); err != nil || txs != 3 {
		t.Fatal(txs, err)
	}
	t.Log("current receipt DDL, concurrent event dedup, single tx fee, immutable evidence, conflict, commit recovery, rollback guard and unchanged funds verified")
}
