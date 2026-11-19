//go:build integration

package integration

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/internal/migration"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainreceipt"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainsend"
	sendmodel "github.com/0xabcd-k/DexWallet/backend/model/chainsend"
	"github.com/0xabcd-k/DexWallet/backend/transaction"
	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

type tonJettonBroadcast struct {
	tonSendBroadcast
	wallet string
}

func (b tonJettonBroadcast) ResolveTONJettonWallet(context.Context, string, string) (string, error) {
	return b.wallet, nil
}
func TestTONJettonSendRestartAndRollbackPreserveAuthorization(t *testing.T) {
	ctx := context.Background()
	name, dsn, db, err := testRun.NewDatabase(ctx, "ton_jetton_send")
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
	runner := transaction.New(db)
	w, sign := tonSendKey(t)
	w.JettonMaster = "0:" + strings.Repeat("cd", 32)
	p := tonProposal("jetton-recovery", 3)
	p.Atoms = "100000"
	p.Jetton = chainsend.JettonTransfer{Master: w.JettonMaster, Wallet: "0:" + strings.Repeat("ef", 32), AttachTON: "50000000", ForwardTON: "1", QueryID: "2026111901"}
	var raw []byte
	calls, signs := 0, 0
	signer := tonSendSigner(func(ctx context.Context, r chainsend.SigningRequest) ([]byte, error) { signs++; return sign(ctx, r) })
	broadcast := tonJettonBroadcast{tonSendBroadcast: func(_ context.Context, _ string, b []byte) error {
		calls++
		raw = append([]byte(nil), b...)
		return io.ErrUnexpectedEOF
	}, wallet: p.Jetton.Wallet}
	sender := tonSender(t, db, runner, w, signer, broadcast)
	if _, err = sender.Prepare(ctx, p); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*chainsend.Proposal){func(p *chainsend.Proposal) { p.Jetton.QueryID = "2" }, func(p *chainsend.Proposal) { p.Jetton.AttachTON = "60000000" }, func(p *chainsend.Proposal) { p.Jetton.ForwardTON = "2" }, func(p *chainsend.Proposal) { p.Atoms = "200000" }, func(p *chainsend.Proposal) { p.Jetton = chainsend.JettonTransfer{} }} {
		bad := p
		change(&bad)
		if _, err = sender.Prepare(ctx, bad); !errors.Is(err, chainsend.ErrConflict) {
			t.Fatal("changed authorization accepted", err)
		}
	}
	if _, err = sender.Execute(ctx, p.Key); !errors.Is(err, chainsend.ErrOutcomeUnknown) {
		t.Fatal(err)
	}
	before := tonFind(t, db, p.Key)
	broadcast.tonSendBroadcast = func(_ context.Context, _ string, b []byte) error {
		calls++
		if !bytes.Equal(raw, b) {
			t.Error("Jetton retry replaced original message")
		}
		return nil
	}
	recovered := tonSender(t, db, runner, w, signer, broadcast)
	after, err := recovered.Execute(ctx, p.Key)
	if err != nil || after.State != chainsend.Submitted || after.Proposal != p || after.BodyHash != before.BodyHash || after.Attempts != 2 || signs != 1 || calls != 2 {
		t.Fatal(after, signs, calls, err)
	}
	if _, err = recovered.Execute(ctx, p.Key); err != nil || calls != 2 {
		t.Fatal("replayed submitted payment", err)
	}
	// Even a submitted authorization is immutable history, not disposable schema data.
	if err = m.Migrate(20); err == nil {
		t.Fatal("rollback discarded Jetton authorization")
	}
	if got := tonFind(t, db, p.Key); got != after {
		t.Fatal("failed rollback changed intent", got)
	}
	// Stored JSON remains a trust boundary on restart; altered budgets cannot broadcast.
	mustExec(t, db, "UPDATE chain_send_intent SET jetton_json=JSON_SET(jetton_json,'$.attachTon','60000000') WHERE request_key=?", p.Key)
	if _, err = recovered.Execute(ctx, p.Key); err == nil || calls != 2 {
		t.Fatal("stored tampering accepted", err)
	}
	for _, table := range []string{"user_account", "ledger_voucher", "ledger_entry", "account_hold", "transfer_order", "chain_transfer_event"} {
		var count int
		if err = db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil || count != 0 {
			t.Fatal(table, count, err)
		}
	}
}

func (s *tonReceiptSource) ReadTONJettonSendEvidence(context.Context, string, string, string, string) (chainassets.TONEvidence, error) {
	return s.proof, nil
}
func TestTONJettonSendReconcileHistoricalIntentAfterCommitLoss(t *testing.T) {
	ctx := context.Background()
	db, _ := userDatabase(t, "ton_jetton_reconcile")
	runner := transaction.New(db)
	conn := sqlx.NewSqlConnFromDB(db)
	raw, err := os.ReadFile("../../logic/chainreceipt/testdata/ton-jetton-send.json")
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Intent   chainsend.View
		Wallet   chainsend.Wallet
		Receipt  chainreceipt.Receipt
		Evidence chainassets.TONEvidence
	}
	if err = json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	var dispatch struct {
		Transactions []struct {
			In struct {
				Content struct{ Body string } `json:"message_content"`
			} `json:"in_msg"`
		}
	}
	if err = json.Unmarshal(f.Evidence.Dispatch, &dispatch); err != nil {
		t.Fatal(err)
	}
	b, err := base64.StdEncoding.DecodeString(dispatch.Transactions[0].In.Content.Body)
	if err != nil {
		t.Fatal(err)
	}
	signed, err := cell.FromBOC(b)
	if err != nil {
		t.Fatal(err)
	}
	senderAddr, _ := address.ParseRawAddr(f.Intent.Sender)
	external, err := tlb.ToCell(&tlb.ExternalMessage{DstAddr: senderAddr, Body: signed})
	if err != nil {
		t.Fatal(err)
	}
	j, _ := json.Marshal(f.Intent.Jetton)
	// Restore already signed historical public bytes. Do not generate fresh payment authorization.
	row := &sendmodel.ChainSendIntent{Network: f.Intent.Network, RequestKey: f.Intent.Key, Sender: f.Intent.Sender, Recipient: f.Intent.Recipient, AssetId: f.Intent.AssetID, AmountAtoms: f.Intent.Atoms, JettonJson: string(j), PublicKey: hex.EncodeToString(f.Wallet.PublicKey), SubwalletId: int64(f.Wallet.SubwalletID), Seqno: int64(f.Intent.Seqno), ValidUntil: int64(f.Intent.ValidUntil), PayloadHash: f.Intent.PayloadHash, SignedBoc: sql.NullString{String: base64.StdEncoding.EncodeToString(external.ToBOC()), Valid: true}, MessageHash: f.Intent.MessageHash, BodyHash: f.Intent.BodyHash, State: chainsend.BroadcastUnknown, Attempts: 1, RecoveryState: chainsend.RecoveryReady, RecoveryNextAt: time.Now().Add(-time.Minute)}
	if _, err = sendmodel.NewChainSendIntentModel(conn).Insert(ctx, row); err != nil {
		t.Fatal(err)
	}
	catalog, err := chainassets.New(chainassets.Config{Networks: []chainassets.Endpoint{{Network: chainassets.TONTestnet, URL: "http://127.0.0.1:1", TestUSDTContract: f.Wallet.JettonMaster}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()
	source := &tonReceiptSource{catalog: catalog.Catalog(), proof: f.Evidence}
	receipts := chainreceipt.NewTON(conn, runner, source)
	_, signer := tonSendKey(t) // Wrong key intentionally: reconcile never signs or broadcasts.
	failed := tonSender(t, db, &tonSendCommitFailure{base: runner, failAt: 1, persist: false}, f.Wallet, signer, tonSendBroadcast(tonNoBroadcast))
	result, err := failed.Reconcile(ctx, row.RequestKey, receipts)
	if !errors.Is(err, chainsend.ErrOutcomeUnknown) || result.Reused || result.Receipt.Event != f.Receipt.Event {
		t.Fatal(result, err)
	}
	recovered := tonSender(t, db, runner, f.Wallet, signer, tonSendBroadcast(tonNoBroadcast))
	result, err = recovered.Reconcile(ctx, row.RequestKey, receipts)
	if err != nil || !result.Reused || result.Receipt.Transaction != f.Receipt.Transaction {
		t.Fatal(result, err)
	}
	if got := tonFind(t, db, row.RequestKey); got.State != chainsend.Submitted || got.Attempts != 1 {
		t.Fatal(got)
	}
	if _, err = recovered.Execute(ctx, row.RequestKey); err != nil {
		t.Fatal("confirmed Jetton executed again", err)
	}
	for table, want := range map[string]int{"chain_transfer_event": 1, "chain_observed_transaction": 1, "user_account": 0, "ledger_entry": 0, "ledger_voucher": 0, "account_hold": 0, "transfer_order": 0} {
		var count int
		if err = db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil || count != want {
			t.Fatal(table, count, err)
		}
	}
	source.proof.SenderWallet = source.proof.Wallet
	if _, err = recovered.Reconcile(ctx, row.RequestKey, receipts); err == nil {
		t.Fatal("wrong token sender reused receipt")
	}
}
