//go:build integration

package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	bg "github.com/0xabcd-k/DexWallet/backend/background"
	"github.com/0xabcd-k/DexWallet/backend/event"
	"github.com/0xabcd-k/DexWallet/backend/idcodec"
	"github.com/0xabcd-k/DexWallet/backend/internal/migration"
	"github.com/0xabcd-k/DexWallet/backend/internal/testenv"
	model "github.com/0xabcd-k/DexWallet/backend/model/background"
	"github.com/0xabcd-k/DexWallet/backend/transaction"
	rocketmq "github.com/apache/rocketmq-client-go/v2"
	"github.com/apache/rocketmq-client-go/v2/admin"
	"github.com/apache/rocketmq-client-go/v2/primitive"
	"github.com/apache/rocketmq-client-go/v2/producer"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

type bgFixture struct {
	db     *sql.DB
	store  *bg.Store
	codec  *idcodec.Codec
	config bg.Config
}

func newBackground(t *testing.T, label string) *bgFixture {
	t.Helper()
	db, _ := userDatabase(t, label)
	c := bg.Defaults()
	c.Publishers = 1
	c.Workers = 1
	c.PollInterval = 20 * time.Millisecond
	c.MaxAttempts = 2
	s, err := bg.NewStore(db, c)
	if err != nil {
		t.Fatal(err)
	}
	codec, err := idcodec.New("test-only-key123", idcodec.Event)
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, db, "CREATE TABLE background_effect (id BIGINT PRIMARY KEY, executions BIGINT NOT NULL)")
	mustExec(t, db, "INSERT INTO background_effect VALUES(1,0)")
	return &bgFixture{db, s, codec, c}
}
func addEvent(t *testing.T, f *bgFixture, business int64) int64 {
	t.Helper()
	err := transaction.New(f.db).TransactCtx(context.Background(), func(ctx context.Context, tx sqlx.Session) error {
		return event.Append(ctx, tx, event.TransferCompleted, business, event.TransferVersion, event.Transfer{SenderID: 1, RecipientID: 2, VoucherID: business, Amount: "10.00", Unit: "USD"})
	})
	if err != nil {
		t.Fatal(err)
	}
	var id int64
	if err = f.db.QueryRow("SELECT id FROM event_outbox WHERE business_id=?", business).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}
func effectHandler() bg.Handler {
	return bg.Handler{Name: "fixture_effect", Kind: event.TransferCompleted, Version: event.TransferVersion, Run: func(ctx context.Context, tx sqlx.Session, _ *model.EventOutbox) error {
		_, err := tx.ExecCtx(ctx, "UPDATE background_effect SET executions=executions+1 WHERE id=1")
		return err
	}}
}
func effectCount(t *testing.T, f *bgFixture, want int) {
	t.Helper()
	var n int
	if err := f.db.QueryRow("SELECT executions FROM background_effect WHERE id=1").Scan(&n); err != nil || n != want {
		t.Fatal("effects", n, want, err)
	}
}
func due(t *testing.T, f *bgFixture, q bg.Queue) {
	t.Helper()
	table := "event_outbox"
	if q == bg.Tasks {
		table = "background_task"
	}
	mustExec(t, f.db, "UPDATE "+table+" SET next_at='2000-01-01' WHERE state='ready'")
}
func expire(t *testing.T, f *bgFixture, q bg.Queue) {
	t.Helper()
	table := "event_outbox"
	if q == bg.Tasks {
		table = "background_task"
	}
	mustExec(t, f.db, "UPDATE "+table+" SET lease_until='2000-01-01' WHERE state='running'")
}
func claim(t *testing.T, f *bgFixture, q bg.Queue, handler string) *bg.Claim {
	t.Helper()
	c, err := f.store.Claim(context.Background(), q, handler)
	if err != nil || c == nil {
		t.Fatal("claim", c, err)
	}
	return c
}

type bgSender struct {
	bodies [][]byte
	err    error
	status primitive.SendStatus
}

func (s *bgSender) SendSync(_ context.Context, messages ...*primitive.Message) (*primitive.SendResult, error) {
	for _, m := range messages {
		s.bodies = append(s.bodies, append([]byte{}, m.Body...))
	}
	return &primitive.SendResult{Status: s.status}, s.err
}

func TestBackgroundTransferOutboxAtomicity(t *testing.T) {
	f := newLedger(t, "bg_transfer")
	if _, err := f.post(funding(1, 1, "100")); err != nil {
		t.Fatal(err)
	}
	service := transferService(f, f.tx)
	r := transferRequest("outbox_key_012345", 2, "10")
	order, err := service.Submit(context.Background(), transferActor(1), r)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.Submit(context.Background(), transferActor(1), r); err != nil {
		t.Fatal(err)
	}
	if ledgerCount(t, f.db, "event_outbox") != 1 {
		t.Fatal("duplicate event")
	}
	var payload string
	if err = f.db.QueryRow("SELECT payload FROM event_outbox WHERE kind=? AND business_id=? AND version=?", event.TransferCompleted, order.Id, event.TransferVersion).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	var snapshot event.Transfer
	if err = json.Unmarshal([]byte(payload), &snapshot); err != nil || snapshot.Amount != "10.00" || snapshot.VoucherID != order.VoucherId.Int64 || snapshot.RecipientID != 2 {
		t.Fatal(snapshot, err)
	}
	mustExec(t, f.db, fmt.Sprintf("ALTER TABLE event_outbox ADD CONSTRAINT ck_bg_fail CHECK(business_id=%d)", order.Id))
	r.Key = "outbox_key_987654"
	if _, err = service.Submit(context.Background(), transferActor(1), r); err == nil {
		t.Fatal("event failure committed funds")
	}
	ledgerBalance(t, f, 1, "90.00")
	ledgerBalance(t, f, 2, "10.00")
	assertTransferCount(t, f, 1)
	mustExec(t, f.db, "ALTER TABLE event_outbox DROP CHECK ck_bg_fail")
	if _, err = service.Submit(context.Background(), transferActor(1), r); err != nil {
		t.Fatal(err)
	}
	if ledgerCount(t, f.db, "event_outbox") != 2 {
		t.Fatal("missing event")
	}
}

func TestBackgroundPublishUncertainAndRepeatedReception(t *testing.T) {
	f := newBackground(t, "bg_publish")
	id := addEvent(t, f, 1)
	ctx := context.Background()
	sender := &bgSender{err: errors.New("broker acknowledgement lost")}
	if work, err := f.store.PublishOne(ctx, sender, f.codec, "isolated"); !work || err == nil {
		t.Fatal(work, err)
	}
	row, err := f.store.Event(ctx, id)
	if err != nil || row.State != model.Ready || row.Attempts != 1 || row.LastError == "" {
		t.Fatal(row, err)
	}
	// A timeout may still have delivered the first message. Accept it, then replay.
	h := effectHandler()
	for range 2 {
		if err = f.store.Receive(ctx, id, []bg.Handler{h}); err != nil {
			t.Fatal(err)
		}
	}
	if ledgerCount(t, f.db, "background_task") != 1 {
		t.Fatal("duplicate reception")
	}
	sender.err = nil
	due(t, f, bg.Events)
	if _, err = f.store.PublishOne(ctx, sender, f.codec, "isolated"); err != nil {
		t.Fatal(err)
	}
	if len(sender.bodies) != 2 || string(sender.bodies[0]) != string(sender.bodies[1]) {
		t.Fatal("retry changed event identity")
	}
	row, err = f.store.Event(ctx, id)
	if err != nil || row.State != model.Published {
		t.Fatal(row, err)
	}
	if err = f.store.Execute(ctx, *claim(t, f, bg.Tasks, h.Name), h); err != nil {
		t.Fatal(err)
	}
	if err = f.store.Receive(ctx, id, []bg.Handler{h}); err != nil {
		t.Fatal(err)
	}
	if c, err := f.store.Claim(ctx, bg.Tasks, h.Name); err != nil || c != nil {
		t.Fatal("replayed completed task", c, err)
	}
	effectCount(t, f, 1)
	// Explicit republish retains identity and successful task state.
	if err = f.store.Retry(ctx, bg.Events, id, "test-operator", "reconcile missing downstream acceptance"); err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.PublishOne(ctx, sender, f.codec, "isolated"); err != nil {
		t.Fatal(err)
	}
	if string(sender.bodies[2]) != string(sender.bodies[0]) {
		t.Fatal("manual replay changed identity")
	}
}

func TestBackgroundClaimConcurrencyExpiryAndFencing(t *testing.T) {
	f := newBackground(t, "bg_claims")
	ctx := context.Background()
	for i := int64(1); i <= 6; i++ {
		addEvent(t, f, i)
	}
	locker, err := f.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer locker.Rollback()
	var locked int64
	if err = locker.QueryRow("SELECT id FROM event_outbox ORDER BY id LIMIT 1 FOR UPDATE").Scan(&locked); err != nil {
		t.Fatal(err)
	}
	quick, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	c, err := f.store.Claim(quick, bg.Events, "")
	if err != nil || c == nil || c.ID == locked {
		t.Fatal("did not skip locked event", c, err)
	}
	if err = locker.Rollback(); err != nil {
		t.Fatal(err)
	}
	results := make([]*bg.Claim, 6)
	failures := make([]error, 6)
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func() { defer wg.Done(); results[i], failures[i] = f.store.Claim(ctx, bg.Events, "") }()
	}
	wg.Wait()
	ids := map[int64]bool{c.ID: true}
	for i, c := range results {
		if failures[i] != nil {
			t.Fatal(failures[i])
		}
		if c != nil {
			if ids[c.ID] {
				t.Fatal("duplicate active claim")
			}
			ids[c.ID] = true
		}
	}
	if len(ids) != 6 {
		t.Fatal(ids)
	}
	h := effectHandler()
	if err = f.store.Receive(ctx, locked, []bg.Handler{h}); err != nil {
		t.Fatal(err)
	}
	old := claim(t, f, bg.Tasks, h.Name)
	expire(t, f, bg.Tasks)
	fresh := claim(t, f, bg.Tasks, h.Name)
	if fresh.Generation <= old.Generation {
		t.Fatal("claim generation did not advance")
	}
	if err = f.store.Execute(ctx, *old, h); !errors.Is(err, bg.ErrLeaseLost) {
		t.Fatal(err)
	}
	effectCount(t, f, 0)
	if err = f.store.Execute(ctx, *fresh, h); err != nil {
		t.Fatal(err)
	}
	effectCount(t, f, 1)
	if err = f.store.Retry(ctx, bg.Tasks, fresh.ID, "operator", "repeat completed action"); !errors.Is(err, bg.ErrRetryDenied) {
		t.Fatal(err)
	}
}

func TestBackgroundTaskRollbackRetryDeadAndBlocked(t *testing.T) {
	f := newBackground(t, "bg_task_retry")
	id := addEvent(t, f, 1)
	ctx := context.Background()
	h := effectHandler()
	if err := f.store.Receive(ctx, id, []bg.Handler{h}); err != nil {
		t.Fatal(err)
	}
	run := h.Run
	sentinel := errors.New("dependency temporarily unavailable")
	h.Run = func(ctx context.Context, tx sqlx.Session, e *model.EventOutbox) error {
		if err := run(ctx, tx, e); err != nil {
			return err
		}
		return bg.Retryable(sentinel)
	}
	first := claim(t, f, bg.Tasks, h.Name)
	if err := f.store.Execute(ctx, *first, h); !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
	effectCount(t, f, 0)
	row, err := f.store.Task(ctx, first.ID)
	if err != nil || row.State != model.Ready || row.LastError == "" {
		t.Fatal(row, err)
	}
	due(t, f, bg.Tasks)
	if err = f.store.Execute(ctx, *claim(t, f, bg.Tasks, h.Name), h); !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
	effectCount(t, f, 0)
	row, err = f.store.Task(ctx, first.ID)
	if err != nil || row.State != model.Dead {
		t.Fatal(row, err)
	}
	if err = f.store.Retry(ctx, bg.Tasks, first.ID, "operator", "dependency recovered"); err != nil {
		t.Fatal(err)
	}
	next := claim(t, f, bg.Tasks, h.Name)
	if next.Generation <= row.Generation || next.Attempts != 1 {
		t.Fatal(next)
	}
	h.Run = run
	if err = f.store.Execute(ctx, *next, h); err != nil {
		t.Fatal(err)
	}
	effectCount(t, f, 1)
	// Unknown external facts are explicitly blocked, never retried as generic failures.
	id = addEvent(t, f, 2)
	if err = f.store.Receive(ctx, id, []bg.Handler{h}); err != nil {
		t.Fatal(err)
	}
	h.Run = func(context.Context, sqlx.Session, *model.EventOutbox) error {
		return bg.Uncertain(errors.New("business evidence unresolved"))
	}
	unknown := claim(t, f, bg.Tasks, h.Name)
	if err = f.store.Execute(ctx, *unknown, h); err == nil {
		t.Fatal("unknown succeeded")
	}
	row, err = f.store.Task(ctx, unknown.ID)
	if err != nil || row.State != model.Blocked {
		t.Fatal(row, err)
	}
	if err = f.store.Retry(ctx, bg.Tasks, unknown.ID, "operator", "blind retry"); !errors.Is(err, bg.ErrRetryDenied) {
		t.Fatal(err)
	}
	// A handler panic rolls back its effects and becomes a diagnosable dead task.
	id = addEvent(t, f, 3)
	if err = f.store.Receive(ctx, id, []bg.Handler{effectHandler()}); err != nil {
		t.Fatal(err)
	}
	h.Run = func(ctx context.Context, tx sqlx.Session, e *model.EventOutbox) error {
		if err := run(ctx, tx, e); err != nil {
			return err
		}
		panic("handler panic")
	}
	panicked := claim(t, f, bg.Tasks, h.Name)
	if err = f.store.Execute(ctx, *panicked, h); err == nil {
		t.Fatal("panic succeeded")
	}
	row, err = f.store.Task(ctx, panicked.ID)
	if err != nil || row.State != model.Dead {
		t.Fatal(row, err)
	}
	effectCount(t, f, 1)
}

func TestBackgroundAcceptanceAtomicAndLeaseAttemptLimit(t *testing.T) {
	f := newBackground(t, "bg_acceptance")
	id := addEvent(t, f, 1)
	ctx := context.Background()
	a, b := effectHandler(), effectHandler()
	a.Name = "first_handler"
	b.Name = "second_handler"
	mustExec(t, f.db, "ALTER TABLE background_task ADD CONSTRAINT ck_bg_accept CHECK(handler='first_handler')")
	if err := f.store.Receive(ctx, id, []bg.Handler{a, b}); err == nil {
		t.Fatal("partial acceptance")
	}
	if ledgerCount(t, f.db, "background_task") != 0 {
		t.Fatal("partial tasks survived")
	}
	mustExec(t, f.db, "ALTER TABLE background_task DROP CHECK ck_bg_accept")
	if err := f.store.Receive(ctx, id, []bg.Handler{a, b}); err != nil {
		t.Fatal(err)
	}
	if err := f.store.Receive(ctx, id, []bg.Handler{a, b}); err != nil {
		t.Fatal(err)
	}
	if ledgerCount(t, f.db, "background_task") != 2 {
		t.Fatal("fanout dedup failed")
	}
	b.Version++
	if err := f.store.Receive(ctx, id, []bg.Handler{b}); err == nil {
		t.Fatal("unsupported version accepted")
	}
	first := claim(t, f, bg.Tasks, a.Name)
	expire(t, f, bg.Tasks)
	claim(t, f, bg.Tasks, a.Name)
	expire(t, f, bg.Tasks)
	if c, err := f.store.Claim(ctx, bg.Tasks, a.Name); err != nil || c != nil {
		t.Fatal(c, err)
	}
	row, err := f.store.Task(ctx, first.ID)
	if err != nil || row.State != model.Dead || row.Attempts != 2 {
		t.Fatal(row, err)
	}
}

func TestBackgroundPublishRecordedLateFailure(t *testing.T) {
	f := newBackground(t, "bg_late_publish")
	id := addEvent(t, f, 1)
	ctx := context.Background()
	sender := &bgSender{}
	mustExec(t, f.db, "ALTER TABLE event_outbox ADD CONSTRAINT ck_bg_publish CHECK(state<>'published')")
	if _, err := f.store.PublishOne(ctx, sender, f.codec, "isolated"); err == nil {
		t.Fatal("late publish recording failure ignored")
	}
	row, err := f.store.Event(ctx, id)
	if err != nil || row.State != model.Running {
		t.Fatal(row, err)
	}
	mustExec(t, f.db, "ALTER TABLE event_outbox DROP CHECK ck_bg_publish")
	expire(t, f, bg.Events)
	if _, err = f.store.PublishOne(ctx, sender, f.codec, "isolated"); err != nil {
		t.Fatal(err)
	}
	if len(sender.bodies) != 2 || string(sender.bodies[0]) != string(sender.bodies[1]) {
		t.Fatal("recovery created different event")
	}
	// Only SendOK is treated as published, even when the SDK returns no error.
	id = addEvent(t, f, 2)
	sender.status = primitive.SendFlushDiskTimeout
	if _, err = f.store.PublishOne(ctx, sender, f.codec, "isolated"); err == nil {
		t.Fatal("uncertain broker status accepted")
	}
	due(t, f, bg.Events)
	if _, err = f.store.PublishOne(ctx, sender, f.codec, "isolated"); err == nil {
		t.Fatal("uncertain broker status accepted")
	}
	row, err = f.store.Event(ctx, id)
	if err != nil || row.State != model.Dead {
		t.Fatal(row, err)
	}
}

func TestBackgroundSchemaAndHistoryGuard(t *testing.T) {
	ctx := context.Background()
	name, dsn, db, err := testRun.NewDatabase(ctx, "bg_schema")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	m, err := migration.Open(dsn, name, "../../manifest/sql/diff/business", "business")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if err = m.Migrate(8); err != nil {
		t.Fatal(err)
	}
	if err = m.Up(); err != nil {
		t.Fatal(err)
	}
	_, _, full, err := testRun.NewDatabase(ctx, "bg_full")
	if err != nil {
		t.Fatal(err)
	}
	defer full.Close()
	for _, source := range []string{"tables.sql", "constraints.sql"} {
		ddl, err := os.ReadFile("../../manifest/sql/ddl/background/" + source)
		if err != nil {
			t.Fatal(err)
		}
		mustExec(t, full, string(ddl))
	}
	for _, table := range []string{"event_outbox", "background_task"} {
		var label, a, b string
		if err = db.QueryRow("SHOW CREATE TABLE "+table).Scan(&label, &a); err != nil {
			t.Fatal(err)
		}
		if err = full.QueryRow("SHOW CREATE TABLE "+table).Scan(&label, &b); err != nil || a != b {
			t.Fatal("DDL mismatch", table, a, b, err)
		}
	}
	if err = m.Migrate(8); err != nil {
		t.Fatal("empty down", err)
	}
	if err = m.Up(); err != nil {
		t.Fatal(err)
	}
	mustExec(t, db, "INSERT INTO event_outbox(kind,business_id,version,payload,last_error,retry_reason) VALUES('transfer.completed',1,1,'{}','','')")
	for _, statement := range []string{"UPDATE event_outbox SET state='running'", "UPDATE event_outbox SET payload='invalid'", "UPDATE event_outbox SET attempts=-1", "INSERT INTO background_task(event_id,handler,last_error,retry_reason) VALUES(999,'missing','','')"} {
		if _, err = db.Exec(statement); err == nil {
			t.Fatal("constraint accepted", statement)
		}
	}
	if err = m.Migrate(8); err == nil || !strings.Contains(err.Error(), "background_rollback_guard") {
		t.Fatal("history lost", err)
	}
	if ledgerCount(t, db, "event_outbox") != 1 {
		t.Fatal("event lost")
	}
}

func TestBackgroundKilledWorkerRecovery(t *testing.T) {
	for _, phase := range []string{"claimed", "executing"} {
		t.Run(phase, func(t *testing.T) {
			f := newBackground(t, "bg_kill_"+phase)
			id := addEvent(t, f, 1)
			h := effectHandler()
			if err := f.store.Receive(context.Background(), id, []bg.Handler{h}); err != nil {
				t.Fatal(err)
			}
			child, err := testenv.NewRun()
			if err != nil {
				t.Fatal(err)
			}
			cfg, err := testenv.AdminConfig()
			if err != nil {
				t.Fatal(err)
			}
			if err = f.db.QueryRow("SELECT DATABASE()").Scan(&cfg.DBName); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestBackgroundWorkerProcess$")
			cmd.Env = append(os.Environ(), "DEXWALLET_TEST_RUN_DIR="+child.Dir, "DEXWALLET_BG_HELPER="+phase, "DEXWALLET_BG_HELPER_DSN="+cfg.FormatDSN())
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err = cmd.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
				if err := testenv.Recover(child.Dir); err != nil {
					t.Error(err)
				}
			}()
			ticker := time.NewTicker(20 * time.Millisecond)
			defer ticker.Stop()
			for {
				if _, err = os.Stat(filepath.Join(child.Dir, "ready")); err == nil {
					break
				}
				select {
				case <-ticker.C:
				case <-ctx.Done():
					t.Fatal("worker did not reach crash point")
				}
			}
			if err = cmd.Process.Kill(); err != nil {
				t.Fatal(err)
			}
			_ = cmd.Wait()
			expire(t, f, bg.Tasks)
			effectCount(t, f, 0)
			restarted, err := bg.NewStore(f.db, f.config)
			if err != nil {
				t.Fatal(err)
			}
			f.store = restarted
			if err = f.store.Execute(ctx, *claim(t, f, bg.Tasks, h.Name), h); err != nil {
				t.Fatal(err)
			}
			effectCount(t, f, 1)
		})
	}
}
func TestBackgroundWorkerProcess(t *testing.T) {
	phase := os.Getenv("DEXWALLET_BG_HELPER")
	if phase == "" {
		return
	}
	cfg, err := testenv.ValidateDSN(os.Getenv("DEXWALLET_BG_HELPER_DSN"), "migration")
	if err != nil {
		t.Fatal(err)
	}
	cfg.ParseTime = true
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s, err := bg.NewStore(db, bg.Defaults())
	if err != nil {
		t.Fatal(err)
	}
	h := effectHandler()
	c, err := s.Claim(context.Background(), bg.Tasks, h.Name)
	if err != nil || c == nil {
		t.Fatal(c, err)
	}
	ready := func() {
		if err := os.WriteFile(filepath.Join(testRun.Dir, "ready"), []byte("ready"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if phase == "claimed" {
		ready()
		select {}
	}
	run := h.Run
	h.Run = func(ctx context.Context, tx sqlx.Session, e *model.EventOutbox) error {
		if err := run(ctx, tx, e); err != nil {
			return err
		}
		ready()
		select {}
	}
	if err = s.Execute(context.Background(), *c, h); err != nil {
		t.Fatal(err)
	}
}

func TestBackgroundRealMQAndDrain(t *testing.T) {
	f := newBackground(t, "bg_mq")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	ns := strings.TrimSuffix(testRun.Name("topic", ""), "_") + "_background"
	topic, group := ns+bg.TopicSuffix, ns+bg.GroupSuffix
	label := "background" + bg.GroupSuffix
	for _, r := range []testenv.Resource{{Kind: "topic", Name: topic}, {Kind: "retry", Name: testRun.Name("retry", label)}, {Kind: "dlq", Name: testRun.Name("dlq", label)}, {Kind: "group", Name: group}} {
		if err := testRun.Record(r.Kind, r.Name); err != nil {
			t.Fatal(err)
		}
	}
	a, err := admin.NewAdmin(admin.WithResolver(primitive.NewPassthroughResolver([]string{testenv.NameServer})))
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if err = a.CreateTopic(ctx, admin.WithTopicCreate(topic), admin.WithBrokerAddrCreate(testenv.BrokerAddress), admin.WithReadQueueNums(2), admin.WithWriteQueueNums(2)); err != nil {
		t.Fatal(err)
	}
	p, err := rocketmq.NewProducer(producer.WithNameServer(primitive.NamesrvAddr{testenv.NameServer}), producer.WithGroupName(testRun.Name("group", "bg_producer")), producer.WithInstanceName(testRun.ID+"_bg"), producer.WithRetry(0))
	if err != nil {
		t.Fatal(err)
	}
	if err = p.Start(); err != nil {
		t.Fatal(err)
	}
	defer p.Shutdown()
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	h := effectHandler()
	run := h.Run
	h.Run = func(ctx context.Context, tx sqlx.Session, e *model.EventOutbox) error {
		if err := run(ctx, tx, e); err != nil {
			return err
		}
		close(entered)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	r, err := bg.NewRuntime(f.store, p, f.codec, ns, []bg.Handler{h})
	if err != nil {
		t.Fatal(err)
	}
	if err = r.Start(ctx, []string{testenv.NameServer}, primitive.Credentials{}); err != nil {
		t.Fatal(err)
	}
	defer func() {
		r.Stop()
		drain, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := r.Wait(drain); err != nil {
			t.Error(err)
		}
	}()
	second, err := bg.NewRuntime(f.store, p, f.codec, ns, []bg.Handler{h})
	if err != nil {
		t.Fatal(err)
	}
	if err = second.Start(ctx, []string{testenv.NameServer}, primitive.Credentials{}); err != nil {
		t.Fatal(err)
	}
	defer func() {
		second.Stop()
		if err := second.Wait(ctx); err != nil {
			t.Error(err)
		}
	}()
	id := addEvent(t, f, 1)
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("event did not reach SQL handler")
	}
	r.Stop()
	second.Stop()
	short, stop := context.WithTimeout(ctx, 30*time.Millisecond)
	// A graceful drain is still waiting while this admitted transaction is in progress.
	done := make(chan error, 1)
	go func() { done <- errors.Join(r.Wait(ctx), second.Wait(ctx)) }()
	select {
	case err := <-done:
		t.Fatal("drain returned before business commit", err)
	case <-short.Done():
	}
	stop()
	once.Do(func() { close(release) })
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	row, err := model.NewBackgroundTaskModel(sqlx.NewSqlConnFromDB(f.db)).FindOneByEventIdHandler(ctx, id, h.Name)
	if err != nil || row.State != model.Succeeded {
		t.Fatal(row, err)
	}
	effectCount(t, f, 1)
	// Replay through the broker after restart. The accepted task remains succeeded.
	if err = f.store.Retry(ctx, bg.Events, id, "operator", "test replay after restart"); err != nil {
		t.Fatal(err)
	}
	h = effectHandler()
	restarted, err := bg.NewRuntime(f.store, p, f.codec, ns, []bg.Handler{h})
	if err != nil {
		t.Fatal(err)
	}
	if err = restarted.Start(ctx, []string{testenv.NameServer}, primitive.Credentials{}); err != nil {
		t.Fatal(err)
	}
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		row, err := f.store.Event(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if row.State == model.Published {
			break
		}
		select {
		case <-ticker.C:
		case <-deadline.C:
			t.Fatal("republication timed out")
		}
	}
	restarted.Stop()
	if err = restarted.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	effectCount(t, f, 1)
}

func TestBackgroundOperationsCLI(t *testing.T) {
	f := newBackground(t, "bg_cli")
	id := addEvent(t, f, 1)
	sender := &bgSender{err: errors.New("test broker unavailable")}
	for range 2 {
		_, _ = f.store.PublishOne(context.Background(), sender, f.codec, "isolated")
		due(t, f, bg.Events)
	}
	row, err := f.store.Event(context.Background(), id)
	if err != nil || row.State != model.Dead {
		t.Fatal(row, err)
	}
	content, err := os.ReadFile("../../api/etc/platform.yaml")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "operations.yaml")
	if err = os.WriteFile(path, []byte(strings.ReplaceAll(string(content), "Enabled: true", "Enabled: false")), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := testenv.AdminConfig()
	if err != nil {
		t.Fatal(err)
	}
	if err = f.db.QueryRow("SELECT DATABASE()").Scan(&cfg.DBName); err != nil {
		t.Fatal(err)
	}
	cfg.MultiStatements = false
	idx, err := f.codec.Encode(id)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for _, action := range []string{"list", "retry"} {
		args := []string{"run", "./tools/background", "-f", path, "--queue", "events", "--action", action}
		if action == "retry" {
			args = append(args, "--idx", idx, "--reason", "test recovery after dependency repair")
		}
		cmd := exec.CommandContext(ctx, "go", args...)
		cmd.Dir = "../.."
		cmd.Env = append(os.Environ(), "DEXWALLET_MYSQL_DSN="+cfg.FormatDSN(), "DEXWALLET_ID_SECRET=test-only-key123")
		output, err := cmd.CombinedOutput()
		if err != nil || !strings.Contains(string(output), idx) {
			t.Fatalf("CLI %s: %v %s", action, err, output)
		}
	}
	retried, err := f.store.Event(ctx, id)
	if err != nil || retried.State != model.Ready || retried.Attempts != 0 || retried.Generation != row.Generation || retried.RetryReason == "" || retried.RetryOperator == "" || retried.LastError == "" {
		t.Fatal(retried, err)
	}
}
