package rewardtransfer

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/0xabcd-k/DexWallet/backend/apperror"
	"github.com/0xabcd-k/DexWallet/backend/logic/account"
	"github.com/0xabcd-k/DexWallet/backend/logic/risk"
	users "github.com/0xabcd-k/DexWallet/backend/logic/user"
	accounts "github.com/0xabcd-k/DexWallet/backend/model/account"
	model "github.com/0xabcd-k/DexWallet/backend/model/rewardtransfer"
	tasks "github.com/0xabcd-k/DexWallet/backend/model/task"
	usermodel "github.com/0xabcd-k/DexWallet/backend/model/user"
	"github.com/0xabcd-k/DexWallet/backend/transaction"
	"github.com/shopspring/decimal"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

const (
	PendingReview                = "pending_review"
	Completed                    = "completed"
	Rejected                     = "rejected"
	Approve                      = "approve"
	Reject                       = "reject"
	MaxPageSize   int64          = 100
	MinimumUSD    int64          = 2
	ActionSubmit  account.Action = "submit"
	ActionApprove account.Action = "approve"
	ActionReject  account.Action = "reject"
)

var ErrOutcomeUnknown = errors.New("reward transfer outcome unknown; recover original request")
var requestPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{16,64}$`)
var amountPattern = regexp.MustCompile(`^(0|[1-9][0-9]{0,17})(\.[0-9]{1,2})?$`)

type Transactor interface {
	TransactCtx(context.Context, func(context.Context, sqlx.Session) error) error
}
type Service struct {
	conn     sqlx.SqlConn
	tx       Transactor
	accounts *account.Service
	risk     risk.Assessor
}

func New(conn sqlx.SqlConn, tx Transactor, a *account.Service, r risk.Assessor) *Service {
	return &Service{conn, tx, a, r}
}

type Request struct{ Key, Amount string }
type State struct {
	Available, Pro        bool
	RewardUSD, PendingUSD string
}

func ParseAmount(raw string) (decimal.Decimal, error) {
	if !amountPattern.MatchString(raw) {
		return decimal.Zero, apperror.New(apperror.InvalidArgument, nil)
	}
	amount, err := decimal.NewFromString(raw)
	if err != nil {
		return decimal.Zero, apperror.New(apperror.InvalidArgument, err)
	}
	if amount.LessThan(decimal.NewFromInt(MinimumUSD)) {
		return decimal.Zero, apperror.New(apperror.RewardTransferMinimum, nil)
	}
	return amount, nil
}
func authorize(u *usermodel.User, a users.Actor) error {
	if u.Status != usermodel.StatusActive {
		return users.ErrDisabled
	}
	if u.AuthVersion != a.AuthVersion {
		return users.ErrStaleSession
	}
	return nil
}
func (s *Service) authorized(ctx context.Context, a users.Actor) error {
	u, err := usermodel.NewUserModel(s.conn).FindOne(ctx, a.UserID)
	if err != nil {
		return err
	}
	return authorize(u, a)
}
func isPro(ctx context.Context, conn sqlx.SqlConn, id int64) (bool, error) {
	r, err := tasks.NewUserRewardStateModel(conn).FindOne(ctx, id)
	if errors.Is(err, tasks.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return r.ProUnlockedAt.Valid, nil
}
func (s *Service) State(ctx context.Context, a users.Actor) (*State, error) {
	if err := s.authorized(ctx, a); err != nil {
		return nil, err
	}
	state := &State{Available: s.risk != nil, RewardUSD: "0.00", PendingUSD: "0.00"}
	var err error
	state.Pro, err = isPro(ctx, s.conn, a.UserID)
	if err != nil {
		return nil, err
	}
	balances, err := s.accounts.List(ctx, a.UserID)
	if err != nil {
		return nil, err
	}
	for _, b := range balances {
		if b.Kind == accounts.KindRewardUSD {
			state.RewardUSD = b.Balance
		}
	}
	var pending struct {
		Amount decimal.Decimal `db:"amount"`
	}
	err = s.conn.QueryRowCtx(ctx, &pending, "SELECT COALESCE(SUM(amount),0) AS amount FROM reward_transfer WHERE user_id=? AND status=?", a.UserID, PendingReview)
	state.PendingUSD = pending.Amount.StringFixed(2)
	return state, err
}
func notFound(err error) error {
	if errors.Is(err, sqlx.ErrNotFound) {
		return apperror.New(apperror.NotFound, nil)
	}
	return err
}
func (s *Service) ByRequest(ctx context.Context, a users.Actor, key string) (*model.RewardTransfer, error) {
	if !requestPattern.MatchString(key) {
		return nil, apperror.New(apperror.InvalidArgument, nil)
	}
	if err := s.authorized(ctx, a); err != nil {
		return nil, err
	}
	r, err := model.NewRewardTransferModel(s.conn).FindOneByUserIdRequestKey(ctx, a.UserID, key)
	return r, notFound(err)
}
func (s *Service) Detail(ctx context.Context, a users.Actor, id int64) (*model.RewardTransfer, error) {
	if err := s.authorized(ctx, a); err != nil {
		return nil, err
	}
	r, err := model.NewRewardTransferModel(s.conn).FindOne(ctx, id)
	if err != nil {
		return nil, notFound(err)
	}
	if r.UserId != a.UserID {
		return nil, apperror.New(apperror.NotFound, nil)
	}
	return r, nil
}
func (s *Service) List(ctx context.Context, a users.Actor, before, limit int64) ([]*model.RewardTransfer, int64, error) {
	if err := s.authorized(ctx, a); err != nil {
		return nil, 0, err
	}
	return s.Page(ctx, a.UserID, "all", before, limit)
}

// Page and FindForReview require the caller's administrator authorization when userID is zero.
func (s *Service) Page(ctx context.Context, userID int64, status string, before, limit int64) ([]*model.RewardTransfer, int64, error) {
	if before < 0 || limit < 1 || limit > MaxPageSize || (status != "all" && status != PendingReview && status != Completed && status != Rejected) {
		return nil, 0, apperror.New(apperror.InvalidArgument, nil)
	}
	rows := []*model.RewardTransfer{}
	q := "SELECT * FROM reward_transfer WHERE 1=1"
	args := []any{}
	if userID != 0 {
		q += " AND user_id=?"
		args = append(args, userID)
	}
	if status != "all" {
		q += " AND status=?"
		args = append(args, status)
	}
	if before > 0 {
		q += " AND id<?"
		args = append(args, before)
	}
	q += " ORDER BY id DESC LIMIT ?"
	args = append(args, limit+1)
	if err := s.conn.QueryRowsCtx(ctx, &rows, q, args...); err != nil {
		return nil, 0, err
	}
	var next int64
	if int64(len(rows)) > limit {
		rows = rows[:limit]
		next = rows[len(rows)-1].Id
	}
	return rows, next, nil
}
func (s *Service) FindForReview(ctx context.Context, id int64) (*model.RewardTransfer, error) {
	r, err := model.NewRewardTransferModel(s.conn).FindOne(ctx, id)
	return r, notFound(err)
}
func match(r *model.RewardTransfer, amount decimal.Decimal) error {
	if !r.Amount.Equal(amount) {
		return apperror.New(apperror.RewardTransferRequestConflict, nil)
	}
	return nil
}
func missing(err error) bool {
	var e *apperror.Error
	return errors.As(err, &e) && e.Code == apperror.NotFound
}
func (s *Service) Submit(ctx context.Context, a users.Actor, req Request) (*model.RewardTransfer, error) {
	if !requestPattern.MatchString(req.Key) {
		return nil, apperror.New(apperror.InvalidArgument, nil)
	}
	amount, err := ParseAmount(req.Amount)
	if err != nil {
		return nil, err
	}
	// Recover committed requests before eligibility or risk can change.
	original, err := s.ByRequest(ctx, a, req.Key)
	if err == nil {
		return original, match(original, amount)
	}
	if !missing(err) {
		return nil, err
	}
	state, err := s.State(ctx, a)
	if err != nil {
		return nil, err
	}
	if !state.Pro {
		return nil, apperror.New(apperror.RewardTransferProRequired, nil)
	}
	balance, err := decimal.NewFromString(state.RewardUSD)
	if err != nil {
		return nil, err
	}
	if balance.LessThan(amount) {
		return nil, account.ErrInsufficientBalance
	}
	if s.risk == nil {
		return nil, apperror.New(apperror.RewardRiskUnavailable, nil)
	}
	// External evaluation is outside the funds transaction. Recheck identity,
	// eligibility, request idempotency and spendable balance under locks below.
	decision, err := s.risk.Assess(ctx, risk.Request{UserID: a.UserID, Amount: amount})
	if err != nil {
		return nil, apperror.New(apperror.RewardRiskUnavailable, err)
	}
	if err = decision.Validate(); err != nil {
		return nil, apperror.New(apperror.RewardRiskUnavailable, err)
	}
	var result *model.RewardTransfer
	err = s.tx.TransactCtx(ctx, func(ctx context.Context, session sqlx.Session) error {
		conn := sqlx.NewSqlConnFromSession(session)
		u, err := usermodel.NewUserModel(conn).FindForUpdate(ctx, a.UserID)
		if err != nil {
			return err
		}
		if err = authorize(u, a); err != nil {
			return err
		}
		m := model.NewRewardTransferModel(conn)
		row, err := m.FindOneByUserIdRequestKey(ctx, a.UserID, req.Key)
		if err == nil {
			result = row
			return match(row, amount)
		}
		if !errors.Is(err, model.ErrNotFound) {
			return err
		}
		pro, err := isPro(ctx, conn, a.UserID)
		if err != nil {
			return err
		}
		if !pro {
			return apperror.New(apperror.RewardTransferProRequired, nil)
		}
		row = &model.RewardTransfer{UserId: a.UserID, RequestKey: req.Key, Amount: amount, Status: PendingReview, RiskDecision: decision.Action, RiskVersion: decision.Version, RiskReason: decision.Reason}
		if decision.Action == risk.Allow {
			row.Status = Completed
			row.ResolvedAt = sql.NullTime{Time: time.Now().UTC(), Valid: true}
		}
		inserted, err := m.Insert(ctx, row)
		if err != nil {
			return err
		}
		row.Id, err = inserted.LastInsertId()
		if err != nil {
			return err
		}
		destination := account.Entry{System: account.SystemRewardTransit, Side: account.SideCredit, Amount: amount}
		if row.Status == Completed {
			destination = account.Credit(a.UserID, accounts.KindUSD, amount)
		}
		voucher, err := s.accounts.Post(ctx, session, account.Posting{Source: account.Source{Type: account.BusinessRewardTransfer, ID: row.Id, Action: ActionSubmit}, Entries: []account.Entry{account.Debit(a.UserID, accounts.KindRewardUSD, amount), destination}})
		if err != nil {
			return err
		}
		row.SubmissionVoucherId = sql.NullInt64{Int64: voucher, Valid: true}
		if err = m.Update(ctx, row); err != nil {
			return err
		}
		result, err = m.FindOne(ctx, row.Id)
		return err
	})
	var failure *transaction.Failure
	if errors.As(err, &failure) && failure.Unknown() {
		original, lookup := s.ByRequest(ctx, a, req.Key)
		if lookup == nil {
			logx.WithContext(ctx).Errorf("reward transfer %d recovered after unknown commit: %v", original.Id, err)
			return original, match(original, amount)
		}
		return nil, fmt.Errorf("%w: %w", ErrOutcomeUnknown, errors.Join(err, lookup))
	}
	if err != nil {
		return nil, err
	}
	return result, nil
}

// ReviewInSession is called only by the backoffice use case after locked admin
// authorization. Accepted funds continue processing even if the user is disabled.
func (s *Service) ReviewInSession(ctx context.Context, session sqlx.Session, id, adminID int64, decision, reason string) (*model.RewardTransfer, bool, error) {
	if err := transaction.RequireSession(ctx, session); err != nil {
		return nil, false, err
	}
	reason = strings.TrimSpace(reason)
	if id <= 0 || adminID <= 0 || reason == "" || utf8.RuneCountInString(reason) > 512 || (decision != Approve && decision != Reject) {
		return nil, false, apperror.New(apperror.InvalidArgument, nil)
	}
	conn := sqlx.NewSqlConnFromSession(session)
	m := model.NewRewardTransferModel(conn)
	seed, err := m.FindOne(ctx, id)
	if err != nil {
		return nil, false, notFound(err)
	}
	// Match the task/draw/transfer user-before-account lock order.
	if _, err = usermodel.NewUserModel(conn).FindForUpdate(ctx, seed.UserId); err != nil {
		return nil, false, err
	}
	var row model.RewardTransfer
	if err = conn.QueryRowCtx(ctx, &row, "SELECT * FROM reward_transfer WHERE id=? FOR UPDATE", id); err != nil {
		return nil, false, notFound(err)
	}
	status, action, kind := Completed, ActionApprove, accounts.KindUSD
	if decision == Reject {
		status, action, kind = Rejected, ActionReject, accounts.KindRewardUSD
	}
	if row.Status != PendingReview {
		if row.Status == status && row.ReviewerId.Valid && row.ReviewerId.Int64 == adminID && row.ReviewReason == reason {
			return &row, false, nil
		}
		return nil, false, apperror.New(apperror.Conflict, nil)
	}
	voucher, err := s.accounts.Post(ctx, session, account.Posting{Source: account.Source{Type: account.BusinessRewardTransfer, ID: id, Action: action}, OriginalVoucherID: row.SubmissionVoucherId.Int64, Entries: []account.Entry{{System: account.SystemRewardTransit, Side: account.SideDebit, Amount: row.Amount}, account.Credit(row.UserId, kind, row.Amount)}})
	if err != nil {
		return nil, false, err
	}
	row.Status = status
	row.ReviewerId = sql.NullInt64{Int64: adminID, Valid: true}
	row.ReviewReason = reason
	row.ResolutionVoucherId = sql.NullInt64{Int64: voucher, Valid: true}
	row.ResolvedAt = sql.NullTime{Time: time.Now().UTC(), Valid: true}
	if err = m.Update(ctx, &row); err != nil {
		return nil, false, err
	}
	return &row, true, nil
}
