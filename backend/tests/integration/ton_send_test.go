//go:build integration

package integration

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/internal/migration"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainsend"
	"github.com/0xabcd-k/DexWallet/backend/transaction"
	"github.com/xssnick/tonutils-go/ton/wallet"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

type tonSendSigner func(context.Context, chainsend.SigningRequest) ([]byte, error)

func (f tonSendSigner) SignTON(ctx context.Context, r chainsend.SigningRequest) ([]byte, error) {
	return f(ctx, r)
}

type tonSendBroadcast func(context.Context, string, []byte) error

func (f tonSendBroadcast) SubmitTON(ctx context.Context, n string, b []byte) error {
	return f(ctx, n, b)
}
func tonSendKey(t *testing.T) (chainsend.Wallet, tonSendSigner) {
	t.Helper()
	pub, key, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	return chainsend.Wallet{PublicKey: pub, SubwalletID: wallet.DefaultSubwallet}, func(_ context.Context, r chainsend.SigningRequest) ([]byte, error) {
		c, err := cell.FromBOC(r.PayloadBOC)
		if err != nil {
			return nil, err
		}
		if hex.EncodeToString(c.Hash()) != r.PayloadHash {
			return nil, chainsend.ErrConflict
		}
		return ed25519.Sign(key, c.Hash()), nil
	}
}
func tonSender(t *testing.T, db *sql.DB, tx chainsend.Transactor, w chainsend.Wallet, s chainsend.Signer, b chainsend.Broadcaster) *chainsend.TON {
	t.Helper()
	x, err := chainsend.NewTON(chainsend.NewStore(sqlx.NewSqlConnFromDB(db), tx), w, s, b)
	if err != nil {
		t.Fatal(err)
	}
	return x
}
func tonProposal(key string, seq uint32) chainsend.Proposal {
	return chainsend.Proposal{Key: key, Recipient: "0:" + strings.Repeat("ab", 32), Atoms: "10000000", Seqno: seq, ValidUntil: uint32(time.Now().Add(time.Hour).Unix())}
}
func tonNoBroadcast(context.Context, string, []byte) error { return errors.New("unexpected broadcast") }
func tonFind(t *testing.T, db *sql.DB, key string) chainsend.View {
	t.Helper()
	v, err := chainsend.NewStore(sqlx.NewSqlConnFromDB(db), transaction.New(db)).Find(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func TestTONSendSchemaReservationAndMigration(t *testing.T) {
	ctx := context.Background()
	name, dsn, db, err := testRun.NewDatabase(ctx, "ton_send_schema")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	m, err := migration.Open(dsn, name, "../../manifest/sql/diff/business", "business")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	for _, v := range []uint{19, 20, 19, 20, 21, 20, 21} {
		if err = m.Migrate(v); err != nil {
			t.Fatal(err)
		}
	}
	_, _, full, err := testRun.NewDatabase(ctx, "ton_send_ddl")
	if err != nil {
		t.Fatal(err)
	}
	defer full.Close()
	for _, file := range []string{"tables.sql", "constraints.sql"} {
		b, err := os.ReadFile(filepath.Join("../../manifest/sql/ddl/chainsend", file))
		if err != nil {
			t.Fatal(err)
		}
		mustExec(t, full, string(b))
	}
	var table, a, b string
	if err = db.QueryRow("SHOW CREATE TABLE chain_send_intent").Scan(&table, &a); err != nil {
		t.Fatal(err)
	}
	if err = full.QueryRow("SHOW CREATE TABLE chain_send_intent").Scan(&table, &b); err != nil || strings.ReplaceAll(a, "_ascii'", "_utf8mb4'") != strings.ReplaceAll(b, "_ascii'", "_utf8mb4'") {
		t.Fatal("DDL/migration differ", err, a, b)
	}
	w, signer := tonSendKey(t)
	sender := tonSender(t, db, transaction.New(db), w, signer, tonSendBroadcast(tonNoBroadcast))
	p := tonProposal("same-order", 7)
	var wg sync.WaitGroup
	failures := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := sender.Prepare(ctx, p); failures <- err }()
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Error(err)
		}
	}
	var count int
	if err = db.QueryRow("SELECT COUNT(*) FROM chain_send_intent").Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	for _, edit := range []func(*chainsend.Proposal){func(p *chainsend.Proposal) { p.Atoms = "2" }, func(p *chainsend.Proposal) { p.Recipient = "0:" + strings.Repeat("cd", 32) }, func(p *chainsend.Proposal) { p.Seqno++ }, func(p *chainsend.Proposal) { p.ValidUntil++ }} {
		bad := p
		edit(&bad)
		if _, err = sender.Prepare(ctx, bad); !errors.Is(err, chainsend.ErrConflict) {
			t.Fatal(err)
		}
	}
	other := p
	other.Key = "same-seqno"
	if _, err = sender.Prepare(ctx, other); !errors.Is(err, chainsend.ErrSeqnoReserved) {
		t.Fatal(err)
	}
	expired := tonProposal("expired", 8)
	expired.ValidUntil = 1
	if _, err = sender.Prepare(ctx, expired); !errors.Is(err, chainsend.ErrExpired) {
		t.Fatal(err)
	}
	// Native intents survive the optional Jetton column round trip.
	for _, v := range []uint{20, 21} {
		if err = m.Migrate(v); err != nil {
			t.Fatal(err)
		}
	}
	if got := tonFind(t, db, p.Key); got.Proposal != p {
		t.Fatal("native intent changed", got)
	}
	// Expired intent cannot be silently deleted by a rollback, even before signing.
	if err = m.Migrate(19); err == nil {
		t.Fatal("rollback erased a reserved intent")
	}
	if err = db.QueryRow("SELECT COUNT(*) FROM chain_send_intent").Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
}
func TestTONSendTimeoutRestartKeepsExactMessage(t *testing.T) {
	ctx := context.Background()
	db, _ := userDatabase(t, "ton_send_recovery")
	runner := transaction.New(db)
	w, sign := tonSendKey(t)
	var signs, broadcasts atomic.Int64
	var first []byte
	signer := tonSendSigner(func(ctx context.Context, r chainsend.SigningRequest) ([]byte, error) {
		signs.Add(1)
		v := tonFind(t, db, r.Key)
		if v.State != chainsend.Prepared {
			t.Error("signing before prepared commit", v)
		}
		return sign(ctx, r)
	})
	p := tonProposal("restart", 7)
	broadcaster := tonSendBroadcast(func(_ context.Context, n string, raw []byte) error {
		broadcasts.Add(1)
		v := tonFind(t, db, p.Key)
		if n != chainassets.TONTestnet || v.State != chainsend.BroadcastUnknown || v.MessageHash == "" || v.Attempts != 1 {
			t.Error("broadcast before durable marker", v)
		}
		var persisted string
		if err := db.QueryRow("SELECT signed_boc FROM chain_send_intent WHERE request_key=?", p.Key).Scan(&persisted); err != nil {
			t.Error(err)
		}
		if base64.StdEncoding.EncodeToString(raw) != persisted {
			t.Error("broadcast differs from durable bytes")
		}
		first = append([]byte(nil), raw...)
		return io.ErrUnexpectedEOF
	})
	sender := tonSender(t, db, runner, w, signer, broadcaster)
	if _, err := sender.Prepare(ctx, p); err != nil {
		t.Fatal(err)
	}
	if _, err := sender.Execute(ctx, p.Key); !errors.Is(err, chainsend.ErrOutcomeUnknown) {
		t.Fatal(err)
	}
	before := tonFind(t, db, p.Key)
	if before.State != chainsend.BroadcastUnknown || !strings.Contains(before.LastError, "EOF") {
		t.Fatal(before)
	}
	recovered := tonSender(t, db, runner, w, signer, tonSendBroadcast(func(_ context.Context, _ string, raw []byte) error {
		broadcasts.Add(1)
		if !bytes.Equal(raw, first) {
			t.Error("restart replaced message")
		}
		return nil
	}))
	after, err := recovered.Execute(ctx, p.Key)
	if err != nil || after.State != chainsend.Submitted || after.Attempts != 2 || after.MessageHash != before.MessageHash || after.BodyHash != before.BodyHash || after.LastError != "" {
		t.Fatal(after, err)
	}
	if _, err = recovered.Execute(ctx, p.Key); err != nil {
		t.Fatal(err)
	}
	if signs.Load() != 1 || broadcasts.Load() != 2 {
		t.Fatal(signs.Load(), broadcasts.Load())
	}
	// Node acceptance alone must never create a balance change or a verified receipt.
	for _, table := range []string{"user_account", "ledger_voucher", "ledger_entry", "account_hold", "transfer_order", "event_outbox", "chain_observed_transaction", "chain_transfer_event"} {
		var count int
		if err = db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil || count != 0 {
			t.Fatal(table, count, err)
		}
	}
}

type tonSendCommitFailure struct {
	base          *transaction.Runner
	failAt, calls int
	persist       bool
}

func (x *tonSendCommitFailure) TransactCtx(ctx context.Context, fn func(context.Context, sqlx.Session) error) error {
	x.calls++
	if x.calls != x.failAt {
		return x.base.TransactCtx(ctx, fn)
	}
	if x.persist {
		if err := x.base.TransactCtx(ctx, fn); err != nil {
			return err
		}
	}
	return &transaction.Failure{Phase: "commit", Cause: io.ErrUnexpectedEOF}
}
func TestTONSendCommitUncertaintyAtEachBoundary(t *testing.T) {
	ctx := context.Background()
	db, _ := userDatabase(t, "ton_send_commit")
	runner := transaction.New(db)
	for _, test := range []struct {
		name       string
		stage      int
		persist    bool
		state      string
		firstCalls int
		firstOK    bool
	}{
		{"signed-not-stored", 1, false, chainsend.Prepared, 0, false},
		{"signed-commit-lost", 1, true, chainsend.Submitted, 1, true},
		{"marker-not-stored", 2, false, chainsend.Signed, 0, false},
		{"marker-commit-lost", 2, true, chainsend.BroadcastUnknown, 0, false},
		{"ack-not-stored", 3, false, chainsend.BroadcastUnknown, 1, false},
		{"ack-commit-lost", 3, true, chainsend.Submitted, 1, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			w, signer := tonSendKey(t)
			p := tonProposal(test.name, 7)
			var calls int
			var original []byte
			broadcast := tonSendBroadcast(func(_ context.Context, _ string, b []byte) error {
				calls++
				if original == nil {
					original = append([]byte(nil), b...)
				} else if !bytes.Equal(original, b) {
					t.Error("replaced artifact")
				}
				return nil
			})
			sender := tonSender(t, db, runner, w, signer, broadcast)
			if _, err := sender.Prepare(ctx, p); err != nil {
				t.Fatal(err)
			}
			faulty := tonSender(t, db, &tonSendCommitFailure{base: runner, failAt: test.stage, persist: test.persist}, w, signer, broadcast)
			_, err := faulty.Execute(ctx, p.Key)
			if test.firstOK && err != nil || !test.firstOK && !errors.Is(err, chainsend.ErrOutcomeUnknown) {
				t.Fatal(err)
			}
			if v := tonFind(t, db, p.Key); v.State != test.state || calls != test.firstCalls {
				t.Fatal(v, calls)
			}
			v, err := sender.Execute(ctx, p.Key)
			if err != nil || v.State != chainsend.Submitted {
				t.Fatal(v, err)
			}
			expected := test.firstCalls
			if test.state != chainsend.Submitted {
				expected++
			}
			if calls != expected {
				t.Fatal(calls, expected)
			}
		})
	}
	for _, persist := range []bool{false, true} {
		t.Run("prepare-persist-"+map[bool]string{false: "false", true: "true"}[persist], func(t *testing.T) {
			w, sig := tonSendKey(t)
			sender := tonSender(t, db, &tonSendCommitFailure{base: runner, failAt: 1, persist: persist}, w, sig, tonSendBroadcast(tonNoBroadcast))
			p := tonProposal("prepare-"+hex.EncodeToString(w.PublicKey[:4]), 1)
			v, err := sender.Prepare(ctx, p)
			if persist {
				if err != nil || v.State != chainsend.Prepared {
					t.Fatal(v, err)
				}
			} else if !errors.Is(err, chainsend.ErrOutcomeUnknown) {
				t.Fatal(err)
			}
		})
	}
}
func TestTONSendRejectsWrongSignerAndLateFailureDoesNotDowngrade(t *testing.T) {
	ctx := context.Background()
	db, _ := userDatabase(t, "ton_send_concurrent")
	runner := transaction.New(db)
	w, signer := tonSendKey(t)
	_, wrongSigner := tonSendKey(t)
	var broadcasts atomic.Int64
	firstEntered, releaseFirst := make(chan struct{}), make(chan struct{})
	broadcaster := tonSendBroadcast(func(_ context.Context, _ string, _ []byte) error {
		if broadcasts.Add(1) == 1 {
			close(firstEntered)
			<-releaseFirst
			return io.ErrUnexpectedEOF
		}
		return nil
	})
	sender := tonSender(t, db, runner, w, wrongSigner, broadcaster)
	p := tonProposal("parallel", 9)
	if _, err := sender.Prepare(ctx, p); err != nil {
		t.Fatal(err)
	}
	if _, err := sender.Execute(ctx, p.Key); !errors.Is(err, chainsend.ErrInvalidSignature) {
		t.Fatal(err)
	}
	if broadcasts.Load() != 0 || tonFind(t, db, p.Key).State != chainsend.Prepared {
		t.Fatal("wrong signer reached broadcast")
	}
	sender = tonSender(t, db, runner, w, signer, broadcaster)
	done := make(chan error, 1)
	go func() { _, err := sender.Execute(ctx, p.Key); done <- err }()
	select {
	case <-firstEntered:
	case err := <-done:
		t.Fatal(err)
	case <-time.After(10 * time.Second):
		t.Fatal("first broadcast did not start")
	}
	v, err := sender.Execute(ctx, p.Key)
	close(releaseFirst)
	if err != nil || v.State != chainsend.Submitted {
		t.Fatal(v, err)
	}
	if err = <-done; !errors.Is(err, chainsend.ErrOutcomeUnknown) {
		t.Fatal(err)
	}
	v = tonFind(t, db, p.Key)
	if v.State != chainsend.Submitted || v.LastError != "" || v.Attempts != 2 {
		t.Fatal("late error downgraded accepted message", v)
	}
}

// Advance only the DB session clock, so expiry between signing and broadcast is
// deterministic without sleeping or changing the host clock. Reset before reuse.
type tonSendFutureClock struct {
	base *transaction.Runner
	now  uint32
}

func (x tonSendFutureClock) TransactCtx(ctx context.Context, fn func(context.Context, sqlx.Session) error) error {
	return x.base.TransactCtx(ctx, func(ctx context.Context, s sqlx.Session) error {
		if _, err := s.ExecCtx(ctx, "SET timestamp=?", x.now); err != nil {
			return err
		}
		result := fn(ctx, s)
		_, reset := s.ExecCtx(ctx, "SET timestamp=0")
		return errors.Join(result, reset)
	})
}
func TestTONSendExpiryAndStoredTamperingNeverRebroadcast(t *testing.T) {
	ctx := context.Background()
	db, _ := userDatabase(t, "ton_send_expiry")
	runner := transaction.New(db)
	w, sign := tonSendKey(t)
	var calls atomic.Int64
	b := tonSendBroadcast(func(context.Context, string, []byte) error { calls.Add(1); return io.ErrUnexpectedEOF })
	sender := tonSender(t, db, runner, w, sign, b)
	p := tonProposal("expires-before-broadcast", 7)
	if _, err := sender.Prepare(ctx, p); err != nil {
		t.Fatal(err)
	}
	expired := tonSender(t, db, tonSendFutureClock{runner, p.ValidUntil + 1}, w, sign, b)
	if _, err := expired.Execute(ctx, p.Key); !errors.Is(err, chainsend.ErrExpired) {
		t.Fatal(err)
	}
	v := tonFind(t, db, p.Key)
	if v.State != chainsend.Signed || v.Attempts != 0 || v.MessageHash == "" || calls.Load() != 0 {
		t.Fatal(v, calls.Load())
	}
	extended := p
	extended.ValidUntil++
	if _, err := sender.Prepare(ctx, extended); !errors.Is(err, chainsend.ErrConflict) {
		t.Fatal("renewed expired payment", err)
	}
	// Timeout occurs before expiry on another intent; after expiry it remains unknown.
	p = tonProposal("unknown-then-expired", 8)
	if _, err := sender.Prepare(ctx, p); err != nil {
		t.Fatal(err)
	}
	if _, err := sender.Execute(ctx, p.Key); !errors.Is(err, chainsend.ErrOutcomeUnknown) {
		t.Fatal(err)
	}
	before := tonFind(t, db, p.Key)
	expired = tonSender(t, db, tonSendFutureClock{runner, p.ValidUntil + 1}, w, sign, b)
	if _, err := expired.Execute(ctx, p.Key); !errors.Is(err, chainsend.ErrExpired) {
		t.Fatal(err)
	}
	v = tonFind(t, db, p.Key)
	if v.State != chainsend.BroadcastUnknown || v.Attempts != 1 || v.MessageHash != before.MessageHash || calls.Load() != 1 {
		t.Fatal(v, calls.Load())
	}
	// Restart validates persisted bytes instead of trusting the stored status/hash.
	mustExec(t, db, "UPDATE chain_send_intent SET signed_boc=? WHERE request_key=?", base64.StdEncoding.EncodeToString([]byte("corrupt artifact")), p.Key)
	if _, err := sender.Execute(ctx, p.Key); !errors.Is(err, chainsend.ErrConflict) {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatal("corrupted persisted message broadcast")
	}
}
