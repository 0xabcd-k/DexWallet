//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"errors"
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

func (s *baseReceiptSource) ReadBaseCallEvidence(context.Context, string, string) (chainassets.BaseEvidence, error) {
	return s.proof, nil
}

func TestBaseCallReceiptMigrationDedupAndRecovery(t *testing.T) {
	ctx := context.Background()
	name, dsn, db, err := testRun.NewDatabase(ctx, "base_call_receipts")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	m, err := migration.Open(dsn, name, "../../manifest/sql/diff/business", "business")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if err = m.Migrate(18); err != nil {
		t.Fatal(err)
	}
	request, source := baseReceiptFixture(t, "base-internal-eth-fixture")
	logIndex := 5
	token := request
	token.CallIndex, token.LogIndex, token.AssetID, token.Atoms = nil, &logIndex, "base-sepolia:usdc", "10000"
	legacy, err := chainreceipt.NewBaseVerifier(source).Verify(ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	proof := source.proof
	proof.CallTrace = nil
	raw, _ := json.Marshal(proof)
	x, e := legacy.Transaction, legacy.Event
	r, err := db.Exec("INSERT INTO chain_observed_transaction(network,signature,slot,fee_payer,fee_atoms,block_hash,execution_fee_atoms,l1_fee_atoms,operator_fee_atoms,fingerprint,evidence_json) VALUES(?,?,?,?,?,?,?,?,?,?,?)", x.Network, x.Signature, x.Slot, x.FeePayer, x.FeeAtoms, x.BlockHash, x.ExecutionFeeAtoms, x.L1FeeAtoms, x.OperatorFeeAtoms, x.Fingerprint, string(raw))
	if err != nil {
		t.Fatal(err)
	}
	id, err := r.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, db, "INSERT INTO chain_transfer_event(transaction_id,event_kind,instruction_index,asset_id,program_id,mint,decimals,source_account,source_owner,recipient_account,recipient_owner,amount_atoms) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)", id, e.Kind, e.Instruction, e.AssetID, e.ProgramID, e.Mint, e.Decimals, e.SourceAccount, e.SourceOwner, e.RecipientAccount, e.RecipientOwner, e.Atoms)
	for _, version := range []uint{19, 18, 19} {
		if err = m.Migrate(version); err != nil {
			t.Fatal(err)
		}
	}
	conn, runner := sqlx.NewSqlConnFromDB(db), transaction.New(db)
	service := chainreceipt.NewBase(conn, runner, source)
	if s, err := service.Record(ctx, token); err != nil || !s.Reused || s.Receipt.Transaction != legacy.Transaction {
		t.Fatal(s, err)
	}
	// Lost commit without persistence must not leave trace enrichment behind.
	if _, err = chainreceipt.NewBase(conn, receiptCommitLost{runner, false}, source).Record(ctx, request); !errors.Is(err, chainreceipt.ErrOutcomeUnknown) {
		t.Fatal(err)
	}
	var traces int
	if err = db.QueryRow("SELECT COUNT(*) FROM chain_observed_transaction WHERE call_trace_json IS NOT NULL").Scan(&traces); err != nil || traces != 0 {
		t.Fatal(traces, err)
	}
	second := request
	i := 7
	second.CallIndex = &i
	second.Atoms = "20"
	second.Recipient = "0x0000000000000000000000000000000000000002"
	requests := []chainreceipt.BaseRequest{token, request, second}
	var wait sync.WaitGroup
	var created atomic.Int64
	failures := make(chan error, 18)
	for i := 0; i < 18; i++ {
		wait.Add(1)
		go func(i int) {
			defer wait.Done()
			s, err := service.Record(ctx, requests[i%3])
			if err != nil {
				failures <- err
			} else if !s.Reused {
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
	var original, trace, fee string
	if err = db.QueryRow("SELECT COUNT(*),MAX(evidence_json),MAX(call_trace_json),MAX(fee_atoms) FROM chain_observed_transaction").Scan(&txs, &original, &trace, &fee); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow("SELECT COUNT(*) FROM chain_transfer_event").Scan(&events); err != nil {
		t.Fatal(err)
	}
	if txs != 1 || events != 3 || original != string(raw) || trace != string(source.proof.CallTrace) || fee != x.FeeAtoms {
		t.Fatal("duplicate fee or replaced evidence", txs, events, fee)
	}
	found, err := service.Find(ctx, request.Network, request.Hash, chainreceipt.EVMCall, 6)
	if err != nil || found.Event.Atoms != request.Atoms || found.Event.SourceOwner == found.Transaction.FeePayer {
		t.Fatal(found, err)
	}
	for _, index := range []int{-1, 0} {
		if _, err = service.Find(ctx, request.Network, request.Hash, chainreceipt.EVMCall, index); !errors.Is(err, chainreceipt.ErrInvalidRequest) {
			t.Fatal(err)
		}
	}
	// Even an unselected sibling change must conflict with the first full trace.
	firstTrace := append(json.RawMessage(nil), source.proof.CallTrace...)
	editBaseJSON(t, &source.proof.CallTrace, func(m map[string]any) { m["calls"].([]any)[0].(map[string]any)["output"] = "0x12" })
	if _, err = service.Record(ctx, request); !errors.Is(err, chainreceipt.ErrConflict) {
		t.Fatal(err)
	}
	if _, err = chainreceipt.NewBase(conn, receiptCommitLost{runner, false}, source).Record(ctx, request); !errors.Is(err, chainreceipt.ErrOutcomeUnknown) {
		t.Fatal(err)
	}
	source.proof.CallTrace = firstTrace
	// A fresh event with a durable commit but lost response is recovered.
	mustExec(t, db, "DELETE FROM chain_transfer_event WHERE event_kind='evm_call' AND instruction_index=7")
	recovered, err := chainreceipt.NewBase(conn, receiptCommitLost{runner, true}, source).Record(ctx, second)
	if err != nil || !recovered.Reused || recovered.Receipt.Event.Instruction != 7 {
		t.Fatal(recovered, err)
	}
	for _, table := range []string{"user_account", "ledger_voucher", "ledger_entry", "account_hold", "transfer_order", "event_outbox"} {
		var n int
		if err = db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil || n != 0 {
			t.Fatal(table, n, err)
		}
	}
	if err = m.Migrate(18); err == nil || !strings.Contains(err.Error(), "base_call_trace_rollback_guard") {
		t.Fatal("trace rollback not guarded", err)
	}
	if _, err = service.Find(ctx, request.Network, request.Hash, chainreceipt.EVMCall, 6); err != nil {
		t.Fatal(err)
	}
	t.Log("business 18/19, preserved USDC evidence, 18 concurrent observations/3 events/1 fee, trace conflicts, commit recovery, rollback guard and unchanged funds passed")
}
