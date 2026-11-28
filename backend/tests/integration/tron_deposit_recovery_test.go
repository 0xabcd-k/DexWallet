//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/internal/migration"
	"github.com/0xabcd-k/DexWallet/backend/internal/tronnative"
	"github.com/0xabcd-k/DexWallet/backend/logic/account"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainreceipt"
	"github.com/0xabcd-k/DexWallet/backend/logic/deposit"
	"github.com/0xabcd-k/DexWallet/backend/logic/user"
	accounts "github.com/0xabcd-k/DexWallet/backend/model/account"
	"github.com/0xabcd-k/DexWallet/backend/transaction"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

type nileHistory struct {
	e    chainassets.TRONEvidence
	hash string
}

func (n *nileHistory) ReadTRONEvidence(_ context.Context, network, hash string) (chainassets.TRONEvidence, error) {
	if network != chainassets.TRONNile || hash != n.hash {
		return chainassets.TRONEvidence{}, errors.New("unexpected Nile transaction")
	}
	return n.e, nil
}
func (n *nileHistory) Catalog() []chainassets.Network {
	return []chainassets.Network{{ID: chainassets.TRONNile, Family: chainassets.TRON, Assets: []chainassets.Asset{{ID: chainassets.TRONNileTRX, Origin: chainassets.Native, Decimals: 6}}}}
}
func (n *nileHistory) NileTransactionSeen(context.Context, string) (bool, error) { return true, nil }
func (n *nileHistory) CheckNileDepositResources(context.Context, chainassets.DepositPreview) error {
	return errors.New("unexpected resource preflight during historical recovery")
}
func (n *nileHistory) SubmitNileTRX(context.Context, tronnative.Signed) error {
	return errors.New("unexpected broadcast during historical recovery")
}

// Hold one previously read success until another worker has committed a final review.
type delayedNileReceipt struct {
	*nileHistory
	entered, release chan struct{}
}

func (n *delayedNileReceipt) ReadTRONEvidence(ctx context.Context, network, hash string) (chainassets.TRONEvidence, error) {
	e, err := n.nileHistory.ReadTRONEvidence(ctx, network, hash)
	close(n.entered)
	select {
	case <-n.release:
		return e, err
	case <-ctx.Done():
		return e, ctx.Err()
	}
}
func TestNileTRXDepositRecovery(t *testing.T) {
	ctx := context.Background()
	raw, err := os.ReadFile("../../logic/chainreceipt/testdata/tron-trx-self-deposit.json")
	if err != nil {
		t.Fatal(err)
	}
	var f nileDepositProof
	if err = json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	q := f.Quote
	n := &nileHistory{e: f.Evidence, hash: q.MessageSHA256}
	name, dsn, db, err := testRun.NewDatabase(ctx, "nile_deposit_recovery")
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
	quotes := deposit.NewConfirmations(conn, nil, "")
	orders := deposit.NewTronOrders(conn, quotes, nil, nil, nil)
	worker := deposit.NewTronExecution(conn, runner, ledger, n)
	step := func() error { _, e := worker.Step(ctx, q.QuoteIdx); return e }
	at := time.UnixMilli(q.QuotedAt).UTC()
	snapshot, _ := json.Marshal(q)
	mustExec(t, db, "INSERT INTO user(id,nickname) VALUES(1,'nile'),(2,'other')")
	mustExec(t, db, "INSERT INTO deposit_quote_confirmation(quote_idx,user_id,snapshot_json,quote_expires_at,confirmed_at) VALUES(?,1,?,?,?)", q.QuoteIdx, snapshot, time.UnixMilli(q.ExpiresAt).UTC(), at)
	if o, e := orders.Get(ctx, 1, q.QuoteIdx); e != nil || o.State != deposit.TronExpired {
		t.Fatal("unsigned expiry", e)
	}
	mustExec(t, db, "INSERT INTO tron_deposit_order(quote_idx,tx_id,user_signature,order_signature,accepted_at,state) VALUES(?,?,?,?,?,'signed')", q.QuoteIdx, q.MessageSHA256, f.UserSignature, f.OrderSignature, at)
	actor := user.Actor{UserID: 1, AuthVersion: 1}
	if _, e := orders.Authorize(ctx, actor, q.QuoteIdx, f.UserSignature, f.OrderSignature); e != nil {
		t.Fatal("signed replay", e)
	}
	if _, e := orders.Get(ctx, 2, q.QuoteIdx); e == nil {
		t.Fatal("read different user's order")
	}
	other := q
	other.QuoteIdx = strings.Repeat("1", 32)
	if _, e := other.BindNileDeposit(f.UserSignature, f.OrderSignature); e == nil {
		t.Fatal("public chain signature claimed for a different quote")
	}
	if _, e := orders.Authorize(ctx, actor, q.QuoteIdx, f.UserSignature, f.UserSignature); e == nil {
		t.Fatal("chain signature substituted for order proof")
	}
	trx := chainreceipt.TRONRequest{Network: chainassets.TRONNile, Hash: n.hash, AssetID: q.AssetID, Recipient: q.Recipient, Atoms: "1000"}
	if _, err = chainreceipt.NewTRON(conn, runner, n).Record(ctx, trx); err != nil {
		t.Fatal(err)
	}
	mustExec(t, db, "INSERT INTO chain_receive_address(id,network,owner,route) VALUES(1,?,?,'single_use')", chainassets.TRONNile, q.Recipient)
	mustExec(t, db, `INSERT INTO chain_receive_request(request_key,address_id,payment_reference,purpose,beneficiary_id,payer_owner,asset_id,amount_atoms,opens_at,expires_at) VALUES('merchant',1,'','merchant_payment',2,?,?,1000,1,4102444800)`, q.WalletAddress, q.AssetID)
	k := chainreceipt.EventKey{Network: chainassets.TRONNile, Signature: n.hash, Kind: chainreceipt.TRONTransfer}
	if _, err = chainreceipt.NewAttribution(conn, nil).Match(ctx, k); !errors.Is(err, chainreceipt.ErrReceiveConflict) {
		t.Fatal("merchant claimed self deposit", err)
	}
	mustExec(t, db, "UPDATE chain_receive_request SET matched_event_id=(SELECT id FROM chain_transfer_event LIMIT 1)")
	if err = step(); !errors.Is(err, chainreceipt.ErrReceiveConflict) {
		t.Fatal("credited previously allocated receipt", err)
	}
	mustExec(t, db, "UPDATE chain_receive_request SET matched_event_id=NULL")
	n.e.Info = json.RawMessage("{}")
	if err = step(); err != nil {
		t.Fatal("partial evidence", err)
	}
	n.e.Transaction = json.RawMessage("{}")
	if o, e := worker.Step(ctx, q.QuoteIdx); e != nil || o.State != deposit.TronSigned {
		t.Fatal("original transaction seen on FullNode", e)
	}
	// A delayed solid receipt can still settle an order flagged after expiry.
	mustExec(t, db, "UPDATE tron_deposit_order SET state='review'")
	for _, burn := range []bool{true, false} {
		n.e = f.Evidence
		var info map[string]any
		_ = json.Unmarshal(n.e.Info, &info)
		if burn {
			info["fee"] = int64(100)
			info["receipt"] = map[string]any{"net_fee": 100}
		} else {
			info["receipt"] = map[string]any{"net_usage": q.DelegatedBandwidth - 1}
		}
		n.e.Info, _ = json.Marshal(info)
		if o, e := worker.Step(ctx, q.QuoteIdx); e != nil || o.State != deposit.TronReview || !o.FinalizedHeight.Valid || !o.ActualFeeSun.Valid || o.CreditVoucherId.Valid {
			t.Fatal("unexpected resources auto-credited", e)
		}
		mustExec(t, db, "UPDATE tron_deposit_order SET state='review',finalized_height=NULL,actual_fee_sun=NULL,actual_bandwidth=NULL")
	}
	n.e = f.Evidence
	delayed := &delayedNileReceipt{nileHistory: n, entered: make(chan struct{}), release: make(chan struct{})}
	late := deposit.NewTronExecution(conn, runner, ledger, delayed)
	scope, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, e := late.Step(scope, q.QuoteIdx); done <- e }()
	select {
	case <-delayed.entered:
	case <-scope.Done():
		t.Fatal(scope.Err())
	}
	var failed map[string]any
	if err = json.Unmarshal(f.Evidence.Transaction, &failed); err != nil {
		t.Fatal(err)
	}
	failed["ret"] = []any{map[string]any{"contractRet": "TRANSFER_FAILED"}}
	n.e.Transaction, _ = json.Marshal(failed)
	failedRow, e := worker.Step(ctx, q.QuoteIdx)
	close(delayed.release)
	if e != nil || failedRow.State != deposit.TronReview || !failedRow.FinalizedHeight.Valid {
		t.Fatal("solidified failure", e)
	}
	if e = <-done; e != nil {
		t.Fatal("stale worker", e)
	}
	mustExec(t, db, "UPDATE tron_deposit_order SET state='review',finalized_height=NULL,actual_fee_sun=NULL,actual_bandwidth=NULL")
	page, err := ledger.Entries(ctx, 1, accounts.KindUSD, 0, 2)
	if err != nil || len(page.Entries) != 0 {
		t.Fatal("unverified funds posted", err)
	}
	n.e = f.Evidence
	results := make(chan error, 2)
	for range 2 {
		go func() { results <- step() }()
	}
	for range 2 {
		if err = <-results; err != nil {
			t.Fatal(err)
		}
	}
	page, err = ledger.Entries(ctx, 1, accounts.KindUSD, 0, 2)
	if err != nil || len(page.Entries) != 1 || page.Entries[0].DepositOrderIdx != q.QuoteIdx || page.Entries[0].Action != "credit_tron_trx" || page.Entries[0].BalanceAfter.StringFixed(2) != "0.01" {
		t.Fatal("original statement", err)
	}
	cmd := exec.Command("go", "run", "./tools/tron_deposit_nile", "-quote", q.QuoteIdx, "-rpc", "http://127.0.0.1:1")
	cmd.Dir = "../.."
	cmd.Env = append(os.Environ(), "DEXWALLET_MYSQL_DSN="+dsn)
	if out, e := cmd.CombinedOutput(); e != nil || !strings.Contains(string(out), "credited_usd=0.01") {
		t.Fatalf("keyless CLI %v %s", e, out)
	}
	if err = m.Migrate(44); err == nil || !strings.Contains(err.Error(), "tron_deposit_rollback_guard") {
		t.Fatal("rollback discarded deposit", err)
	}
}
