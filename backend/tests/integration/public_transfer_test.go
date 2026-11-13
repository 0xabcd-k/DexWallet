//go:build integration

package integration

import (
	"context"
	"errors"
	"fmt"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
	"sync"
	"testing"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/apperror"
	"github.com/0xabcd-k/DexWallet/backend/internal/migration"
	transfers "github.com/0xabcd-k/DexWallet/backend/logic/transfer"
	users "github.com/0xabcd-k/DexWallet/backend/logic/user"
	model "github.com/0xabcd-k/DexWallet/backend/model/user"
	"github.com/0xabcd-k/DexWallet/backend/transaction"
)

func TestPublicTransferLimitsConcurrencyAndPlanChanges(t *testing.T) {
	ctx := context.Background()
	f := newLedger(t, "public_limits")
	s := transferService(f, f.tx)
	if _, err := f.post(funding(1, 1, "1000")); err != nil {
		t.Fatal(err)
	}
	mustExec(t, f.db, "INSERT INTO transfer_limit_plan(id,name,min_amount,max_amount,daily_outgoing_limit,creator_id,request_key) VALUES(2,'One cent',0.01,0.01,0.01,0,'one_cent_fixture'),(3,'Larger',0.01,100,600,0,'larger_fixture_0')")
	mustExec(t, f.db, "UPDATE user SET transfer_limit_plan_id=2 WHERE id=1")
	var wg sync.WaitGroup
	errs := make([]error, 8)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = s.Submit(ctx, transferActor(1), transferRequest(fmt.Sprintf("last_cent_key_%04d", i), 2, "0.01"))
		}()
	}
	wg.Wait()
	successes := 0
	for _, err := range errs {
		if err == nil {
			successes++
		} else if !errors.Is(err, transfers.ErrDailyLimit) {
			t.Fatal(err)
		}
	}
	if successes != 1 {
		t.Fatal("concurrent daily limit", successes)
	}
	p, err := s.Policy(ctx, transferActor(1))
	if err != nil || p.Used.StringFixed(2) != "0.01" || !p.Remaining.IsZero() || p.ResetAt.Location() != time.UTC {
		t.Fatal(p, err)
	}
	mustExec(t, f.db, "UPDATE user SET transfer_limit_plan_id=3 WHERE id=1")
	p, err = s.Policy(ctx, transferActor(1))
	if err != nil || p.Remaining.StringFixed(2) != "599.99" {
		t.Fatal(p, err)
	}
	r := transferRequest("larger_request_00", 2, "100")
	original, err := s.Submit(ctx, transferActor(1), r)
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, f.db, "UPDATE user SET transfer_limit_plan_id=2,payment_restricted_until=DATE_ADD(UTC_TIMESTAMP(),INTERVAL 1 DAY),nickname='changed' WHERE id=1")
	recovered, err := s.Submit(ctx, transferActor(1), r)
	if err != nil || recovered.Id != original.Id || recovered.SenderNickname != "one" || recovered.LimitPlanId.Int64 != 3 {
		t.Fatal(recovered, err)
	}
	p, err = s.Policy(ctx, transferActor(1))
	if err != nil || p.Used.StringFixed(2) != "100.01" || !p.Remaining.IsZero() {
		t.Fatal(p, err)
	}
	if _, err = s.Submit(ctx, transferActor(1), transferRequest("frozen_request_0", 2, "0.01")); !errors.Is(err, transfers.ErrRestricted) {
		t.Fatal(err)
	}
	if _, err = s.Detail(ctx, transferActor(3), original.Id); err == nil {
		t.Fatal("third party read")
	} else {
		var app *apperror.Error
		if !errors.As(err, &app) || app.Code != apperror.NotFound {
			t.Fatal(err)
		}
	}
	if _, err = s.Detail(ctx, transferActor(2), original.Id); err != nil {
		t.Fatal(err)
	}
	assertTransferCount(t, f, 2)
}
func TestIdentityHistoryRestrictionAndPreviewRevision(t *testing.T) {
	ctx := context.Background()
	db, s := userDatabase(t, "identity_restriction")
	initial, err := s.ResolveOrCreateVerifiedEmail(ctx, normalizedEmail(t, "first@example.com"))
	if err != nil {
		t.Fatal(err)
	}
	actor := users.Actor{UserID: initial.User.Id, AuthVersion: initial.User.AuthVersion}
	tg := &users.VerifiedTelegram{TelegramUserID: 916001, FirstName: "Test"}
	impact, err := s.IdentityImpact(ctx, actor, users.Bind, model.IdentityTelegram, users.EmailAddress{}, tg)
	if err != nil || impact.Replacement {
		t.Fatal(impact, err)
	}
	firstTG, err := s.ChangeIdentity(ctx, actor, users.Bind, model.IdentityTelegram, users.EmailAddress{}, tg, &users.IdentityApproval{Revision: impact.Revision})
	if err != nil || firstTG.User.PaymentRestrictedUntil.Valid {
		t.Fatal(firstTG, err)
	}
	// A preview from before this first bind cannot confirm another mutation.
	_, err = s.ChangeIdentity(ctx, actor, users.Replace, model.IdentityEmail, normalizedEmail(t, "second@example.com"), nil, &users.IdentityApproval{Revision: impact.Revision, Replacement: true})
	if !errors.Is(err, users.ErrBindingChanged) {
		t.Fatal(err)
	}
	unbound, err := s.ChangeIdentity(ctx, actor, users.Unbind, model.IdentityEmail, users.EmailAddress{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if unbound.User.PaymentRestrictedUntil.Valid || unbound.User.LastEmailIdentityId != initial.Binding.IdentityID {
		t.Fatal("unlink lost history", unbound.User)
	}
	actor.AuthVersion = unbound.User.AuthVersion
	same, err := s.ChangeIdentity(ctx, actor, users.Bind, model.IdentityEmail, normalizedEmail(t, "first@example.com"), nil)
	if err != nil || same.User.PaymentRestrictedUntil.Valid || same.User.AuthVersion != actor.AuthVersion {
		t.Fatal(same, err)
	}
	unbound, err = s.ChangeIdentity(ctx, actor, users.Unbind, model.IdentityEmail, users.EmailAddress{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	actor.AuthVersion = unbound.User.AuthVersion
	target := normalizedEmail(t, "second@example.com")
	impact, err = s.IdentityImpact(ctx, actor, users.Bind, model.IdentityEmail, target, nil)
	if err != nil || !impact.Replacement {
		t.Fatal(impact, err)
	}
	changed, err := s.ChangeIdentity(ctx, actor, users.Bind, model.IdentityEmail, target, nil, &users.IdentityApproval{Revision: impact.Revision, Replacement: true})
	if err != nil {
		t.Fatal(err)
	}
	if !changed.User.PaymentRestrictedUntil.Valid || changed.User.AuthVersion != actor.AuthVersion+1 || time.Until(changed.User.PaymentRestrictedUntil.Time) < 23*time.Hour {
		t.Fatal(changed.User)
	}
	actor.AuthVersion = changed.User.AuthVersion
	repeated, err := s.ChangeIdentity(ctx, actor, users.Bind, model.IdentityEmail, target, nil)
	if err != nil || repeated.User.PaymentRestrictedUntil != changed.User.PaymentRestrictedUntil {
		t.Fatal("no-op extended restriction", repeated, err)
	}
	unbound, err = s.ChangeIdentity(ctx, actor, users.Unbind, model.IdentityEmail, users.EmailAddress{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	actor.AuthVersion = unbound.User.AuthVersion
	same, err = s.ChangeIdentity(ctx, actor, users.Bind, model.IdentityEmail, target, nil)
	if err != nil || same.User.PaymentRestrictedUntil != changed.User.PaymentRestrictedUntil {
		t.Fatal("relink cleared restriction", same, err)
	}
	// Direct replacement must not shorten an already longer restriction.
	mustExec(t, db, "UPDATE user SET payment_restricted_until=DATE_ADD(UTC_TIMESTAMP(6),INTERVAL 2 DAY) WHERE id=?", actor.UserID)
	previous, err := s.FindUserByID(ctx, actor.UserID)
	if err != nil {
		t.Fatal(err)
	}
	direct, err := s.ChangeIdentity(ctx, actor, users.Replace, model.IdentityEmail, normalizedEmail(t, "first@example.com"), nil)
	if err != nil || direct.User.AuthVersion != actor.AuthVersion+1 || direct.User.PaymentRestrictedUntil != previous.PaymentRestrictedUntil {
		t.Fatal(direct, err)
	}
	actor.AuthVersion = direct.User.AuthVersion
	unbound, err = s.ChangeIdentity(ctx, actor, users.Unbind, model.IdentityTelegram, users.EmailAddress{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	actor.AuthVersion = unbound.User.AuthVersion
	// Explicit unknown legacy history cannot silently become a first bind.
	mustExec(t, db, "UPDATE user SET last_telegram_identity_id=-1 WHERE id=?", actor.UserID)
	_, err = s.IdentityImpact(ctx, actor, users.Bind, model.IdentityTelegram, users.EmailAddress{}, tg)
	if !errors.Is(err, users.ErrHistoryUnknown) {
		t.Fatal(err)
	}
}

func TestPublicTransferUTCResetUsesOrderCreationDay(t *testing.T) {
	f := newLedger(t, "public_midnight")
	ctx := context.Background()
	if _, err := f.post(funding(1, 1, "1000")); err != nil {
		t.Fatal(err)
	}
	mustExec(t, f.db, "INSERT INTO transfer_limit_plan(id,name,min_amount,max_amount,daily_outgoing_limit,creator_id,request_key) VALUES(2,'Daily one',0.01,1,1,0,'daily_one_fixture')")
	mustExec(t, f.db, "UPDATE user SET transfer_limit_plan_id=2 WHERE id=1")
	at := func(instant time.Time) *transfers.Service {
		return transferService(f, transferTx(func(ctx context.Context, fn func(context.Context, sqlx.Session) error) error {
			return f.tx.TransactCtx(ctx, func(ctx context.Context, tx sqlx.Session) error {
				if _, err := tx.ExecCtx(ctx, "SET timestamp=?", instant.Unix()); err != nil {
					return err
				}
				defer tx.ExecCtx(context.Background(), "SET timestamp=0")
				return fn(ctx, tx)
			})
		}))
	}
	before := time.Date(2028, 1, 2, 23, 59, 59, 0, time.UTC)
	after := before.Add(time.Second)
	first, err := at(before).Submit(ctx, transferActor(1), transferRequest("before_midnight_0", 2, "1"))
	if err != nil {
		t.Fatal(err)
	}
	if !first.CreatedAt.Equal(before) {
		t.Fatal("incorrect order date", first.CreatedAt)
	}
	if _, err = at(before).Submit(ctx, transferActor(1), transferRequest("before_midnight_1", 2, "0.01")); !errors.Is(err, transfers.ErrDailyLimit) {
		t.Fatal(err)
	}
	second, err := at(after).Submit(ctx, transferActor(1), transferRequest("after_midnight_00", 2, "1"))
	if err != nil || !second.CreatedAt.Equal(after) {
		t.Fatal(second, err)
	}
	p, err := at(after).Policy(ctx, transferActor(1))
	if err != nil || p.Used.StringFixed(2) != "1.00" || !p.ResetAt.Equal(after.AddDate(0, 0, 1)) {
		t.Fatal(p, err)
	}
	assertTransferCount(t, f, 2)
}

func TestPublicTransferMigrationHistoryAndRegistrationDefault(t *testing.T) {
	ctx := context.Background()
	name, dsn, db, err := testRun.NewDatabase(ctx, "public_upgrade")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	m, err := migration.Open(dsn, name, "../../manifest/sql/diff/business", "business")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if err = m.Migrate(9); err != nil {
		t.Fatal(err)
	}
	mustExec(t, db, "INSERT INTO user(id,nickname) VALUES(1,'Existing'),(2,'Unknown')")
	mustExec(t, db, "INSERT INTO email_identity(id,user_id,binding_version,email_address,email_local_part,email_domain,email_original) VALUES(1,1,1,'existing@example.com','existing','example.com','existing@example.com'),(2,NULL,2,'unbound@example.com','unbound','example.com','unbound@example.com')")
	if err = m.Up(); err != nil {
		t.Fatal(err)
	}
	conn := sqlx.NewSqlConnFromDB(db)
	um := model.NewUserModel(conn)
	first, err := um.FindOne(ctx, 1)
	if err != nil || first.LastEmailIdentityId != 1 || first.LastTelegramIdentityId != -1 || first.TransferLimitPlanId != 1 || first.Nickname != "Existing" {
		t.Fatal(first, err)
	}
	unknown, err := um.FindOne(ctx, 2)
	if err != nil || unknown.LastEmailIdentityId != -1 {
		t.Fatal(unknown, err)
	}
	mustExec(t, db, "INSERT INTO transfer_limit_plan(id,name,min_amount,max_amount,daily_outgoing_limit,creator_id,request_key) VALUES(2,'New default',0.01,2,3,0,'new_default_plan')")
	service := users.New(um, model.NewEmailIdentityModel(conn), model.NewTelegramIdentityModel(conn), transaction.New(db), 2)
	created, err := service.ResolveOrCreateVerifiedEmail(ctx, normalizedEmail(t, "new@example.com"))
	if err != nil || created.User.TransferLimitPlanId != 2 || created.User.LastEmailIdentityId != created.Binding.IdentityID || created.User.LastTelegramIdentityId != 0 {
		t.Fatal(created, err)
	}
	bad := users.New(um, model.NewEmailIdentityModel(conn), model.NewTelegramIdentityModel(conn), transaction.New(db), 999)
	if _, err = bad.ResolveOrCreateVerifiedEmail(ctx, normalizedEmail(t, "bad-default@example.com")); err == nil {
		t.Fatal("missing default plan allowed registration")
	}
	if ledgerCount(t, db, "user") != 3 {
		t.Fatal("failed default left orphan user")
	}
}

func TestIdentityPreviewDoesNotDiscloseAnotherEmailOwner(t *testing.T) {
	ctx := context.Background()
	_, s := userDatabase(t, "identity_preview_privacy")
	own, err := s.ResolveOrCreateVerifiedEmail(ctx, normalizedEmail(t, "own@example.com"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ResolveOrCreateVerifiedEmail(ctx, normalizedEmail(t, "other@example.com")); err != nil {
		t.Fatal(err)
	}
	actor := users.Actor{UserID: own.User.Id, AuthVersion: own.User.AuthVersion}
	var previous *users.IdentityImpact
	for _, email := range []string{"other@example.com", "not-registered@example.com"} {
		impact, err := s.IdentityImpact(ctx, actor, users.Replace, model.IdentityEmail, normalizedEmail(t, email), nil)
		if err != nil || !impact.Replacement {
			t.Fatal(impact, err)
		}
		if previous != nil && *previous != *impact {
			t.Fatal("preview leaked target ownership")
		}
		previous = impact
	}
}
