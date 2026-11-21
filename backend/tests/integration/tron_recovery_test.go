//go:build integration

package integration

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/config"
	"github.com/0xabcd-k/DexWallet/backend/internal/migration"
	"github.com/0xabcd-k/DexWallet/backend/internal/tronnative"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainreceipt"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainsend"
	model "github.com/0xabcd-k/DexWallet/backend/model/chainsend"
	"github.com/0xabcd-k/DexWallet/backend/svc"
	"github.com/0xabcd-k/DexWallet/backend/transaction"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/mr-tron/base58"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

type tronPayoutCapture struct {
	Intent   model.TronPayout
	Receipt  chainreceipt.Receipt
	Evidence chainassets.TRONEvidence
}

func loadTRONPayout(t *testing.T, name string) tronPayoutCapture {
	t.Helper()
	raw, err := os.ReadFile("../../logic/chainreceipt/testdata/tron-payout-" + name + ".json")
	if err != nil {
		t.Fatal(err)
	}
	var c tronPayoutCapture
	if err = json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	if c.Intent.AssetId == "" { // Native capture predates token support.
		c.Intent.AssetId = c.Receipt.Event.AssetID
	}
	return c
}

// Insert exactly the original authorization using the pre-recovery schema.
func restoreTRONPayout(t *testing.T, db *sql.DB, p model.TronPayout, state string) {
	t.Helper()
	if state == chainsend.Prepared {
		p.Signature = ""
	}
	if state != chainsend.TRONConfirmed {
		p.TransferLogIndex = sql.NullInt64{}
	}
	mustExec(t, db, `INSERT INTO tron_payout
		(request_key,network,sender,recipient,sun,asset_id,token_contract,token_atoms,fee_limit_sun,
		 ref_block_id,ref_height,timestamp_ms,expiration_ms,raw_hex,tx_id,signature,state,transfer_log_index)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, p.RequestKey, p.Network, p.Sender, p.Recipient, p.Sun,
		p.AssetId, p.TokenContract, p.TokenAtoms, p.FeeLimitSun, p.RefBlockId, p.RefHeight, p.TimestampMs, p.ExpirationMs, p.RawHex, p.TxId, p.Signature, state, p.TransferLogIndex)
}

func tronScheduler(t *testing.T, db *sql.DB, sender string, node chainsend.TRONNode, source chainreceipt.TRONSource, attempts int64) *chainsend.TRONRecovery {
	t.Helper()
	c := chainsend.TRONRecoveryConfig{Sender: sender, RecoveryPolicy: chainsend.DefaultRecoveryPolicy()}
	c.MaxAttempts = attempts
	r, err := chainsend.NewTRONRecovery(db, node, chainreceipt.NewTRON(sqlx.NewSqlConnFromDB(db), transaction.New(db), source), c)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestTRONRecoveryHistorical(t *testing.T) { checkTRONRecoveryHistory(t, false) }
func TestLiveTRONRecoveryHistorical(t *testing.T) {
	if os.Getenv("DEXWALLET_TEST_TRON_RECOVERY_LIVE") != "1" {
		t.Skip("explicit Nile historical RPC opt-in required")
	}
	checkTRONRecoveryHistory(t, true)
}
func checkTRONRecoveryHistory(t *testing.T, live bool) {
	for _, name := range []string{"trx", "usdt", "usdt-fixture"} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			db, _ := userDatabase(t, "tron_hist_"+strings.ReplaceAll(name, "-", "_"))
			c := loadTRONPayout(t, name)
			state := chainsend.Signed
			if name == "usdt" {
				state = chainsend.Submitted
			}
			restoreTRONPayout(t, db, c.Intent, state)
			var source chainreceipt.TRONSource = &tronReceiptSource{proof: c.Evidence}
			if live {
				url := os.Getenv("DEXWALLET_CHAIN_TRON_NILE_RPC_URL")
				if url == "" {
					t.Fatal("set the Nile RPC endpoint for live verification")
				}
				real, err := chainassets.New(chainassets.Config{Timeout: 30 * time.Second, Networks: []chainassets.Endpoint{{Network: chainassets.TRONNile, URL: url}}}, nil)
				if err != nil {
					t.Fatal(err)
				}
				defer real.Close()
				source = real
			}
			// Only the read-only receipt source is live. The send node has no RPC endpoint.
			node := &tronSendNode{submit: func(tronnative.Signed) error { t.Fatal("historical broadcast"); return nil }}
			r := tronScheduler(t, db, c.Intent.Sender, node, source, 2)
			if worked, err := r.RunOne(ctx); !worked || err != nil {
				t.Fatal(worked, err)
			}
			rows, err := r.List(ctx, chainsend.RecoveryConfirmed, 1)
			if err != nil || len(rows) != 1 {
				t.Fatal(rows, err)
			}
			v := rows[0]
			if v.TxID != c.Intent.TxId || v.Atoms != c.Receipt.Event.Atoms || v.AssetID != c.Intent.AssetId || v.TransferLogIndex != c.Intent.TransferLogIndex || v.ExpirationMs != c.Intent.ExpirationMs || v.BroadcastAttempts != 0 || v.Attempts != 1 || v.Generation != 1 || !v.CheckedAt.Valid || v.LeaseUntil.Valid {
				t.Fatal(v)
			}
			receipts := chainreceipt.NewTRON(sqlx.NewSqlConnFromDB(db), transaction.New(db), source)
			got, err := receipts.Find(ctx, chainassets.TRONNile, v.TxID, c.Receipt.Event.Kind, c.Receipt.Event.Instruction)
			if err != nil || got.Transaction != c.Receipt.Transaction || got.Event != c.Receipt.Event {
				t.Fatal(got, err)
			}
			if worked, err := r.RunOne(ctx); worked || err != nil {
				t.Fatal("confirmed payout reclaimed", worked, err)
			}
			t.Logf("txID=%s fee=%s sun attempts=%d broadcasts=%d", v.TxID, got.Transaction.FeeAtoms, v.Attempts, v.BroadcastAttempts)
		})
	}
}

func TestTRONRecoveryExpiredBudgetAndOfflineRestart(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	db, _ := userDatabase(t, "tron_expired_restart")
	c := loadTRONPayout(t, "usdt-fixture")
	p := c.Intent
	restoreTRONPayout(t, db, p, chainsend.Signed)
	src := &tronReceiptSource{proof: chainassets.TRONEvidence{Genesis: c.Evidence.Genesis, Info: json.RawMessage("{}"), Transaction: json.RawMessage("{}")}}
	r := tronScheduler(t, db, p.Sender, &tronSendNode{}, src, 2)
	if _, err := r.RunOne(ctx); !errors.Is(err, chainsend.ErrTRONExpired) || errors.Is(err, chainsend.ErrRecoveryBudget) {
		t.Fatal(err)
	}
	rows, err := r.List(ctx, chainsend.RecoveryReady, 1)
	if err != nil || len(rows) != 1 || rows[0].BroadcastAttempts != 0 {
		t.Fatal(rows, err)
	}
	due := func() {
		mustExec(t, db, "UPDATE tron_payout SET recovery_next_at=UTC_TIMESTAMP(6) WHERE request_key=?", p.RequestKey)
	}
	due()
	if _, err := r.RunOne(ctx); !errors.Is(err, chainsend.ErrTRONExpired) || !errors.Is(err, chainsend.ErrRecoveryBudget) {
		t.Fatal(err)
	}
	rows, err = r.List(ctx, chainsend.RecoveryReview, 1)
	if err != nil || len(rows) != 1 || rows[0].TxID != p.TxId || rows[0].Attempts != 2 || rows[0].BroadcastAttempts != 0 {
		t.Fatal(rows, err)
	}
	if worked, err := r.RunOne(ctx); worked || err != nil {
		t.Fatal("review record reclaimed", worked, err)
	}
	if err = r.Retry(ctx, p.RequestKey, "ops", "late solid receipt"); err != nil {
		t.Fatal(err)
	}
	src.proof = c.Evidence
	mustExec(t, db, "ALTER TABLE tron_payout ADD CONSTRAINT tron_recovery_pause CHECK(state<>'confirmed')")
	if _, err = r.RunOne(ctx); err == nil || !strings.Contains(err.Error(), "tron_recovery_pause") {
		t.Fatal("missing confirmation failure", err)
	}
	mustExec(t, db, "ALTER TABLE tron_payout DROP CHECK tron_recovery_pause")
	due()
	offline, err := chainassets.New(chainassets.Config{Networks: []chainassets.Endpoint{{Network: chainassets.TRONNile, URL: "http://127.0.0.1:1"}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer offline.Close()
	policy := chainsend.TRONRecoveryConfig{Sender: p.Sender, RecoveryPolicy: chainsend.DefaultRecoveryPolicy()}
	shared := &svc.ServiceContext{DB: db, Config: config.Config{TRONRecovery: &policy}, ChainAssets: offline, TRONReceipts: chainreceipt.NewTRON(sqlx.NewSqlConnFromDB(db), transaction.New(db), offline)}
	if err = shared.StartBackground(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() {
		shared.StopBackground()
		if err := shared.WaitBackground(ctx); err != nil {
			t.Error(err)
		}
	}()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		rows, err = r.List(ctx, chainsend.RecoveryConfirmed, 1)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) == 1 {
			break
		}
		select {
		case <-tick.C:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	v := rows[0]
	if v.TxID != p.TxId || v.TransferLogIndex != p.TransferLogIndex || v.LeaseUntil.Valid || v.Attempts != 2 || v.BroadcastAttempts != 0 || !strings.Contains(v.RetryNote, "late solid receipt") {
		t.Fatal(v)
	}
	for table, want := range map[string]int{"chain_observed_transaction": 1, "chain_transfer_event": 1, "event_outbox": 0, "ledger_voucher": 0} {
		var n int
		if err = db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil || n != want {
			t.Fatal(table, n, err)
		}
	}
}

type delayedTRONSource struct {
	*tronReceiptSource
	entered, release chan struct{}
}

func (s *delayedTRONSource) ReadTRONEvidence(ctx context.Context, network, hash string) (chainassets.TRONEvidence, error) {
	close(s.entered)
	select {
	case <-ctx.Done():
		return chainassets.TRONEvidence{}, ctx.Err()
	case <-s.release:
		return s.tronReceiptSource.ReadTRONEvidence(ctx, network, hash)
	}
}

func TestTRONRecoveryTakeoverAndLostAck(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	db, _ := userDatabase(t, "tron_takeover")
	c := loadTRONPayout(t, "usdt")
	src := &tronReceiptSource{proof: chainassets.TRONEvidence{Genesis: c.Evidence.Genesis, Transaction: json.RawMessage("{}"), Info: json.RawMessage("{}")}}
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	address := append([]byte{0x41}, crypto.PubkeyToAddress(key.PublicKey).Bytes()...)
	first := sha256.Sum256(address)
	sum := sha256.Sum256(first[:])
	sender := base58.Encode(append(address, sum[:4]...))
	node := &tronUSDTNode{tronSendNode: &tronSendNode{head: tronnative.Head{ID: c.Intent.RefBlockId, Height: c.Intent.RefHeight, Timestamp: time.Now().UnixMilli()}}, contract: c.Intent.TokenContract}
	conn := sqlx.NewSqlConnFromDB(db)
	s, err := chainsend.NewTRON(conn, sender, node, chainreceipt.NewTRON(conn, transaction.New(db), src))
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.PrepareUSDT(ctx, "recover-original", c.Intent.Recipient, node.contract, c.Intent.TokenAtoms, c.Intent.FeeLimitSun)
	if err != nil {
		t.Fatal(err)
	}
	hash, _ := hex.DecodeString(p.TxId)
	sig, err := crypto.Sign(hash, key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Bind(ctx, p.RequestKey, hex.EncodeToString(sig)); err != nil {
		t.Fatal(err)
	}
	var wires []string
	node.submitToken = func(s tronnative.Signed) error {
		if s.ID() != p.TxId || s.TokenContract() != p.TokenContract {
			t.Fatal("different authorization on replay")
		}
		wires = append(wires, s.Hex())
		if len(wires) == 1 {
			return io.ErrUnexpectedEOF
		}
		return nil
	}
	delayed := &delayedTRONSource{src, make(chan struct{}), make(chan struct{})}
	late := tronScheduler(t, db, sender, node, delayed, 5)
	done := make(chan error, 1)
	go func() { _, err := late.RunOne(ctx); done <- err }()
	select {
	case <-delayed.entered:
	case err := <-done:
		t.Fatal(err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	r := tronScheduler(t, db, sender, node, src, 5)
	if worked, err := r.RunOne(ctx); worked || err != nil {
		t.Fatal("active lease claimed", worked, err)
	}
	if _, err = s.Step(ctx, p.RequestKey); err == nil || len(wires) != 0 {
		t.Fatal("manual step bypassed active lease", err)
	}
	mustExec(t, db, "UPDATE tron_payout SET recovery_lease_until=TIMESTAMPADD(SECOND,-1,UTC_TIMESTAMP(6))")
	// The taking-over node blocks the old token configuration. No new sender authorization is implied.
	otherConfig := &tronUSDTNode{tronSendNode: node.tronSendNode}
	if _, err := tronScheduler(t, db, sender, otherConfig, src, 5).RunOne(ctx); !errors.Is(err, chainsend.ErrInvalidRequest) {
		t.Fatal(err)
	}
	close(delayed.release)
	if err = <-done; !errors.Is(err, chainsend.ErrRecoveryLeaseLost) || len(wires) != 0 {
		t.Fatal("late worker sent or overwrote review", err)
	}
	if _, err = s.Step(ctx, p.RequestKey); err == nil || len(wires) != 0 {
		t.Fatal("manual step bypassed review", err)
	}
	other := tronScheduler(t, db, c.Intent.Recipient, node, src, 5)
	if rows, err := other.List(ctx, "", 5); err != nil || len(rows) != 0 {
		t.Fatal("listed another sender's payout", rows, err)
	}
	if err = other.Retry(ctx, p.RequestKey, "ops", "wrong sender"); !errors.Is(err, chainsend.ErrRecoveryRetryDenied) {
		t.Fatal(err)
	}
	if err = r.Retry(ctx, p.RequestKey, "ops", "verified original token"); err != nil {
		t.Fatal(err)
	}
	if _, err = r.RunOne(ctx); !errors.Is(err, chainsend.ErrOutcomeUnknown) {
		t.Fatal(err)
	}
	due := func() { mustExec(t, db, "UPDATE tron_payout SET recovery_next_at=UTC_TIMESTAMP(6)") }
	due()
	node.seen = true
	if _, err = r.RunOne(ctx); !errors.Is(err, chainreceipt.ErrUnconfirmed) || len(wires) != 1 {
		t.Fatal("pending transaction replayed", err)
	}
	due()
	node.seen = false
	if _, err = r.RunOne(ctx); !errors.Is(err, chainreceipt.ErrUnconfirmed) || len(wires) != 2 || wires[0] != wires[1] {
		t.Fatal("lost ack changed the signed transaction", err)
	}
	due()
	if _, err = r.RunOne(ctx); !errors.Is(err, chainreceipt.ErrUnconfirmed) || len(wires) != 2 {
		t.Fatal("acknowledged transaction replayed", err)
	}
	rows, err := r.List(ctx, chainsend.RecoveryReady, 1)
	if err != nil || len(rows) != 1 || rows[0].BroadcastAttempts != 2 || rows[0].Attempts != 4 || rows[0].SendState != chainsend.Submitted {
		t.Fatal(rows, err)
	}
}

func TestTRONRecoveryMigration(t *testing.T) {
	ctx := context.Background()
	name, dsn, db, err := testRun.NewDatabase(ctx, "tron_recovery_upgrade")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	m, err := migration.Open(dsn, name, "../../manifest/sql/diff/business", "business")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if err = m.Migrate(30); err != nil {
		t.Fatal(err)
	}
	token := loadTRONPayout(t, "usdt")
	native := loadTRONPayout(t, "trx")
	fixture := loadTRONPayout(t, "usdt-fixture")
	restoreTRONPayout(t, db, token.Intent, chainsend.TRONConfirmed)
	restoreTRONPayout(t, db, fixture.Intent, chainsend.Prepared)
	r := tronScheduler(t, db, native.Intent.Sender, &tronSendNode{}, &tronReceiptSource{proof: native.Evidence}, 3)
	rt := chainsend.NewRecoveryRuntime(r)
	if err = rt.Start(ctx); err == nil || !strings.Contains(err.Error(), "schema") {
		t.Fatal("old schema accepted", err)
	}
	rt.Stop()
	if err = rt.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	for _, v := range []uint{31, 30} {
		if err = m.Migrate(v); err != nil {
			t.Fatal(err)
		}
	}
	restoreTRONPayout(t, db, native.Intent, chainsend.Signed)
	if err = m.Migrate(31); err != nil {
		t.Fatal(err)
	}
	rows, err := r.List(ctx, chainsend.RecoveryConfirmed, 10)
	if err != nil || len(rows) != 1 || rows[0].TxID != token.Intent.TxId || rows[0].Generation != 0 || !rows[0].CheckedAt.Valid || rows[0].TransferLogIndex != token.Intent.TransferLogIndex {
		t.Fatal(rows, err)
	}
	if worked, err := r.RunOne(ctx); !worked || err != nil {
		t.Fatal(worked, err)
	}
	if worked, err := r.RunOne(ctx); worked || err != nil {
		t.Fatal("claimed unsigned or historical confirmed record", worked, err)
	}
	if err = m.Migrate(30); err == nil {
		t.Fatal("downgrade discarded recovery history")
	}
	rows, err = r.List(ctx, chainsend.RecoveryConfirmed, 10)
	if err != nil || len(rows) != 2 {
		t.Fatal("history after refused downgrade", rows, err)
	}
}
