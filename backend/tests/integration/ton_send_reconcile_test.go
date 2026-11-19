//go:build integration

package integration

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"testing"

	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainreceipt"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainsend"
	"github.com/0xabcd-k/DexWallet/backend/transaction"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

func (s *tonReceiptSource) ReadTONSendEvidence(context.Context, string, string) (chainassets.TONEvidence, error) {
	return s.proof, nil
}
func TestTONSendReconcileRecoversBetweenReceiptAndAcceptance(t *testing.T) {
	ctx := context.Background()
	db, _ := userDatabase(t, "ton_reconcile")
	runner := transaction.New(db)
	conn := sqlx.NewSqlConnFromDB(db)
	req, source := tonReceiptFixture(t, "ton-no-state")
	w, sig := tonSendKey(t)
	calls := 0
	var sent tlb.ExternalMessage
	sender := tonSender(t, db, runner, w, sig, tonSendBroadcast(func(_ context.Context, _ string, b []byte) error {
		calls++
		root, err := cell.FromBOC(b)
		if err != nil {
			return err
		}
		if err = tlb.LoadFromCell(&sent, root.BeginParse()); err != nil {
			return err
		}
		return io.ErrUnexpectedEOF
	}))
	p := tonProposal("recover-confirmation", 7)
	p.Recipient, p.Atoms = req.Recipient, req.Atoms
	v, err := sender.Prepare(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = sender.Execute(ctx, p.Key); !errors.Is(err, chainsend.ErrOutcomeUnknown) {
		t.Fatal(err)
	}
	// Synthetic chain evidence for this ephemeral signed request. No RPC or real transfer.
	edit := func(raw *json.RawMessage, fn func(map[string]any)) {
		var root map[string]any
		if err := json.Unmarshal(*raw, &root); err != nil {
			t.Fatal(err)
		}
		fn(root["transactions"].([]any)[0].(map[string]any))
		*raw, _ = json.Marshal(root)
	}
	edit(&source.proof.Parent, func(tx map[string]any) {
		tx["account"] = v.Sender
		in := tx["in_msg"].(map[string]any)
		in["destination"], in["init_state"] = v.Sender, nil
		in["message_content"] = map[string]any{"hash": base64.StdEncoding.EncodeToString(sent.Body.Hash()), "body": base64.StdEncoding.EncodeToString(sent.Body.ToBOC())}
		tx["out_msgs"].([]any)[0].(map[string]any)["source"] = v.Sender
	})
	edit(&source.proof.Transaction, func(tx map[string]any) { tx["in_msg"].(map[string]any)["source"] = v.Sender })
	receipts := chainreceipt.NewTON(conn, runner, source)
	// The fact commit succeeds, then the sender's acceptance commit is lost without persistence.
	failed := tonSender(t, db, &tonSendCommitFailure{base: runner, failAt: 1, persist: false}, w, sig, tonSendBroadcast(tonNoBroadcast))
	result, err := failed.Reconcile(ctx, p.Key, receipts)
	if !errors.Is(err, chainsend.ErrOutcomeUnknown) || result.Receipt.Event.Atoms != p.Atoms || result.Reused {
		t.Fatal(result, err)
	}
	if tonFind(t, db, p.Key).State != chainsend.BroadcastUnknown {
		t.Fatal("failed acceptance commit was fabricated")
	}
	result, err = sender.Reconcile(ctx, p.Key, receipts)
	if err != nil || !result.Reused || result.Receipt.Event.SourceOwner != v.Sender {
		t.Fatal(result, err)
	}
	if tonFind(t, db, p.Key).State != chainsend.Submitted {
		t.Fatal("observed acceptance not recovered")
	}
	if _, err = sender.Execute(ctx, p.Key); err != nil || calls != 1 {
		t.Fatal("confirmed payment sent again", calls, err)
	}
	var count int
	if err = db.QueryRow("SELECT COUNT(*) FROM chain_transfer_event").Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	for _, table := range []string{"ledger_voucher", "ledger_entry", "user_account", "account_hold", "transfer_order", "event_outbox"} {
		if err = db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil || count != 0 {
			t.Fatal(table, count, err)
		}
	}
	// A different payment to the same destination/amount cannot be borrowed as proof.
	edit(&source.proof.Parent, func(tx map[string]any) {
		tx["in_msg"].(map[string]any)["message_content"].(map[string]any)["hash"] = base64.StdEncoding.EncodeToString(make([]byte, 32))
	})
	if _, err = sender.Reconcile(ctx, p.Key, receipts); err == nil {
		t.Fatal("borrowed receipt accepted")
	}
}
