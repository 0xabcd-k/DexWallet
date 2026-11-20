//go:build integration

package integration

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/config"
	"github.com/0xabcd-k/DexWallet/backend/internal/migration"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainreceipt"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainsend"
	model "github.com/0xabcd-k/DexWallet/backend/model/chainsend"
	"github.com/0xabcd-k/DexWallet/backend/svc"
	"github.com/0xabcd-k/DexWallet/backend/transaction"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

type basePayoutCapture struct {
	Intent   model.BasePayout
	Receipt  chainreceipt.Receipt
	Evidence chainassets.BaseEvidence
}

func loadBasePayout(t *testing.T, name string) basePayoutCapture {
	t.Helper()
	raw, err := os.ReadFile("../../logic/chainreceipt/testdata/" + name + ".json")
	if err != nil {
		t.Fatal(err)
	}
	var c basePayoutCapture
	if err = json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	var tx types.Transaction
	if err = json.Unmarshal(c.Evidence.Transaction, &tx); err != nil {
		t.Fatal(err)
	}
	wire, err := tx.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	c.Intent.SignedTx = hexutil.Encode(wire)
	c.Intent.AssetId = c.Receipt.Event.AssetID // Native ETH capture predates business 27.
	return c
}

// Restore the original authorization using only business 27 fields.
func restoreBasePayout(t *testing.T, db *sql.DB, p model.BasePayout, state string) {
	t.Helper()
	if state == chainsend.Prepared {
		p.TxHash, p.SignedTx = "", ""
	}
	if state != chainsend.BaseConfirmed {
		p.TransferLogIndex = sql.NullInt64{}
	}
	mustExec(t, db, "INSERT INTO base_payout(request_key,chain_id,sender,recipient,asset_id,wei_amount,token_atoms,nonce,gas_limit,fee_cap,tip_cap,signing_hash,state,tx_hash,signed_tx,transfer_log_index) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)", p.RequestKey, p.ChainId, p.Sender, p.Recipient, p.AssetId, p.WeiAmount, p.TokenAtoms, p.Nonce, p.GasLimit, p.FeeCap, p.TipCap, p.SigningHash, state, p.TxHash, p.SignedTx, p.TransferLogIndex)
}

func baseScheduler(t *testing.T, db *sql.DB, sender string, node chainsend.BaseNode, source chainreceipt.BaseSource, attempts int64) *chainsend.BaseRecovery {
	t.Helper()
	conf := chainsend.BaseRecoveryConfig{Sender: common.HexToAddress(sender).Hex(), RecoveryPolicy: chainsend.DefaultRecoveryPolicy()}
	conf.MaxAttempts = attempts
	r, err := chainsend.NewBaseRecovery(db, node, chainreceipt.NewBase(sqlx.NewSqlConnFromDB(db), transaction.New(db), source), conf)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func noBaseBroadcast(t *testing.T) *baseUSDCPayoutNode {
	t.Helper()
	deny := func(*types.Transaction) error {
		t.Error("unexpected Base broadcast")
		return errors.New("broadcast disabled")
	}
	return &baseUSDCPayoutNode{&basePayoutNode{submit: deny}, deny}
}

func TestBaseRecoveryHistorical(t *testing.T) { verifyBaseRecoveryHistory(t, false) }
func TestLiveBaseRecoveryHistorical(t *testing.T) {
	if os.Getenv("DEXWALLET_TEST_BASE_RECOVERY_LIVE") != "1" {
		t.Skip("read-only Sepolia verification requires explicit opt-in")
	}
	verifyBaseRecoveryHistory(t, true)
}

func verifyBaseRecoveryHistory(t *testing.T, live bool) {
	db, _ := userDatabase(t, "base_recovery_history")
	var liveSource *chainassets.Service
	if live {
		url := os.Getenv("DEXWALLET_CHAIN_BASE_SEPOLIA_RPC_URL")
		if url == "" {
			url = "https://sepolia.base.org"
		}
		var err error
		liveSource, err = chainassets.New(chainassets.Config{Timeout: 30 * time.Second, Networks: []chainassets.Endpoint{{Network: chainassets.BaseSepolia, URL: url}}}, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer liveSource.Close()
	}
	for _, tc := range []struct{ name, state string }{{"base-payout-eth", chainsend.Signed}, {"base-payout-usdc", chainsend.Submitted}} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			c := loadBasePayout(t, tc.name)
			restoreBasePayout(t, db, c.Intent, tc.state)
			var source chainreceipt.BaseSource = &baseReceiptSource{proof: c.Evidence}
			if liveSource != nil {
				source = liveSource
			}
			r := baseScheduler(t, db, c.Intent.Sender, noBaseBroadcast(t), source, 4)
			if worked, err := r.RunOne(ctx); !worked || err != nil {
				t.Fatal(worked, err)
			}
			if worked, err := r.RunOne(ctx); worked || err != nil {
				t.Fatal("confirmed payout reclaimed", worked, err)
			}
			rows, err := r.List(ctx, chainsend.RecoveryConfirmed, 10)
			if err != nil || len(rows) == 0 {
				t.Fatal(rows, err)
			}
			row := rows[len(rows)-1]
			if row.TxHash != c.Intent.TxHash || row.AssetID != c.Receipt.Event.AssetID || row.Atoms != c.Receipt.Event.Atoms || row.Nonce != c.Intent.Nonce || row.SendState != chainsend.BaseConfirmed || row.Attempts != 1 || row.BroadcastAttempts != 0 || !row.CheckedAt.Valid || row.LeaseUntil.Valid || row.TransferLogIndex != c.Intent.TransferLogIndex {
				t.Fatal(row)
			}
			store := chainreceipt.NewBase(sqlx.NewSqlConnFromDB(db), transaction.New(db), source)
			got, err := store.Find(ctx, chainassets.BaseSepolia, row.TxHash, c.Receipt.Event.Kind, c.Receipt.Event.Instruction)
			if err != nil || got.Event != c.Receipt.Event || got.Transaction != c.Receipt.Transaction {
				t.Fatal("recovered receipt differs from archived payout", got, err)
			}
			t.Logf("%s hash=%s event=%s/%d fee=%s wei broadcasts=%d", row.AssetID, row.TxHash, got.Event.Kind, got.Event.Instruction, got.Transaction.FeeAtoms, row.BroadcastAttempts)
		})
	}
	for table, want := range map[string]int{"base_payout": 2, "chain_observed_transaction": 2, "chain_transfer_event": 2, "ledger_voucher": 0, "event_outbox": 0} {
		var count int
		if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil || count != want {
			t.Fatal(table, count, err)
		}
	}
}

type delayedBaseReceipt struct {
	*baseReceiptSource
	entered, release chan struct{}
}

func (s *delayedBaseReceipt) ReadBaseEvidence(ctx context.Context, network, hash string) (chainassets.BaseEvidence, error) {
	close(s.entered)
	select {
	case <-s.release:
		return s.baseReceiptSource.ReadBaseEvidence(ctx, network, hash)
	case <-ctx.Done():
		return chainassets.BaseEvidence{}, ctx.Err()
	}
}

func TestBaseRecoveryLeaseNonceReviewAndRetry(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, _ := userDatabase(t, "base_recovery_lease")
	c := loadBasePayout(t, "base-payout-eth")
	restoreBasePayout(t, db, c.Intent, chainsend.Signed)
	src := &baseReceiptSource{proof: chainassets.BaseEvidence{Transaction: json.RawMessage("null"), Receipt: json.RawMessage("null")}}
	slow := &delayedBaseReceipt{src, make(chan struct{}), make(chan struct{})}
	oldNode := noBaseBroadcast(t)
	oldNode.nonce = 6 // The stale worker sees the original nonce, and must still be fenced.
	newNode := noBaseBroadcast(t)
	newNode.nonce = 7
	late := baseScheduler(t, db, c.Intent.Sender, oldNode, slow, 3)
	current := baseScheduler(t, db, c.Intent.Sender, newNode, src, 3)
	other := baseScheduler(t, db, c.Intent.Recipient, newNode, src, 3)
	if worked, err := other.RunOne(ctx); worked || err != nil {
		t.Fatal("wrong sender admitted", worked, err)
	}
	done := make(chan error, 1)
	go func() { _, err := late.RunOne(ctx); done <- err }()
	select {
	case <-slow.entered:
	case err := <-done:
		t.Fatal(err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if worked, err := current.RunOne(ctx); worked || err != nil {
		t.Fatal("active lease admitted", worked, err)
	}
	mustExec(t, db, "UPDATE base_payout SET recovery_lease_until=TIMESTAMPADD(SECOND,-1,UTC_TIMESTAMP(6))")
	if worked, err := current.RunOne(ctx); !worked || !errors.Is(err, chainsend.ErrBaseNonceChanged) {
		t.Fatal(worked, err)
	}
	close(slow.release)
	if err := <-done; !errors.Is(err, chainsend.ErrRecoveryLeaseLost) {
		t.Fatal("stale worker overwrote review", err)
	}
	rows, err := current.List(ctx, chainsend.RecoveryReview, 2)
	if err != nil || len(rows) != 1 || rows[0].Generation != 2 || rows[0].Attempts != 2 || rows[0].BroadcastAttempts != 0 || rows[0].LeaseUntil.Valid || !strings.Contains(rows[0].Error, "pending=7") {
		t.Fatal(rows, err)
	}
	if worked, err := current.RunOne(ctx); worked || err != nil {
		t.Fatal("review was reclaimed", worked, err)
	}
	executor, err := chainsend.NewBase(sqlx.NewSqlConnFromDB(db), c.Intent.Sender, oldNode, chainreceipt.NewBase(sqlx.NewSqlConnFromDB(db), transaction.New(db), src))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := executor.Step(ctx, c.Intent.RequestKey); err == nil {
		t.Fatal("explicit step bypassed review")
	}
	if err = other.Retry(ctx, c.Intent.RequestKey, "operator", "wrong sender"); !errors.Is(err, chainsend.ErrRecoveryRetryDenied) {
		t.Fatal(err)
	}
	if err = current.Retry(ctx, c.Intent.RequestKey, "operator", "checked nonce and original hash"); err != nil {
		t.Fatal(err)
	}
	src.proof = c.Evidence
	if worked, err := current.RunOne(ctx); !worked || err != nil {
		t.Fatal(worked, err)
	}
	rows, err = current.List(ctx, chainsend.RecoveryConfirmed, 1)
	if err != nil || len(rows) != 1 || rows[0].TxHash != c.Intent.TxHash || rows[0].Generation != 4 || rows[0].Attempts != 1 || rows[0].BroadcastAttempts != 0 || !strings.Contains(rows[0].RetryNote, "checked nonce") {
		t.Fatal(rows, err)
	}
	if err = current.Retry(ctx, c.Intent.RequestKey, "operator", "again"); !errors.Is(err, chainsend.ErrRecoveryRetryDenied) {
		t.Fatal(err)
	}
}

func TestBaseRecoveryLostAckAndOfflineRuntime(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, _ := userDatabase(t, "base_recovery_restart")
	c := loadBasePayout(t, "base-payout-usdc")
	restoreBasePayout(t, db, c.Intent, chainsend.Signed)
	src := &baseReceiptSource{proof: chainassets.BaseEvidence{Transaction: json.RawMessage("null"), Receipt: json.RawMessage("null")}}
	calls := 0
	node := noBaseBroadcast(t)
	node.nonce = 7
	node.send = func(tx *types.Transaction) error {
		actual, err := tx.MarshalBinary()
		if err != nil || !bytes.Equal(actual, common.FromHex(c.Intent.SignedTx)) {
			t.Fatalf("replay changed signed payload: %v", err)
		}
		calls++
		if calls == 1 {
			return io.ErrUnexpectedEOF
		}
		return nil
	}
	r := baseScheduler(t, db, c.Intent.Sender, node, src, 2)
	due := func() {
		mustExec(t, db, "UPDATE base_payout SET recovery_next_at=UTC_TIMESTAMP(6) WHERE request_key=?", c.Intent.RequestKey)
	}
	if worked, err := r.RunOne(ctx); !worked || !errors.Is(err, chainsend.ErrOutcomeUnknown) {
		t.Fatal(worked, err)
	}
	if worked, err := r.RunOne(ctx); worked || err != nil {
		t.Fatal("retry interval ignored", worked, err)
	}
	due()
	if worked, err := r.RunOne(ctx); !worked || !errors.Is(err, chainsend.ErrRecoveryBudget) || !errors.Is(err, chainreceipt.ErrUnconfirmed) {
		t.Fatal(worked, err)
	}
	rows, err := r.List(ctx, chainsend.RecoveryReview, 1)
	if err != nil || len(rows) != 1 || rows[0].SendState != chainsend.Submitted || rows[0].TxHash != c.Intent.TxHash || rows[0].BroadcastAttempts != 2 || calls != 2 {
		t.Fatal(rows, err, calls)
	}
	if err = r.Retry(ctx, c.Intent.RequestKey, "operator", "recheck original USDC transfer"); err != nil {
		t.Fatal(err)
	}
	// The node first sees the transaction pending, then explicitly drops it.
	// A submitted Base payout can replay its original bytes at the same nonce.
	src.proof.Transaction = c.Evidence.Transaction
	if _, err := r.RunOne(ctx); !errors.Is(err, chainreceipt.ErrUnconfirmed) || calls != 2 {
		t.Fatal("pending transaction was replayed", err, calls)
	}
	src.proof.Transaction = json.RawMessage("null")
	due()
	if _, err := r.RunOne(ctx); !errors.Is(err, chainsend.ErrRecoveryBudget) || calls != 3 {
		t.Fatal("dropped submitted transaction not replayed", err, calls)
	}
	if err = r.Retry(ctx, c.Intent.RequestKey, "operator", "observed original hash after replay"); err != nil {
		t.Fatal(err)
	}
	src.proof = c.Evidence
	mustExec(t, db, "ALTER TABLE base_payout ADD CONSTRAINT base_test_recovery_write CHECK (state<>'confirmed')")
	if _, err := r.RunOne(ctx); err == nil || !strings.Contains(err.Error(), "base_test_recovery_write") {
		t.Fatal("did not exercise write failure", err)
	}
	mustExec(t, db, "ALTER TABLE base_payout DROP CHECK base_test_recovery_write")
	due()
	offline, err := chainassets.New(chainassets.Config{Networks: []chainassets.Endpoint{{Network: chainassets.BaseSepolia, URL: "http://127.0.0.1:1"}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer offline.Close()
	policy := chainsend.BaseRecoveryConfig{Sender: c.Intent.Sender, RecoveryPolicy: chainsend.DefaultRecoveryPolicy()}
	shared := &svc.ServiceContext{Config: config.Config{BaseRecovery: &policy}, DB: db, ChainAssets: offline, BaseReceipts: chainreceipt.NewBase(sqlx.NewSqlConnFromDB(db), transaction.New(db), offline)}
	if err = shared.StartBackground(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() {
		shared.StopBackground()
		if err := shared.WaitBackground(ctx); err != nil {
			t.Error(err)
		}
	}()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		rows, err = r.List(ctx, chainsend.RecoveryConfirmed, 1)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) == 1 {
			break
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	row := rows[0]
	if row.Attempts != 2 || row.BroadcastAttempts != 3 || row.TransferLogIndex != c.Intent.TransferLogIndex || row.TxHash != c.Intent.TxHash || row.LeaseUntil.Valid || calls != 3 {
		t.Fatal(row, calls)
	}
	var count int
	if err = db.QueryRow("SELECT COUNT(*) FROM chain_transfer_event").Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
}

func TestBaseRecoveryMigration(t *testing.T) {
	ctx := context.Background()
	name, dsn, db, err := testRun.NewDatabase(ctx, "base_recovery_upgrade")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	m, err := migration.Open(dsn, name, "../../manifest/sql/diff/business", "business")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if err = m.Migrate(27); err != nil {
		t.Fatal(err)
	}
	usdc := loadBasePayout(t, "base-payout-usdc")
	restoreBasePayout(t, db, usdc.Intent, chainsend.BaseConfirmed)
	eth := loadBasePayout(t, "base-payout-eth")
	src := &baseReceiptSource{proof: eth.Evidence}
	r := baseScheduler(t, db, eth.Intent.Sender, noBaseBroadcast(t), src, 2)
	runtime := chainsend.NewRecoveryRuntime(r)
	if err = runtime.Start(ctx); err == nil || !strings.Contains(err.Error(), "schema") {
		t.Fatal("old schema accepted", err)
	}
	runtime.Stop()
	if err = runtime.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	unsigned := eth.Intent
	unsigned.RequestKey, unsigned.Nonce = "unsigned", "10"
	restoreBasePayout(t, db, unsigned, chainsend.Prepared)
	for _, version := range []uint{28, 27} {
		if err = m.Migrate(version); err != nil {
			t.Fatal(err)
		}
	}
	restoreBasePayout(t, db, eth.Intent, chainsend.Signed)
	if err = m.Migrate(28); err != nil {
		t.Fatal(err)
	}
	rows, err := r.List(ctx, chainsend.RecoveryConfirmed, 10)
	if err != nil || len(rows) != 1 || rows[0].TxHash != usdc.Intent.TxHash || rows[0].TransferLogIndex != usdc.Intent.TransferLogIndex || rows[0].Generation != 0 || !rows[0].CheckedAt.Valid {
		t.Fatal(rows, err)
	}
	if worked, err := r.RunOne(ctx); !worked || err != nil {
		t.Fatal("old signature not recovered", worked, err)
	}
	if worked, err := r.RunOne(ctx); worked || err != nil {
		t.Fatal("unsigned was claimed", worked, err)
	}
	if err = m.Migrate(27); err == nil {
		t.Fatal("downgrade discarded recovery history")
	}
	rows, err = r.List(ctx, "", 10)
	if err != nil || len(rows) != 2 {
		t.Fatal(rows, err)
	}
}
