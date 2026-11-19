//go:build integration

package integration

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"io"
	"os"
	"sync"
	"testing"

	"github.com/0xabcd-k/DexWallet/backend/internal/migration"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainreceipt"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainsend"
	model "github.com/0xabcd-k/DexWallet/backend/model/chainsend"
	"github.com/0xabcd-k/DexWallet/backend/transaction"
	"github.com/mr-tron/base58"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

type payoutRPC struct {
	block  chainassets.SolanaBlock
	height int64
	submit func([]byte) error
}

func (p *payoutRPC) LatestSolanaBlock(context.Context) (chainassets.SolanaBlock, error) {
	return p.block, nil
}
func (p *payoutRPC) SolanaBlockHeight(context.Context) (int64, error)  { return p.height, nil }
func (p *payoutRPC) SubmitSolana(_ context.Context, wire []byte) error { return p.submit(wire) }

type payoutCapture struct {
	Intent   model.SolanaPayout
	Receipt  chainreceipt.Receipt
	Evidence chainassets.SolanaEvidence
}

func solPayoutCapture(t *testing.T) payoutCapture {
	t.Helper()
	raw, err := os.ReadFile("../../logic/chainreceipt/testdata/sol-payout.json")
	if err != nil {
		t.Fatal(err)
	}
	var c payoutCapture
	if err = json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	return c
}
func TestSolanaPayoutRecovery(t *testing.T) {
	ctx := context.Background()
	db, _ := userDatabase(t, "sol_recovery")
	conn := sqlx.NewSqlConnFromDB(db)
	capture := solPayoutCapture(t)
	block := chainassets.SolanaBlock{Blockhash: capture.Intent.Blockhash, LastValidBlockHeight: capture.Intent.LastValidHeight}
	node := &payoutRPC{block: block, height: block.LastValidBlockHeight}
	_, source := receiptFixtureSource(t, "sol")
	receipts := chainreceipt.New(conn, transaction.New(db), source)
	newService := func() *chainsend.Solana {
		s, err := chainsend.NewSolana(conn, capture.Intent.Sender, node, receipts)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	s := newService()
	original := capture.Intent
	prepare := func(key, amount string) (*model.SolanaPayout, error) {
		return s.Prepare(ctx, key, original.Recipient, amount)
	}
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := prepare(original.RequestKey, original.Lamports)
			if err != nil || r.MessageHash != original.MessageHash {
				t.Errorf("concurrent prepare: %v %v", r, err)
			}
		}()
	}
	wg.Wait()
	if _, err := prepare(original.RequestKey, "1"); !errors.Is(err, chainsend.ErrConflict) {
		t.Fatal(err)
	}
	if _, err := prepare("other", original.Lamports); !errors.Is(err, chainsend.ErrConflict) {
		t.Fatal("same message would identify two payouts", err)
	}
	if _, err := s.Step(ctx, original.RequestKey); !errors.Is(err, chainsend.ErrSolanaSignature) {
		t.Fatal(err)
	}
	_, private, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Bind(ctx, original.RequestKey, base58.Encode(ed25519.Sign(private, []byte("different payment")))); !errors.Is(err, chainsend.ErrSolanaSignature) {
		t.Fatal(err)
	}
	if _, err = s.Bind(ctx, original.RequestKey, original.Signature); err != nil {
		t.Fatal(err)
	}
	source.proof = chainassets.SolanaEvidence{Transaction: json.RawMessage("null"), Status: json.RawMessage(`{"context":{"slot":50},"value":[null]}`)}
	var first []byte
	count := 0
	node.submit = func(wire []byte) error {
		count++
		r, err := s.Find(ctx, original.RequestKey)
		if err != nil || r.Signature != original.Signature || r.Attempts != int64(count) {
			t.Fatal("missing durable broadcast authorization", r, err)
		}
		if count == 1 {
			first = bytes.Clone(wire)
			return io.ErrUnexpectedEOF
		}
		if !bytes.Equal(wire, first) {
			t.Fatal("replay changed signed transaction")
		}
		return nil
	}
	if _, err = s.Step(ctx, original.RequestKey); !errors.Is(err, chainsend.ErrOutcomeUnknown) {
		t.Fatal(err)
	}
	s = newService()
	absent := source.proof
	node.height++
	if _, err = s.Step(ctx, original.RequestKey); !errors.Is(err, chainsend.ErrSolanaExpired) {
		t.Fatal(err)
	}
	// Separate observations model the valid and expired branches of the same authorization.
	node.height--
	source.proof.Status = json.RawMessage(`{"context":{"slot":50},"value":[{"slot":49,"confirmationStatus":"confirmed","err":null}]}`)
	if _, err = s.Step(ctx, original.RequestKey); !errors.Is(err, chainreceipt.ErrUnconfirmed) {
		t.Fatal(err)
	}
	source.proof.Status = json.RawMessage(`{}`)
	if _, err = s.Step(ctx, original.RequestKey); !errors.Is(err, chainreceipt.ErrInvalidEvidence) {
		t.Fatal(err)
	}
	source.proof = capture.Evidence
	editReceipt(t, source, func(tx map[string]any) {
		tx["transaction"].(map[string]any)["message"].(map[string]any)["recentBlockhash"] = original.Recipient
	})
	if _, err = s.Step(ctx, original.RequestKey); !errors.Is(err, chainreceipt.ErrInvalidEvidence) {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatal("unexpected replay", count)
	}
	source.proof = absent
	if row, err := s.Step(ctx, original.RequestKey); err != nil || row.State != chainsend.Submitted {
		t.Fatal(row, err)
	}
	if _, err = s.Step(ctx, original.RequestKey); !errors.Is(err, chainreceipt.ErrUnconfirmed) {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatal("submitted message replayed", count)
	}
	// Fail the local confirmation after the independently committed chain receipt.
	source.proof = capture.Evidence
	mustExec(t, db, "ALTER TABLE solana_payout ADD CONSTRAINT sol_test_no_finish CHECK (state <> 'confirmed')")
	if _, err = s.Step(ctx, original.RequestKey); err == nil {
		t.Fatal("lost confirmation write was hidden")
	}
	stored, err := receipts.Find(ctx, chainassets.SolanaDevnet, original.Signature, 0)
	if err != nil || stored.Transaction != capture.Receipt.Transaction || stored.Event != capture.Receipt.Event {
		t.Fatal(stored, err)
	}
	var savedEvidence string
	if err = db.QueryRow("SELECT evidence_json FROM chain_observed_transaction").Scan(&savedEvidence); err != nil {
		t.Fatal(err)
	}
	invalidEvidence, err := json.Marshal(absent)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("UPDATE chain_observed_transaction SET evidence_json=?", string(invalidEvidence)); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Step(ctx, original.RequestKey); !errors.Is(err, chainreceipt.ErrInvalidEvidence) {
		t.Fatal("missing stored transaction treated as chain absence", err)
	}
	if _, err = db.Exec("UPDATE chain_observed_transaction SET evidence_json=?", savedEvidence); err != nil {
		t.Fatal(err)
	}
	mustExec(t, db, "ALTER TABLE solana_payout DROP CHECK sol_test_no_finish")
	source.proof = chainassets.SolanaEvidence{} // RPC history is now unavailable.
	node.height++
	if row, err := newService().Step(ctx, original.RequestKey); err != nil || row.State != chainsend.SolanaConfirmed || row.Attempts != 2 {
		t.Fatal(row, err)
	}
	source.proof = chainassets.SolanaEvidence{}
	if row, err := newService().Step(ctx, original.RequestKey); err != nil || row.State != chainsend.SolanaConfirmed {
		t.Fatal(row, err)
	}
	for table, want := range map[string]int{"chain_observed_transaction": 1, "chain_transfer_event": 1, "solana_payout": 1, "ledger_voucher": 0, "account_hold": 0, "transfer_order": 0, "event_outbox": 0} {
		var n int
		if err = db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil || n != want {
			t.Fatal(table, n, err)
		}
	}
}

func TestSolanaPayoutMigration(t *testing.T) {
	ctx := context.Background()
	name, dsn, db, err := testRun.NewDatabase(ctx, "sol_migrate")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	m, err := migration.Open(dsn, name, "../../manifest/sql/diff/business", "business")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	for _, v := range []uint{22, 23, 22, 23} {
		if err = m.Migrate(v); err != nil {
			t.Fatal(err)
		}
	}
	mustExec(t, db, "INSERT INTO solana_payout(request_key,sender,recipient,lamports,blockhash,last_valid_height,message_hash,state) VALUES('test','sender','recipient','1','hash',1,REPEAT('a',64),'prepared')")
	if err = m.Migrate(22); err == nil {
		t.Fatal("downgrade erased payout")
	}
	var key string
	if err = db.QueryRow("SELECT request_key FROM solana_payout").Scan(&key); err != nil || key != "test" {
		t.Fatal(key, err)
	}
}
