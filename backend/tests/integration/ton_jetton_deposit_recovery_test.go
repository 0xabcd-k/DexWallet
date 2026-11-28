//go:build integration

package integration

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/internal/migration"
	"github.com/0xabcd-k/DexWallet/backend/logic/account"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainreceipt"
	"github.com/0xabcd-k/DexWallet/backend/logic/deposit"
	accounts "github.com/0xabcd-k/DexWallet/backend/model/account"
	"github.com/0xabcd-k/DexWallet/backend/transaction"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

func TestTONJettonDepositRecovery(t *testing.T) {
	data, err := os.ReadFile("../../logic/chainreceipt/testdata/ton-self-deposit-jetton.json")
	if err != nil {
		t.Fatal(err)
	}
	var proof tonDepositProof
	if err = json.Unmarshal(data, &proof); err != nil {
		t.Fatal(err)
	}
	q := proof.Quote
	user, _ := hex.DecodeString(proof.UserSignature)
	platform, _ := hex.DecodeString(proof.PlatformSignature)
	relay, err := q.TONRelay(user, platform)
	if err != nil {
		t.Fatal(err)
	}
	receipt, fee, costs, err := chainreceipt.VerifyTONJettonDeposit(proof.Evidence, relay)
	if err != nil || receipt.Event.Atoms != "17640" || fee.Int64() != 3643213 || costs.Excess != 47758913 || costs.Surplus != 2526573 || costs.Reserve != 6283 || costs.SenderPaid != 75086 {
		t.Fatal("actual TON fees and token amounts", err, costs)
	}
	ctx := context.Background()
	name, dsn, db, err := testRun.NewDatabase(ctx, "jetton_credit")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	m, err := migration.Open(dsn, name, "../../manifest/sql/diff/business", "business")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if err = m.Up(); err != nil {
		t.Fatal(err)
	}
	conn := sqlx.NewSqlConnFromDB(db)
	runner := transaction.New(db)
	ledger := account.New(accounts.NewUserAccountModel(conn))
	history := &tonDepositHistory{hash: relay.DispatchHash, evidence: proof.Evidence}
	worker := deposit.NewTonExecution(conn, runner, ledger, history)
	step := func() error { _, err := worker.Step(ctx, q.QuoteIdx, deposit.TonBudget{}, nil); return err }
	mustExec(t, db, "INSERT INTO user(id,nickname) VALUES (1,'jetton'),(2,'merchant')")
	at := time.UnixMilli(q.QuotedAt).UTC()
	snapshot, _ := json.Marshal(q)
	mustExec(t, db, "INSERT INTO deposit_quote_confirmation(quote_idx,user_id,snapshot_json,quote_expires_at,confirmed_at) VALUES(?,1,?,?,?)", q.QuoteIdx, snapshot, time.UnixMilli(q.ExpiresAt).UTC(), at)
	mustExec(t, db, "INSERT INTO ton_deposit_order(quote_idx,auth_hash,jetton_payment_key,user_signature,order_signature,accepted_at,state,platform_signature,dispatch_hash) VALUES(?,?,?,?,?,?,'signed',?,?)", q.QuoteIdx, relay.AuthHash, chainassets.TONJettonPaymentKey(relay.Sender, relay.Jetton.PaymentHash), proof.UserSignature, proof.OrderSignature, at, proof.PlatformSignature, relay.DispatchHash)
	// The normal receipt listener sees the same internal_transfer before the relay executor.
	var trace struct {
		Traces []struct{ Transactions map[string]json.RawMessage }
	}
	if err = json.Unmarshal(proof.Evidence.Trace, &trace); err != nil {
		t.Fatal(err)
	}
	for _, raw := range trace.Traces[0].Transactions {
		var tx struct{ Account string }
		if err = json.Unmarshal(raw, &tx); err != nil {
			t.Fatal(err)
		}
		one, _ := json.Marshal(map[string][]json.RawMessage{"transactions": {raw}})
		switch strings.ToLower(tx.Account) {
		case relay.Jetton.SenderWallet:
			history.evidence.Parent = one
		case relay.Jetton.ReceiverWallet:
			history.evidence.Transaction = one
		}
	}
	req := chainreceipt.TONRequest{Network: chainassets.TONTestnet, Hash: receipt.Transaction.Signature, AssetID: q.AssetID, Recipient: relay.Recipient, Atoms: relay.Atoms.String()}
	if _, err = chainreceipt.NewTON(conn, runner, history).Record(ctx, req); err != nil {
		t.Fatal(err)
	}
	mustExec(t, db, "INSERT INTO chain_receive_address(id,network,owner,route) VALUES(1,?,?,'single_use')", chainassets.TONTestnet, relay.Recipient)
	mustExec(t, db, "INSERT INTO chain_receive_request(request_key,address_id,payment_reference,purpose,beneficiary_id,payer_owner,asset_id,amount_atoms,opens_at,expires_at) VALUES('merchant',1,'','merchant_payment',2,?,?,?,1,4102444800)", relay.Sender, q.AssetID, req.Atoms)
	if _, err = chainreceipt.NewAttribution(conn, nil).Match(ctx, chainreceipt.EventKey{Network: req.Network, Signature: req.Hash, Kind: chainreceipt.TONJetton}); !errors.Is(err, chainreceipt.ErrReceiveConflict) {
		t.Fatal("merchant claimed test Jetton deposit", err)
	}
	mustExec(t, db, "UPDATE chain_receive_request SET matched_event_id=(SELECT id FROM chain_transfer_event LIMIT 1)")
	if err = step(); !errors.Is(err, chainreceipt.ErrReceiveConflict) {
		t.Fatal("claimed a merchant event", err)
	}
	mustExec(t, db, "UPDATE chain_receive_request SET matched_event_id=NULL")
	history.evidence.Trace = nil
	if row, e := worker.Step(ctx, q.QuoteIdx, deposit.TonBudget{}, nil); e != nil || row.CreditVoucherId.Valid {
		t.Fatal("partial Jetton proof", e)
	}
	history.evidence = proof.Evidence
	done := make(chan error, 2)
	for range 2 {
		go func() { done <- step() }()
	}
	for range 2 {
		if err = <-done; err != nil {
			t.Fatal(err)
		}
	}
	row, err := worker.Step(ctx, q.QuoteIdx, deposit.TonBudget{}, nil)
	if err != nil || row.State != deposit.TonCredited || row.ActualFeeNano.Int64 != fee.Int64() || row.JettonSenderFeeNano.Int64 != 75086 || row.RelaySurplusNano.Int64 != costs.Surplus || row.JettonExcessNano.Int64 != costs.Excess || row.JettonReserveNano.Int64 != costs.Reserve {
		t.Fatal("replayed Jetton funds", row, err)
	}
	page, err := ledger.Entries(ctx, 1, accounts.KindUSD, 0, 2)
	if err != nil || len(page.Entries) != 1 || page.Entries[0].Action != "credit_ton_test_usdt" || page.Entries[0].DepositOrderIdx != q.QuoteIdx || page.Entries[0].BalanceAfter.StringFixed(2) != "0.01" {
		t.Fatal("incorrect Jetton statement", page, err)
	}
	if err = m.Migrate(47); err == nil || !strings.Contains(err.Error(), "ton_jetton_deposit_rollback_guard") {
		t.Fatal("discarded Jetton order", err)
	}
}
