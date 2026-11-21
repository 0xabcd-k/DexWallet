//go:build integration

package integration

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/config"
	"github.com/0xabcd-k/DexWallet/backend/internal/migration"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainreceipt"
	"github.com/0xabcd-k/DexWallet/backend/svc"
	"github.com/0xabcd-k/DexWallet/backend/transaction"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

type listenerPayment struct {
	Collection chainreceipt.ReceiveRequest
	Receipt    chainreceipt.Receipt
	Evidence   chainassets.TONEvidence
}

// Restore the payment terms issued before the two real payments. Only the
// previously issued request terms are fixtures; nothing seeds events, cursor positions or tx IDs.
func restoreTONRequests(t *testing.T, db *sql.DB, catalog []chainassets.Network) []listenerPayment {
	t.Helper()
	s := chainreceipt.NewAttribution(sqlx.NewSqlConnFromDB(db), catalog)
	var payments []listenerPayment
	for _, name := range []string{"native", "jetton"} {
		b, err := os.ReadFile("../../logic/chainreceipt/testdata/ton-collection-" + name + ".json")
		if err != nil {
			t.Fatal(err)
		}
		var p listenerPayment
		if err = json.Unmarshal(b, &p); err != nil {
			t.Fatal(err)
		}
		c := p.Collection
		ctx := context.Background()
		if err = s.Register(ctx, c.Network, c.Recipient, c.Route); err != nil {
			t.Fatal(err)
		}
		terms := c.ReceiveTerms
		terms.ExpiresAt = time.Now().Add(time.Hour).Unix()
		if _, err = s.Open(ctx, terms); err != nil {
			t.Fatal(err)
		}
		mustExec(t, db, "UPDATE chain_receive_request SET opens_at=?,expires_at=?,payment_reference=? WHERE request_key=?", c.OpensAt, c.ExpiresAt, c.Reference, c.Key)
		payments = append(payments, p)
	}
	return payments
}

func TestLiveTONReceiptListener(t *testing.T) {
	if os.Getenv("DEXWALLET_TEST_TON_LISTENER_LIVE") != "1" {
		t.Skip("opt-in TON Testnet read-only history")
	}
	db, _ := userDatabase(t, "live_ton_listen")
	cfg := chainassets.Config{Timeout: 20 * time.Second, Networks: []chainassets.Endpoint{{Network: chainassets.TONTestnet, URL: "https://testnet.toncenter.com", TestUSDTContract: "kQD0GKBM8ZbryVk2aESmzfU6b9b_8era_IkvBSELujFZPsyy"}}}
	cfg.LoadEnvironment()
	assets, err := chainassets.New(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer assets.Close()
	payments := restoreTONRequests(t, db, assets.Catalog())
	conn := sqlx.NewSqlConnFromDB(db)
	a := chainreceipt.NewAttribution(conn, assets.Catalog())
	c := chainreceipt.TONListenerConfig{PollInterval: 2 * time.Second, OperationTimeout: 25 * time.Second, RetryInterval: 35 * time.Second}
	shared := svc.ServiceContext{DB: db, ChainAssets: assets, Config: config.Config{TONReceiptListener: &c}, ReceiveAttribution: a, TONReceipts: chainreceipt.NewTON(conn, transaction.New(db), assets)}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if err = shared.StartBackground(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() {
		shared.StopBackground()
		drain, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if err := shared.WaitBackground(drain); err != nil {
			t.Error(err)
		}
	}()
	for _, p := range payments {
		for {
			var current string
			err = db.QueryRowContext(ctx, "SELECT status FROM ton_receive_candidate WHERE tx_hash=?", p.Collection.Event.Signature).Scan(&current)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				t.Fatal(err)
			}
			if current == "matched" {
				break
			}
			select {
			case <-ctx.Done():
				t.Fatal(p.Collection.Key, ctx.Err())
			case <-time.After(time.Second):
			}
		}
		got, err := a.Find(ctx, p.Collection.Key)
		if err != nil || got.Event == nil || *got.Event != *p.Collection.Event {
			t.Fatal(got, err)
		}
		t.Logf("%s %s %s %s", got.Key, got.Atoms, got.Event.Kind, got.Event.Signature)
	}
}

var errListenerOffline = errors.New("test RPC unavailable")

type historyFixtureSource struct {
	*tonReceiptSource
	history      map[string][]json.RawMessage
	proofs       map[string]chainassets.TONEvidence
	wallet       string
	beforePage   func(string)
	gap, offline bool
}

func (s *historyFixtureSource) ResolveTONJettonWallet(context.Context, string, string) (string, error) {
	return s.wallet, nil
}
func (s *historyFixtureSource) ReadTONEvidence(_ context.Context, _, hash, _, _ string) (chainassets.TONEvidence, error) {
	if !s.offline {
		if p, ok := s.proofs[hash]; ok {
			return p, nil
		}
	}
	return chainassets.TONEvidence{}, errListenerOffline
}
func (s *historyFixtureSource) ReadTONHistory(_ context.Context, account string, start int64, _ int) (json.RawMessage, error) {
	if s.beforePage != nil {
		s.beforePage(account)
	}
	var rows []json.RawMessage
	for _, item := range s.history[account] {
		var tx struct{ LT string }
		if err := json.Unmarshal(item, &tx); err != nil {
			return nil, err
		}
		lt, err := strconv.ParseInt(tx.LT, 10, 64)
		if err != nil {
			return nil, err
		}
		if lt >= start {
			rows = append(rows, item)
		}
	}
	if s.gap && len(rows) > 0 {
		rows = rows[1:]
	}
	if len(rows) > 2 {
		rows = rows[:2]
	}
	if rows == nil {
		rows = []json.RawMessage{}
	}
	return json.Marshal(map[string]any{"transactions": rows})
}
func TestTONReceiptListenerCatchup(t *testing.T) {
	db, _ := userDatabase(t, "ton_listener")
	ctx := context.Background()
	_, src := tonReceiptFixture(t, "ton-collection-jetton")
	payments := restoreTONRequests(t, db, src.catalog)
	var page map[string][]json.RawMessage
	b, err := os.ReadFile("../../logic/chainreceipt/testdata/ton-collection-history.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(b, &page); err != nil {
		t.Fatal(err)
	}
	source := &historyFixtureSource{tonReceiptSource: src, history: map[string][]json.RawMessage{}, proofs: map[string]chainassets.TONEvidence{}, wallet: payments[1].Receipt.Event.RecipientAccount}
	for i, p := range payments {
		asset := []string{"native", "jetton"}[i]
		source.history[p.Receipt.Event.RecipientAccount] = page[asset]
		source.proofs[p.Collection.Event.Signature] = p.Evidence
	}
	// The owner also received the one-nanoton Jetton notification. Its public
	// incoming message is enough to classify it before full receipt verification.
	notice := page["native"][len(page["native"])-1]
	var noticeID struct{ Hash string }
	if err = json.Unmarshal(notice, &noticeID); err != nil {
		t.Fatal(err)
	}
	hash, err := base64.StdEncoding.DecodeString(noticeID.Hash)
	if err != nil {
		t.Fatal(err)
	}
	event, err := json.Marshal(map[string]any{"transactions": []json.RawMessage{notice}})
	if err != nil {
		t.Fatal(err)
	}
	source.proofs[hex.EncodeToString(hash)] = chainassets.TONEvidence{Transaction: event}
	conn := sqlx.NewSqlConnFromDB(db)
	a := chainreceipt.NewAttribution(conn, src.catalog)
	receipts := chainreceipt.NewTON(conn, transaction.New(db), source)
	newListener := func() *chainreceipt.TONListener { return chainreceipt.NewTONListener(db, source, receipts, a) }
	l := newListener()
	count := func(query string) int64 {
		t.Helper()
		var n int64
		if err := db.QueryRow(query).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	scan := func() error {
		mustExec(t, db, "UPDATE ton_receive_cursor SET next_poll=TIMESTAMPADD(SECOND,-1,next_poll) WHERE next_poll>UTC_TIMESTAMP(6)")
		return l.Scan(ctx, time.Second)
	}
	source.gap = true
	if err = scan(); !errors.Is(err, chainreceipt.ErrInvalidEvidence) {
		t.Fatal("history gap accepted", err)
	}
	if count("SELECT COALESCE(SUM(last_lt),0) FROM ton_receive_cursor") != 0 {
		t.Fatal("cursor crossed a gap")
	}
	source.gap = false
	for i := 0; i < 12; i++ {
		if err = scan(); err != nil {
			t.Fatal(err)
		}
		if i == 2 {
			l = newListener()
		}
	}
	if n := count("SELECT COUNT(*) FROM ton_receive_candidate"); n != 3 {
		t.Fatalf("two payments and a notification expected, got %d", n)
	}
	source.offline = true
	if err = l.Process(ctx, time.Minute); err == nil {
		t.Fatal("RPC failure hidden")
	}
	// Other candidates remain eligible even while one is waiting to retry.
	if count("SELECT COUNT(*) FROM ton_receive_candidate WHERE attempts=0 AND next_try<=UTC_TIMESTAMP(6)") != 2 {
		t.Fatal("failed candidate blocked queue")
	}
	source.offline = false
	mustExec(t, db, "UPDATE ton_receive_candidate SET next_try=UTC_TIMESTAMP(6)")
	// Simulate a crash after a verified receipt committed and before its request was matched.
	r := payments[0].Receipt
	if _, err = receipts.Record(ctx, chainreceipt.TONRequest{Network: r.Transaction.Network, Hash: r.Transaction.Signature, AssetID: r.Event.AssetID, Recipient: r.Event.RecipientOwner, Atoms: r.Event.Atoms}); err != nil {
		t.Fatal(err)
	}
	delete(source.proofs, r.Transaction.Signature)
	l = newListener()
	for i := 0; i < 4; i++ {
		if err = l.Process(ctx, time.Minute); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range payments {
		got, err := a.Find(ctx, p.Collection.Key)
		if err != nil || got.Event == nil || *got.Event != *p.Collection.Event {
			t.Fatal(got, err)
		}
	}
	if count("SELECT COUNT(*) FROM ton_receive_candidate WHERE status='ignored'") != 1 || count("SELECT COUNT(*) FROM ton_receive_candidate WHERE status='matched'") != 2 || count("SELECT COUNT(*) FROM chain_transfer_event") != 2 {
		t.Fatal("incorrect event count")
	}
	// Both workers fetch the same next page. The second commits while the first
	// is paused; the stale worker must leave both the cursor and inbox intact.
	account := r.Event.RecipientAccount
	last := source.history[account][len(source.history[account])-1]
	var row map[string]any
	if err = json.Unmarshal(last, &row); err != nil {
		t.Fatal(err)
	}
	row["prev_trans_lt"], row["prev_trans_hash"] = row["lt"], row["hash"]
	row["lt"], row["hash"] = "111185131000100", strings.Repeat("ab", 32)
	next, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	source.history[account] = append(source.history[account], next)
	due := func() {
		mustExec(t, db, "UPDATE ton_receive_cursor SET next_poll=TIMESTAMPADD(HOUR,1,UTC_TIMESTAMP(6))")
		mustExec(t, db, "UPDATE ton_receive_cursor SET next_poll=UTC_TIMESTAMP(6) WHERE scan_account=?", account)
	}
	// A failing candidate insert must roll the cursor back as well.
	due()
	mustExec(t, db, "ALTER TABLE ton_receive_candidate ADD CONSTRAINT test_listener_rollback CHECK (tx_lt<>111185131000100)")
	if err = l.Scan(ctx, time.Second); err == nil || !strings.Contains(err.Error(), "test_listener_rollback") {
		t.Fatal(err)
	}
	if count("SELECT COUNT(*) FROM ton_receive_cursor WHERE last_lt=111185131000100") != 0 {
		t.Fatal("cursor committed without inbox")
	}
	mustExec(t, db, "ALTER TABLE ton_receive_candidate DROP CHECK test_listener_rollback")
	due()
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	source.beforePage = func(_ string) {
		if calls.Add(1) == 1 {
			close(entered)
			<-release
		}
	}
	first := make(chan error, 1)
	go func() { first <- l.Scan(ctx, time.Second) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("first worker did not fetch page")
	}
	due()
	secondErr := newListener().Scan(ctx, time.Second)
	close(release)
	if err = <-first; err != nil || secondErr != nil {
		t.Fatal(err, secondErr)
	}
	if count("SELECT COUNT(*) FROM ton_receive_candidate") != 4 || count("SELECT COUNT(*) FROM ton_receive_cursor WHERE last_lt=111185131000100") != 1 {
		t.Fatal("concurrent cursor or inbox incorrect")
	}
	for _, name := range []string{"ledger_voucher", "event_outbox", "user_account"} {
		if count("SELECT COUNT(*) FROM "+name) != 0 {
			t.Fatal(name)
		}
	}
}

func TestTONReceiptListenerMigration(t *testing.T) {
	ctx := context.Background()
	name, dsn, db, err := testRun.NewDatabase(ctx, "listener_migrate")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	m, err := migration.Open(dsn, name, "../../manifest/sql/diff/business", "business")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if err = m.Migrate(32); err != nil {
		t.Fatal(err)
	}
	_, source := tonReceiptFixture(t, "ton-collection-jetton")
	payments := restoreTONRequests(t, db, source.catalog)
	for _, v := range []uint{33, 32, 33} {
		if err = m.Migrate(v); err != nil {
			t.Fatal(v, err)
		}
	}
	conn := sqlx.NewSqlConnFromDB(db)
	a := chainreceipt.NewAttribution(conn, source.catalog)
	for _, p := range payments {
		got, e := a.Find(ctx, p.Collection.Key)
		if e != nil || got.Reference != p.Collection.Reference {
			t.Fatal(got, e)
		}
	}
	mustExec(t, db, "INSERT INTO ton_receive_cursor(address_id,asset_id,starts_at) SELECT address_id,asset_id,opens_at FROM chain_receive_request LIMIT 1")
	if err = m.Migrate(32); err == nil || !strings.Contains(err.Error(), "ton_receive_rollback_guard") {
		t.Fatal("lost listener state", err)
	}
	var n int
	if err = db.QueryRow("SELECT COUNT(*) FROM ton_receive_cursor").Scan(&n); err != nil || n != 1 {
		t.Fatal(n, err)
	}
}
