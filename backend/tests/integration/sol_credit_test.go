//go:build integration

package integration

import (
	"context"
	"crypto/ed25519"
	"database/sql"
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
	accounts "github.com/0xabcd-k/DexWallet/backend/model/account"
	"github.com/0xabcd-k/DexWallet/backend/transaction"
	"github.com/mr-tron/base58"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

// Replay the public terms and signatures of the previously finalized Devnet order.
// The optional RPC lookup is read-only. The isolated ledger starts at schema 39.
func TestSolDepositCreditRecovery(t *testing.T) {
	ctx := context.Background()
	var fixture struct {
		Quote                      chainassets.DepositPreview
		AcceptedAt                 time.Time
		UserSignature, TxSignature string
		Slot                       int64
	}
	raw, err := os.ReadFile("testdata/sol-credit-devnet.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	q := fixture.Quote
	terms := chainassets.SolDepositTerms{Recipient: q.Recipient, GrossLamports: 110000, FeeLamports: 10000}
	msg, err := terms.Check(q, q.FeePayer, fixture.UserSignature)
	if err != nil {
		t.Fatal(err)
	}
	payer, _ := base58.Decode(q.FeePayer)
	sig, err := base58.Decode(fixture.TxSignature)
	if err != nil || !ed25519.Verify(payer, msg, sig) {
		t.Fatal("fixture sponsor signature")
	}
	if os.Getenv("DEXWALLET_TEST_SOL_VERIFY_HISTORY") == "1" {
		endpoint := os.Getenv("DEXWALLET_TEST_SOL_RPC")
		if endpoint == "" {
			endpoint = "https://api.devnet.solana.com"
		}
		service, err := chainassets.New(chainassets.Config{Timeout: 20 * time.Second, Networks: []chainassets.Endpoint{{Network: chainassets.SolanaDevnet, URL: endpoint}}}, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer service.Close()
		userSig, _ := base58.Decode(fixture.UserSignature)
		wire := append(append(append([]byte{2}, sig...), userSig...), msg...)
		seen, err := service.ObserveSolDeposit(ctx, wire, terms)
		if err != nil || !seen.Finalized || seen.Failed || seen.Slot != fixture.Slot || seen.Fee != terms.FeeLamports {
			t.Fatal("public receipt", seen, err)
		}
		t.Logf("Devnet read-only receipt: %s slot %d fee %d", fixture.TxSignature, seen.Slot, seen.Fee)
	}
	name, dsn, db, err := testRun.NewDatabase(ctx, "sol_credit")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	m, err := migration.Open(dsn, name, "../../manifest/sql/diff/business", "business")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if err = m.Migrate(39); err != nil {
		t.Fatal(err)
	}
	mustExec(t, db, "INSERT INTO user(id,nickname) VALUES(1,'devnet')")
	snapshot, _ := json.Marshal(q)
	mustExec(t, db, "INSERT INTO deposit_quote_confirmation(quote_idx,user_id,snapshot_json,quote_expires_at,confirmed_at) VALUES(?,1,?,?,?)", q.QuoteIdx, snapshot, time.UnixMilli(q.ExpiresAt).UTC(), fixture.AcceptedAt)
	mustExec(t, db, `INSERT INTO sol_deposit_order(quote_idx,order_idx,message_base64,message_sha256,last_valid_height,review_after,user_signature,accepted_at,execution_state,sponsor_policy,tx_signature,actual_lamports_fee,finalized_slot)
	VALUES(?,?,?,?,?,?,?,?,'finalized','devnet-sol-single-order-v1',?,?,?)`, q.QuoteIdx, q.SolOrderIdx, q.SolMessage, q.MessageSHA256, q.SolLastValidHeight, time.UnixMilli(q.SolReviewAt).UTC(), fixture.UserSignature, fixture.AcceptedAt, fixture.TxSignature, terms.FeeLamports, fixture.Slot)
	for _, v := range []uint{41, 39, 41} {
		if err = m.Migrate(v); err != nil {
			t.Fatal(v, err)
		}
	}
	conn := sqlx.NewSqlConnFromDB(db)
	ledger := account.New(accounts.NewUserAccountModel(conn))
	credit := func() error { _, err := deposit.CreditSol(ctx, transaction.New(db), ledger, q.QuoteIdx); return err }
	checkSolCreditAttribution(t, db, conn, q, fixture.TxSignature, credit)

	// An altered snapshot cannot credit a higher gross amount, even when its
	// USD arithmetic still balances. It must agree with the signed transfer.
	changed := q
	changed.PayQuantity = "0.00021"
	changed.NetUSD = "0.02"
	b, _ := json.Marshal(changed)
	mustExec(t, db, "UPDATE deposit_quote_confirmation SET snapshot_json=? WHERE quote_idx=?", b, q.QuoteIdx)
	if err = credit(); err == nil {
		t.Fatal("altered gross credited")
	}
	mustExec(t, db, "UPDATE deposit_quote_confirmation SET snapshot_json=? WHERE quote_idx=?", snapshot, q.QuoteIdx)
	for range 2 {
		cmd := exec.Command("go", "run", "./tools/sol_deposit_devnet", "-settle-only", "-quote", q.QuoteIdx)
		cmd.Dir = "../.."
		cmd.Env = append(os.Environ(), "DEXWALLET_MYSQL_DSN="+dsn)
		out, err := cmd.CombinedOutput()
		if err != nil || !strings.Contains(string(out), "credited_usd=0.01") {
			t.Fatalf("keyless recovery: %v %s", err, out)
		}
	}
	page, err := ledger.Entries(ctx, 1, accounts.KindUSD, 0, 2)
	if err != nil || len(page.Entries) != 1 || page.Entries[0].DepositOrderIdx != q.SolOrderIdx || page.Entries[0].BalanceAfter.StringFixed(2) != "0.01" {
		t.Fatal("recovered statement", page, err)
	}
	t.Logf("order %s credited once: voucher=%d USD=%s", q.SolOrderIdx, page.Entries[0].VoucherID, page.Entries[0].Amount.StringFixed(2))
	if err = m.Migrate(39); err == nil || !strings.Contains(err.Error(), "sol_credit_rollback_guard") {
		t.Fatal("downgrade dropped credit", err)
	}
}

func checkSolCreditAttribution(t *testing.T, db *sql.DB, conn sqlx.SqlConn, q chainassets.DepositPreview, sig string, credit func() error) {
	t.Helper()
	// A synthetic competing merchant request matches every transfer field.
	// Its attribution must yield to the previously persisted sponsor signature.
	mustExec(t, db, `INSERT INTO chain_observed_transaction(id,network,signature,slot,fee_payer,fee_atoms,fingerprint,evidence_json)
	VALUES(1,?,?,526729996,?,'10000',?,?)`, chainassets.SolanaDevnet, sig, q.FeePayer, strings.Repeat("a", 64), `{"transaction":{"blockTime":1795749355}}`)
	mustExec(t, db, `INSERT INTO chain_transfer_event(id,transaction_id,instruction_index,asset_id,program_id,mint,decimals,source_account,source_owner,recipient_account,recipient_owner,amount_atoms)
	VALUES(1,1,0,?,'11111111111111111111111111111111','',9,?,?,?,?,'110000')`, q.AssetID, q.WalletAddress, q.WalletAddress, q.Recipient, q.Recipient)
	mustExec(t, db, "INSERT INTO chain_receive_address(id,network,owner,route) VALUES(1,?,?,'single_use')", chainassets.SolanaDevnet, q.Recipient)
	mustExec(t, db, `INSERT INTO chain_receive_request(request_key,address_id,payment_reference,purpose,beneficiary_id,payer_owner,asset_id,amount_atoms,opens_at,expires_at)
	VALUES('test-merchant',1,'','merchant_payment',2,?,?,'110000',1,4102444800)`, q.WalletAddress, q.AssetID)
	key := chainreceipt.EventKey{Network: chainassets.SolanaDevnet, Signature: sig, Kind: chainreceipt.SolanaInstruction}
	if _, err := chainreceipt.NewAttribution(conn, nil).Match(context.Background(), key); !errors.Is(err, chainreceipt.ErrReceiveConflict) {
		t.Fatal("merchant attribution", err)
	}
	// A sponsored USDC order may create the receive ATA at index 0 and transfer at index 1.
	mustExec(t, db, "UPDATE chain_transfer_event SET instruction_index=1 WHERE id=1")
	key.Index = 1
	if _, err := chainreceipt.NewAttribution(conn, nil).Match(context.Background(), key); !errors.Is(err, chainreceipt.ErrReceiveConflict) {
		t.Fatal("second instruction escaped reservation", err)
	}
	mustExec(t, db, "UPDATE chain_receive_request SET matched_event_id=1 WHERE id=1")
	if err := credit(); !errors.Is(err, chainreceipt.ErrReceiveConflict) {
		t.Fatal("legacy matched receipt credited", err)
	}
	mustExec(t, db, "UPDATE chain_receive_request SET matched_event_id=NULL WHERE id=1")
}
