//go:build integration

package integration

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/internal/migration"
	"github.com/0xabcd-k/DexWallet/backend/logic/account"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainreceipt"
	"github.com/0xabcd-k/DexWallet/backend/logic/deposit"
	"github.com/0xabcd-k/DexWallet/backend/logic/user"
	accounts "github.com/0xabcd-k/DexWallet/backend/model/account"
	"github.com/0xabcd-k/DexWallet/backend/transaction"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

type tonDepositHistory struct {
	hash     string
	evidence chainassets.TONEvidence
}

func (n *tonDepositHistory) ReadTONDepositEvidence(_ context.Context, h string) (chainassets.TONEvidence, error) {
	if h != n.hash {
		return chainassets.TONEvidence{}, errors.New("wrong TON body hash")
	}
	return n.evidence, nil
}
func (n *tonDepositHistory) CheckTONDeposit(context.Context, chainassets.DepositPreview, []byte, []byte) error {
	return errors.New("unexpected preflight during recovery")
}
func (n *tonDepositHistory) SubmitTON(context.Context, string, []byte) error {
	return errors.New("unexpected recovery broadcast")
}
func (n *tonDepositHistory) Catalog() []chainassets.Network {
	return []chainassets.Network{{ID: chainassets.TONTestnet, Family: chainassets.TON, Assets: []chainassets.Asset{{ID: "ton-testnet:ton", Origin: chainassets.Native, Decimals: 9}}}}
}
func (n *tonDepositHistory) ReadTONEvidence(context.Context, string, string, string, string) (chainassets.TONEvidence, error) {
	return n.evidence, nil
}

func TestTONNativeDepositRecovery(t *testing.T) {
	for _, caseName := range []string{"credit", "over-cap"} {
		t.Run(caseName, func(t *testing.T) {
			ctx := context.Background()
			raw, err := os.ReadFile("../../logic/chainreceipt/testdata/ton-self-deposit-" + caseName + ".json")
			if err != nil {
				t.Fatal(err)
			}
			var f tonDepositProof
			if err = json.Unmarshal(raw, &f); err != nil {
				t.Fatal(err)
			}
			q := f.Quote
			u, _ := hex.DecodeString(f.UserSignature)
			p, _ := hex.DecodeString(f.PlatformSignature)
			r, err := q.TONRelay(u, p)
			if err != nil {
				t.Fatal(err)
			}
			other := q
			other.QuoteIdx = strings.Repeat("a", 32)
			if _, err = other.BindTONDeposit(f.UserSignature, f.OrderSignature); err == nil {
				t.Fatal("cross-order signature accepted")
			}
			receipt, fee, err := chainreceipt.VerifyTONDeposit(f.Evidence, r)
			if err != nil {
				t.Fatal(err)
			}
			label := "ton_credit"
			if caseName == "over-cap" {
				label = "ton_cap"
			}
			name, dsn, db, err := testRun.NewDatabase(ctx, label)
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
			orders := deposit.NewTonOrders(conn, quotes, nil, nil, nil)
			n := &tonDepositHistory{hash: r.DispatchHash, evidence: f.Evidence}
			worker := deposit.NewTonExecution(conn, runner, ledger, n)
			step := func() error { _, e := worker.Step(ctx, q.QuoteIdx, deposit.TonBudget{}, nil); return e }
			at := time.UnixMilli(q.QuotedAt).UTC()
			snapshot, _ := json.Marshal(q)
			mustExec(t, db, "INSERT INTO user(id,nickname) VALUES(1,'ton'),(2,'other')")
			mustExec(t, db, "INSERT INTO deposit_quote_confirmation(quote_idx,user_id,snapshot_json,quote_expires_at,confirmed_at) VALUES(?,1,?,?,?)", q.QuoteIdx, snapshot, time.UnixMilli(q.ExpiresAt).UTC(), at)
			if o, e := orders.Get(ctx, 1, q.QuoteIdx); e != nil || o.State != deposit.TonExpired {
				t.Fatal("unsigned expiry", e)
			}
			mustExec(t, db, "INSERT INTO ton_deposit_order(quote_idx,auth_hash,user_signature,order_signature,accepted_at,state,platform_signature,dispatch_hash) VALUES(?,?,?,?,?,'signed',?,?)", q.QuoteIdx, r.AuthHash, f.UserSignature, f.OrderSignature, at, f.PlatformSignature, r.DispatchHash)
			if _, err = orders.Authorize(ctx, user.Actor{UserID: 1, AuthVersion: 1}, q.QuoteIdx, f.UserSignature, f.OrderSignature); err != nil {
				t.Fatal("idempotent authorization", err)
			}
			if _, err = orders.Get(ctx, 2, q.QuoteIdx); err == nil {
				t.Fatal("other user read deposit")
			}
			// Reserve the signed user body before the receiving transaction hash exists.
			req := chainreceipt.TONRequest{Network: chainassets.TONTestnet, Hash: receipt.Transaction.Signature, AssetID: q.AssetID, Recipient: r.Recipient, Atoms: r.Atoms.String()}
			if _, err = chainreceipt.NewTON(conn, runner, n).Record(ctx, req); err != nil {
				t.Fatal(err)
			}
			mustExec(t, db, "INSERT INTO chain_receive_address(id,network,owner,route) VALUES(1,?,?,'single_use')", chainassets.TONTestnet, r.Recipient)
			mustExec(t, db, "INSERT INTO chain_receive_request(request_key,address_id,payment_reference,purpose,beneficiary_id,payer_owner,asset_id,amount_atoms,opens_at,expires_at) VALUES('merchant',1,'','merchant_payment',2,?,?,?,1,4102444800)", r.Sender, q.AssetID, r.Atoms.String())
			if _, err = chainreceipt.NewAttribution(conn, nil).Match(ctx, chainreceipt.EventKey{Network: chainassets.TONTestnet, Signature: req.Hash, Kind: chainreceipt.TONIncoming}); !errors.Is(err, chainreceipt.ErrReceiveConflict) {
				t.Fatal("merchant claimed relay", err)
			}
			mustExec(t, db, "UPDATE chain_receive_request SET matched_event_id=(SELECT id FROM chain_transfer_event LIMIT 1)")
			if caseName == "credit" {
				if err = step(); !errors.Is(err, chainreceipt.ErrReceiveConflict) {
					t.Fatal("credited allocated receipt", err)
				}
			}
			mustExec(t, db, "UPDATE chain_receive_request SET matched_event_id=NULL")
			n.evidence.Transaction = nil
			if o, e := worker.Step(ctx, q.QuoteIdx, deposit.TonBudget{}, nil); e != nil || (o.State != deposit.TonReview && o.State != deposit.TonSigned) || o.CreditVoucherId.Valid {
				t.Fatal("partial receipt", e)
			}
			n.evidence = f.Evidence
			// A finalized W5 that skipped its send action cannot be credited by a stale success.
			var failed map[string]any
			if err = json.Unmarshal(f.Evidence.Parent, &failed); err != nil {
				t.Fatal(err)
			}
			tx := failed["transactions"].([]any)[0].(map[string]any)
			tx["description"].(map[string]any)["aborted"] = true
			n.evidence.Parent, _ = json.Marshal(failed)
			if e := step(); e != nil {
				t.Fatal(e)
			}
			n.evidence = f.Evidence
			if row, e := worker.Step(ctx, q.QuoteIdx, deposit.TonBudget{}, nil); e != nil || row.ExecutionFailed != 1 || row.CreditVoucherId.Valid {
				t.Fatal("credited known failed execution", e)
			}
			mustExec(t, db, "UPDATE ton_deposit_order SET execution_failed=0")
			done := make(chan error, 2)
			for range 2 {
				go func() { done <- step() }()
			}
			for range 2 {
				if e := <-done; e != nil {
					t.Fatal(e)
				}
			}
			o, err := orders.Get(ctx, 1, q.QuoteIdx)
			if err != nil || o.ReceiveHash.String != req.Hash || o.ActualFeeNano.Int64 != fee.Int64() {
				t.Fatal("actual cost or receiving identity", err)
			}
			page, err := ledger.Entries(ctx, 1, accounts.KindUSD, 0, 2)
			if err != nil {
				t.Fatal(err)
			}
			if caseName == "over-cap" {
				if o.State != deposit.TonReview || o.CreditVoucherId.Valid || len(page.Entries) != 0 || fee.Int64() != 1287671 {
					t.Fatal("credited an over-cap order")
				}
			} else {
				if o.State != deposit.TonCredited || len(page.Entries) != 1 || page.Entries[0].Action != "credit_ton_native" || page.Entries[0].DepositOrderIdx != q.QuoteIdx || page.Entries[0].BalanceAfter.StringFixed(2) != "0.01" || fee.Int64() != 1163296 {
					t.Fatal("incorrect TON statement")
				}
				cli := exec.Command("go", "run", "./tools/ton_deposit_testnet", "-quote", q.QuoteIdx, "-rpc", "http://127.0.0.1:1")
				cli.Dir = "../.."
				cli.Env = append(os.Environ(), "DEXWALLET_MYSQL_DSN="+dsn)
				if out, e := cli.CombinedOutput(); e != nil || !strings.Contains(string(out), "credited_usd=0.01") {
					t.Fatalf("keyless CLI %v %s", e, out)
				}
			}
			if err = m.Migrate(46); err == nil || !strings.Contains(err.Error(), "ton_deposit_rollback_guard") {
				t.Fatal("rollback discarded TON order", err)
			}
		})
	}
}
