package transfer

import (
	"context"
	"errors"
	"regexp"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/apperror"
	userlogic "github.com/0xabcd-k/DexWallet/backend/logic/user"
	model "github.com/0xabcd-k/DexWallet/backend/model/transfer"
	usermodel "github.com/0xabcd-k/DexWallet/backend/model/user"
	"github.com/shopspring/decimal"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

var (
	ErrRestricted  = errors.New("payments restricted following identity replacement")
	ErrAmountLimit = errors.New("amount outside transfer plan limits")
	ErrDailyLimit  = errors.New("daily transfer limit exceeded")
)
var amountPattern = regexp.MustCompile(`^(0|[1-9][0-9]{0,17})(\.[0-9]{1,2})?$`)

func ParseAmount(raw string) (decimal.Decimal, error) {
	if !amountPattern.MatchString(raw) {
		return decimal.Zero, ErrInvalidRequest
	}
	v, err := decimal.NewFromString(raw)
	if err != nil || !v.IsPositive() {
		return decimal.Zero, ErrInvalidRequest
	}
	return v, nil
}
func ValidRequestKey(key string) bool { return requestKeyPattern.MatchString(key) }
func ValidatePlan(p *model.TransferLimitPlan) error {
	if p.Name == "" || len([]rune(p.Name)) > 128 || p.MinAmount.Sign() <= 0 || p.MinAmount.GreaterThan(p.MaxAmount) || p.MaxAmount.GreaterThan(p.DailyOutgoingLimit) {
		return ErrInvalidRequest
	}
	for _, v := range []decimal.Decimal{p.MinAmount, p.MaxAmount, p.DailyOutgoingLimit} {
		if _, err := ParseAmount(v.String()); err != nil {
			return err
		}
	}
	return nil
}

type Policy struct {
	Plan            *model.TransferLimitPlan
	Used, Remaining decimal.Decimal
	ResetAt         time.Time
	RestrictedUntil time.Time
}

// ReadPolicy must use the transaction connection after the user lock for writes.
// One database instant defines both the budget day and order creation time.
func ReadPolicy(ctx context.Context, conn sqlx.SqlConn, u *usermodel.User) (*Policy, time.Time, error) {
	var clock struct {
		Now time.Time `db:"now_utc"`
	}
	var now time.Time
	if err := conn.QueryRowCtx(ctx, &clock, "SELECT UTC_TIMESTAMP(6) AS now_utc"); err != nil {
		return nil, now, err
	}
	now = clock.Now.UTC()
	p, err := model.NewTransferLimitPlanModel(conn).FindOne(ctx, u.TransferLimitPlanId)
	if err != nil {
		return nil, now, err
	}
	if err = ValidatePlan(p); err != nil {
		return nil, now, err
	}
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, 1)
	var total struct {
		Used decimal.Decimal `db:"used"`
	}
	if err = conn.QueryRowCtx(ctx, &total, "SELECT COALESCE(SUM(amount),0) AS used FROM transfer_order WHERE sender_id=? AND created_at>=? AND created_at<? AND voucher_id IS NOT NULL", u.Id, start, end); err != nil {
		return nil, now, err
	}
	used := total.Used
	policy := &Policy{Plan: p, Used: used, Remaining: decimal.Max(decimal.Zero, p.DailyOutgoingLimit.Sub(used)), ResetAt: end}
	if u.PaymentRestrictedUntil.Valid && now.Before(u.PaymentRestrictedUntil.Time) {
		policy.RestrictedUntil = u.PaymentRestrictedUntil.Time.UTC()
	}
	return policy, now, nil
}
func (s *Service) Policy(ctx context.Context, actor userlogic.Actor) (*Policy, error) {
	var p *Policy
	err := s.tx.TransactCtx(ctx, func(ctx context.Context, tx sqlx.Session) error {
		u, err := s.users.WithSession(tx).FindForUpdate(ctx, actor.UserID)
		if err != nil {
			return err
		}
		if err = checkActor(u, actor); err != nil {
			return err
		}
		p, _, err = ReadPolicy(ctx, sqlx.NewSqlConnFromSession(tx), u)
		return err
	})
	return p, err
}
func (s *Service) Target(ctx context.Context, actor userlogic.Actor, id int64) (*usermodel.User, error) {
	if id <= 0 || id == actor.UserID {
		return nil, ErrInvalidRequest
	}
	u, err := s.users.FindOne(ctx, id)
	if errors.Is(err, usermodel.ErrNotFound) || (err == nil && u.Status != usermodel.StatusActive) {
		return nil, ErrRecipientUnavailable
	}
	return u, err
}
func (s *Service) Detail(ctx context.Context, actor userlogic.Actor, id int64) (*model.TransferOrder, error) {
	u, err := s.users.FindOne(ctx, actor.UserID)
	if err != nil {
		return nil, err
	}
	if err = checkActor(u, actor); err != nil {
		return nil, err
	}
	row, err := s.orders.FindOne(ctx, id)
	if errors.Is(err, model.ErrNotFound) || (err == nil && row.SenderId != actor.UserID && row.RecipientId != actor.UserID) {
		return nil, apperror.New(apperror.NotFound, nil)
	}
	if err != nil {
		return nil, err
	}
	return row, completed(row)
}
