//go:build integration

package integration

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/logic/account"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
	"github.com/0xabcd-k/DexWallet/backend/logic/deposit"
	accounts "github.com/0xabcd-k/DexWallet/backend/model/account"
	"github.com/0xabcd-k/DexWallet/backend/transaction"
	"github.com/mr-tron/base58"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

// Recover both public Devnet orders into one isolated user's USD ledger without a signer.
func TestUSDCDepositCreditRecovery(t *testing.T) {
	ctx := context.Background()
	db, _ := userDatabase(t, "usdc_credit")
	mustExec(t, db, "INSERT INTO user(id,nickname) VALUES(1,'devnet')")
	conn := sqlx.NewSqlConnFromDB(db)
	ledger := account.New(accounts.NewUserAccountModel(conn))
	tx := transaction.New(db)
	orders := map[string]bool{}
	for _, tc := range []struct {
		name    string
		funding int64
	}{{"create", 1488440}, {"existing", 0}} {
		var f struct {
			Quote       chainassets.DepositPreview
			Signature   string
			Transaction struct {
				Slot        int64
				Transaction []string
			}
		}
		raw, err := os.ReadFile("../../logic/chainassets/testdata/usdc-self-deposit-" + tc.name + ".json")
		if err != nil {
			t.Fatal(err)
		}
		if err = json.Unmarshal(raw, &f); err != nil {
			t.Fatal(err)
		}
		q := f.Quote
		snapshot, err := json.Marshal(q)
		if err != nil {
			t.Fatal(err)
		}
		wire, err := base64.StdEncoding.DecodeString(f.Transaction.Transaction[0])
		if err != nil {
			t.Fatal(err)
		}
		at := time.UnixMilli(q.QuotedAt).UTC()
		mustExec(t, db, `INSERT INTO deposit_quote_confirmation(quote_idx,user_id,snapshot_json,quote_expires_at,confirmed_at) VALUES(?,1,?,?,?)`, q.QuoteIdx, snapshot, time.UnixMilli(q.ExpiresAt).UTC(), at)
		mustExec(t, db, `INSERT INTO sol_deposit_order(asset_id,quote_idx,order_idx,message_base64,message_sha256,last_valid_height,review_after,user_signature,accepted_at,execution_state,sponsor_policy,tx_signature,actual_lamports_fee,actual_funding_lamports,finalized_slot)
    VALUES(?,?,?,?,?,?,?,?,?,'finalized','devnet-sol-single-order-v1',?,10000,?,?)`, q.AssetID, q.QuoteIdx, q.SolOrderIdx, q.SolMessage, q.MessageSHA256, q.SolLastValidHeight, time.UnixMilli(q.SolReviewAt).UTC(), base58.Encode(wire[65:129]), at, f.Signature, tc.funding, f.Transaction.Slot)
		order, err := deposit.CreditSol(ctx, tx, ledger, q.QuoteIdx)
		if err != nil {
			t.Fatal(err)
		}
		replay, err := deposit.CreditSol(ctx, tx, ledger, q.QuoteIdx)
		if err != nil || !order.CreditVoucherId.Valid || replay.CreditVoucherId != order.CreditVoucherId {
			t.Fatal("replayed USDC credit", err)
		}
		orders[q.SolOrderIdx] = true
	}
	page, err := ledger.Entries(ctx, 1, accounts.KindUSD, 0, 3)
	if err != nil || len(page.Entries) != 2 || page.Entries[0].BalanceAfter.StringFixed(2) != "0.02" {
		t.Fatal("USD statement", page, err)
	}
	for _, e := range page.Entries {
		if !orders[e.DepositOrderIdx] || e.Amount.StringFixed(2) != "0.01" || e.Action != string(deposit.ActionCreditUSDC) {
			t.Fatal("unexpected USDC credit", e)
		}
		delete(orders, e.DepositOrderIdx)
	}
}
