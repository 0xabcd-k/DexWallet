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

	"github.com/0xabcd-k/DexWallet/backend/internal/baseusdc"
	"github.com/0xabcd-k/DexWallet/backend/internal/migration"
	"github.com/0xabcd-k/DexWallet/backend/logic/account"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainreceipt"
	"github.com/0xabcd-k/DexWallet/backend/logic/deposit"
	"github.com/0xabcd-k/DexWallet/backend/logic/user"
	accounts "github.com/0xabcd-k/DexWallet/backend/model/account"
	"github.com/0xabcd-k/DexWallet/backend/transaction"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

// Only historical lookups and the asset catalog are available to a restored worker.
type historicalBaseDeposit struct {
	deposit.BaseDepositNetwork
	e    chainassets.BaseEvidence
	hash string
}

func (n *historicalBaseDeposit) ReadBaseEvidence(_ context.Context, network, hash string) (chainassets.BaseEvidence, error) {
	if network != chainassets.BaseSepolia || hash != n.hash {
		return chainassets.BaseEvidence{}, errors.New("unexpected history lookup")
	}
	return n.e, nil
}
func (n *historicalBaseDeposit) Catalog() []chainassets.Network {
	return []chainassets.Network{{ID: chainassets.BaseSepolia, Family: chainassets.Base, Assets: []chainassets.Asset{{ID: baseusdc.AssetID, Origin: chainassets.Issuer, Decimals: 6, Contract: baseusdc.SepoliaContract}}}}
}
func TestBaseUSDCDepositRecovery(t *testing.T) {
	var f struct {
		Quote               chainassets.DepositPreview
		Wire, UserSignature string
		Evidence            chainassets.BaseEvidence
	}
	raw, err := os.ReadFile("../../logic/chainreceipt/testdata/base-usdc-self-deposit.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	var signed types.Transaction
	wire, err := hexutil.Decode(f.Wire)
	if err != nil {
		t.Fatal(err)
	}
	if err = signed.UnmarshalBinary(wire); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	name, dsn, db, err := testRun.NewDatabase(ctx, "base_deposit_history")
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
	q := f.Quote
	snap, _ := json.Marshal(q)
	at := time.UnixMilli(q.QuotedAt).UTC()
	mustExec(t, db, "INSERT INTO user(id,nickname) VALUES(1,'base-dev')")
	mustExec(t, db, "INSERT INTO deposit_quote_confirmation(quote_idx,user_id,snapshot_json,quote_expires_at,confirmed_at) VALUES(?,1,?,?,?)", q.QuoteIdx, snap, time.UnixMilli(q.ExpiresAt).UTC(), at)
	conn := sqlx.NewSqlConnFromDB(db)
	runner := transaction.New(db)
	ledger := account.New(accounts.NewUserAccountModel(conn))
	orders := deposit.NewBaseOrders(conn, deposit.NewConfirmations(conn, nil, ""), nil, nil, nil)
	o, err := orders.Get(ctx, 1, q.QuoteIdx)
	if err != nil || o.State != deposit.BaseQuoteExpired {
		t.Fatal("unadmitted historical quote", err)
	}
	if _, err = orders.Get(ctx, 2, q.QuoteIdx); err == nil {
		t.Fatal("another user read original quote")
	}
	n := &historicalBaseDeposit{e: f.Evidence, hash: signed.Hash().Hex()}
	verified, err := chainreceipt.VerifyBaseDeposit(n.e, &signed, q, f.UserSignature)
	if err != nil {
		t.Fatal(err)
	}
	pos := verified.Event.Instruction
	_, err = chainreceipt.NewBase(conn, runner, n).Record(ctx, chainreceipt.BaseRequest{Network: chainassets.BaseSepolia, AssetID: q.AssetID, Hash: n.hash, Recipient: q.Recipient, Atoms: verified.Event.Atoms, LogIndex: &pos})
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, db, "INSERT INTO chain_receive_address(id,network,owner,route) VALUES(1,?,?,'single_use')", chainassets.BaseSepolia, q.Recipient)
	mustExec(t, db, `INSERT INTO chain_receive_request(request_key,address_id,payment_reference,purpose,beneficiary_id,payer_owner,asset_id,amount_atoms,opens_at,expires_at) VALUES('merchant',1,'','merchant_payment',2,?,?,?,1,4102444800)`, strings.ToLower(q.WalletAddress), q.AssetID, verified.Event.Atoms)
	key := chainreceipt.EventKey{Network: chainassets.BaseSepolia, Signature: n.hash, Kind: chainreceipt.EVMLog, Index: pos}
	if _, err = chainreceipt.NewAttribution(conn, nil).Match(ctx, key); !errors.Is(err, chainreceipt.ErrReceiveConflict) {
		t.Fatal("reserved order nonce before signature admission", err)
	}
	mustExec(t, db, `INSERT INTO base_deposit_order(quote_idx,user_signature,accepted_at,state,sponsor,sponsor_nonce,signed_wire,tx_hash) VALUES(?,?,?,'signed',?,?,?,?)`, q.QuoteIdx, f.UserSignature, at, q.FeePayer, signed.Nonce(), f.Wire, n.hash)
	actor := user.Actor{UserID: 1, AuthVersion: 1}
	replayed, err := orders.Authorize(ctx, actor, q.QuoteIdx, f.UserSignature)
	if err != nil || replayed.TxHash.String != n.hash {
		t.Fatal("replay after quote expired", err)
	}
	if _, err = orders.Authorize(ctx, user.Actor{UserID: 2, AuthVersion: 1}, q.QuoteIdx, f.UserSignature); err == nil {
		t.Fatal("replayed authorization as another owner")
	}
	if _, err = orders.Authorize(ctx, actor, q.QuoteIdx, "0x"+strings.Repeat("00", 65)); err == nil {
		t.Fatal("accepted forged signature")
	}
	w := deposit.NewBaseExecution(conn, runner, ledger, n)
	step := func() error { _, e := w.Step(ctx, q.QuoteIdx, deposit.BaseBudget{}, nil); return e }
	n.e.Finalized = json.RawMessage(`{"hash":"0x` + strings.Repeat("1", 64) + `","number":"0x1"}`)
	if err = step(); err != nil {
		t.Fatal(err)
	}
	page, err := ledger.Entries(ctx, 1, accounts.KindUSD, 0, 2)
	if err != nil || len(page.Entries) != 0 {
		t.Fatal("credit before finality", err)
	}
	n.e = f.Evidence
	mustExec(t, db, "UPDATE chain_receive_request SET matched_event_id=(SELECT id FROM chain_transfer_event LIMIT 1)")
	if err = step(); !errors.Is(err, chainreceipt.ErrReceiveConflict) {
		t.Fatal("legacy merchant match credited", err)
	}
	mustExec(t, db, "UPDATE chain_receive_request SET matched_event_id=NULL")
	n.e.OperatorFee = json.RawMessage(`"0x` + strings.Repeat("0", 48) + `ffffffffffffffff"`)
	o, err = w.Step(ctx, q.QuoteIdx, deposit.BaseBudget{}, nil)
	if err != nil || o.State != deposit.BaseReview || o.ActualFeeWei == "" || o.CreditVoucherId.Valid {
		t.Fatal("over-cap review", err)
	}
	// Restore the isolated DB snapshot to exercise the original fee and competing workers.
	mustExec(t, db, "UPDATE base_deposit_order SET state='signed',actual_fee_wei='',finalized_height=0")
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
	if err != nil || len(page.Entries) != 1 || page.Entries[0].DepositOrderIdx != q.QuoteIdx || page.Entries[0].Action != "credit_base_usdc" || page.Entries[0].BalanceAfter.StringFixed(2) != "0.01" {
		t.Fatal("owner statement", err, page)
	}
	cmd := exec.Command("go", "run", "./tools/base_deposit_sepolia", "-quote", q.QuoteIdx, "-rpc", "http://127.0.0.1:1")
	cmd.Dir = "../.."
	cmd.Env = append(os.Environ(), "DEXWALLET_MYSQL_DSN="+dsn)
	if out, err := cmd.CombinedOutput(); err != nil || !strings.Contains(string(out), "credited_usd=0.01") {
		t.Fatalf("keyless CLI %v: %s", err, out)
	}
	if err = m.Migrate(42); err == nil || !strings.Contains(err.Error(), "base_deposit_rollback_guard") {
		t.Fatal("downgrade dropped the order", err)
	}
}
