//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
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

type baseReceiptSource struct {
	catalog []chainassets.Network
	proof   chainassets.BaseEvidence
}

func (s *baseReceiptSource) Catalog() []chainassets.Network { return s.catalog }
func (s *baseReceiptSource) ReadBaseEvidence(context.Context, string, string) (chainassets.BaseEvidence, error) {
	return s.proof, nil
}
func baseReceiptFixture(t *testing.T, name string) (chainreceipt.BaseRequest, *baseReceiptSource) {
	t.Helper()
	b, err := os.ReadFile("../../logic/chainreceipt/testdata/" + name + ".json")
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Request  chainreceipt.BaseRequest
		Evidence chainassets.BaseEvidence
	}
	if err = json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	assets, err := chainassets.New(chainassets.Config{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer assets.Close()
	return f.Request, &baseReceiptSource{assets.Catalog(), f.Evidence}
}
func editBaseJSON(t *testing.T, p *json.RawMessage, edit func(map[string]any)) {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(*p, &m); err != nil {
		t.Fatal(err)
	}
	edit(m)
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	*p = b
}

func TestBaseReceiptUpgradeAndPersistence(t *testing.T) {
	ctx := context.Background()
	name, dsn, db, err := testRun.NewDatabase(ctx, "base_receipts")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	m, err := migration.Open(dsn, name, "../../manifest/sql/diff/business", "business")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if err = m.Migrate(16); err != nil {
		t.Fatal(err)
	}
	// A real business-16 row must retain its identity, fingerprint and evidence.
	data, err := os.ReadFile("../../logic/chainreceipt/testdata/sol.json")
	if err != nil {
		t.Fatal(err)
	}
	var legacy struct {
		Receipt  chainreceipt.Receipt
		Evidence json.RawMessage
	}
	if err = json.Unmarshal(data, &legacy); err != nil {
		t.Fatal(err)
	}
	x, e := legacy.Receipt.Transaction, legacy.Receipt.Event
	result, err := db.Exec("INSERT INTO chain_observed_transaction(network,signature,slot,fee_payer,fee_atoms,fingerprint,evidence_json) VALUES(?,?,?,?,?,?,?)", x.Network, x.Signature, x.Slot, x.FeePayer, x.FeeAtoms, x.Fingerprint, string(legacy.Evidence))
	if err != nil {
		t.Fatal(err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, db, "INSERT INTO chain_transfer_event(transaction_id,instruction_index,asset_id,program_id,mint,decimals,source_account,source_owner,recipient_account,recipient_owner,amount_atoms) VALUES(?,?,?,?,?,?,?,?,?,?,?)", id, e.Instruction, e.AssetID, e.ProgramID, e.Mint, e.Decimals, e.SourceAccount, e.SourceOwner, e.RecipientAccount, e.RecipientOwner, e.Atoms)
	conn := sqlx.NewSqlConnFromDB(db)
	runner := transaction.New(db)
	r, sol := receiptFixtureSource(t, "sol")
	for i := 0; i < 2; i++ {
		if err = m.Up(); err != nil {
			t.Fatal(err)
		}
		saved, err := chainreceipt.New(conn, runner, sol).Record(ctx, r)
		if err != nil || !saved.Reused {
			t.Fatal(saved, err)
		}
		var evidence string
		if err = db.QueryRow("SELECT evidence_json FROM chain_observed_transaction WHERE id=?", id).Scan(&evidence); err != nil || evidence != string(legacy.Evidence) {
			t.Fatal("legacy evidence changed", err)
		}
		if i == 0 {
			if err = m.Migrate(16); err != nil {
				t.Fatal(err)
			}
		}
	}
	request, source := baseReceiptFixture(t, "base-usdc")
	// Synthetic, never broadcast: three events including a native value and a
	// token log at the same numeric position, plus full-width uint256 amounts.
	max := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	request.Atoms = max.String()
	first, second := 0, 7
	request.LogIndex = &first
	editBaseJSON(t, &source.proof.Receipt, func(m map[string]any) {
		logs := m["logs"].([]any)
		l := logs[1].(map[string]any)
		l["logIndex"] = "0x0"
		l["data"] = "0x" + strings.Repeat("f", 64)
		b, _ := json.Marshal(l)
		var next map[string]any
		_ = json.Unmarshal(b, &next)
		next["logIndex"] = "0x7"
		next["data"] = "0x" + strings.Repeat("0", 61) + "7d0"
		m["logs"] = append(logs, next)
	})
	editBaseJSON(t, &source.proof.Transaction, func(m map[string]any) { m["input"] = "0x"; m["value"] = "0x3e8" })
	operator := new(big.Int).Sub(new(big.Int).Set(max), big.NewInt(617208000196))
	source.proof.OperatorFee = json.RawMessage(`"0x` + operator.Text(16) + `"`)
	requests := []chainreceipt.BaseRequest{request, request, request}
	requests[1].LogIndex = &second
	requests[1].Atoms = "2000"
	requests[2].LogIndex = nil
	requests[2].AssetID = "base-sepolia:eth"
	requests[2].Recipient = "0x036cbd53842c5426634e7929541ec2318f3dcf7e"
	requests[2].Atoms = "1000"
	service := chainreceipt.NewBase(conn, runner, source)
	var wait sync.WaitGroup
	var created atomic.Int64
	failures := make(chan error, 18)
	for i := 0; i < 18; i++ {
		wait.Add(1)
		go func(i int) {
			defer wait.Done()
			saved, err := service.Record(ctx, requests[i%3])
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
	if created.Load() != 3 {
		t.Fatal(created.Load())
	}
	var txs, events int
	var fee string
	if err = db.QueryRow("SELECT COUNT(*),MAX(fee_atoms) FROM chain_observed_transaction WHERE network='base-sepolia'").Scan(&txs, &fee); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow("SELECT COUNT(*) FROM chain_transfer_event WHERE event_kind IN ('evm_log','evm_value')").Scan(&events); err != nil {
		t.Fatal(err)
	}
	if txs != 1 || events != 3 || fee != max.String() {
		t.Fatal(txs, events, fee)
	}
	found, err := service.Find(ctx, request.Network, request.Hash, chainreceipt.EVMLog, 0)
	if err != nil || found.Event.Atoms != max.String() {
		t.Fatal(found, err)
	}
	var original string
	if err = db.QueryRow("SELECT evidence_json FROM chain_observed_transaction WHERE network='base-sepolia'").Scan(&original); err != nil {
		t.Fatal(err)
	}
	editBaseJSON(t, &source.proof.Finalized, func(m map[string]any) { m["number"] = "0x7fffffff" })
	reused, err := service.Record(ctx, request)
	if err != nil || !reused.Reused {
		t.Fatal(reused, err)
	}
	var after string
	if err = db.QueryRow("SELECT evidence_json FROM chain_observed_transaction WHERE network='base-sepolia'").Scan(&after); err != nil || after != original {
		t.Fatal("evidence replaced", err)
	}
	editBaseJSON(t, &source.proof.Receipt, func(m map[string]any) {
		m["logs"].([]any)[2].(map[string]any)["data"] = "0x" + strings.Repeat("0", 61) + "7d1"
	})
	if _, err = service.Record(ctx, request); !errors.Is(err, chainreceipt.ErrConflict) {
		t.Fatal(err)
	}
	native, nativeSource := baseReceiptFixture(t, "base-eth")
	if _, err = chainreceipt.NewBase(conn, receiptCommitLost{runner, false}, nativeSource).Record(ctx, native); !errors.Is(err, chainreceipt.ErrOutcomeUnknown) {
		t.Fatal(err)
	}
	recovered, err := chainreceipt.NewBase(conn, receiptCommitLost{runner, true}, nativeSource).Record(ctx, native)
	if err != nil || !recovered.Reused {
		t.Fatal(recovered, err)
	}
	for _, table := range []string{"user_account", "ledger_voucher", "ledger_entry", "account_hold", "transfer_order", "event_outbox"} {
		var count int
		if err = db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil || count != 0 {
			t.Fatal(table, count, err)
		}
	}
	if err = m.Migrate(16); err == nil || !strings.Contains(err.Error(), "base_receipt_rollback_guard") {
		t.Fatal("Base evidence rollback not guarded", err)
	}
	if _, err = service.Find(ctx, request.Network, request.Hash, chainreceipt.EVMLog, 0); err != nil {
		t.Fatal("rollback lost event", err)
	}
	t.Log("business 16/17 Solana preservation, 18 concurrent requests / 3 events / 1 Base fee, uint256, immutable evidence, conflict, commit recovery, unchanged funds and rollback guard passed")
}
