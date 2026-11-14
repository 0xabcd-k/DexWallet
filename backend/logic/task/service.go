// Package task provides task definitions, identity eligibility and read-only
// verification previews. Completion and reward settlement are a later use case.
package task

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/apperror"
	users "github.com/0xabcd-k/DexWallet/backend/logic/user"
	model "github.com/0xabcd-k/DexWallet/backend/model/task"
	usermodel "github.com/0xabcd-k/DexWallet/backend/model/user"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

const (
	KindTelegramChannelFollow = "telegram_channel_follow"
	StatusDraft               = "draft"
	StatusActive              = "active"
	StatusPaused              = "paused"
	Eligible                  = "eligible"
	TelegramRequired          = "telegram_required"
	NotStarted                = "not_started"
	Expired                   = "expired"
	Paused                    = "paused"
	Completed                 = "completed"
	OutcomeMember             = "member"
	OutcomeNotMember          = "not_member"
	PageSize                  = 20
	MaxPageSize               = 100
)

type Service struct {
	conn        sqlx.SqlConn
	definitions model.TaskDefinitionModel
	membership  Membership
}

func New(conn sqlx.SqlConn, membership Membership) *Service {
	return &Service{conn: conn, definitions: model.NewTaskDefinitionModel(conn), membership: membership}
}

type Snapshot struct {
	UserID           int64         `db:"user_id"`
	Status           string        `db:"status"`
	AuthVersion      int64         `db:"auth_version"`
	IdentityRevision int64         `db:"identity_revision"`
	IdentityID       sql.NullInt64 `db:"identity_id"`
	BindingVersion   sql.NullInt64 `db:"binding_version"`
	TelegramUserID   sql.NullInt64 `db:"telegram_user_id"`
	Now              time.Time     `db:"now_utc"`
}
type Item struct {
	Definition  *model.TaskDefinition
	Eligibility string
}
type Page struct {
	Items  []Item
	NextID int64
}
type CheckResult struct {
	Item      Item
	Outcome   string
	CheckedAt time.Time
}

func (s *Service) snapshot(ctx context.Context, actor users.Actor) (Snapshot, error) {
	var v Snapshot
	// Read the current identity and authentication version in one statement, not the display cache.
	err := s.conn.QueryRowCtx(ctx, &v, "SELECT u.id AS user_id,u.status,u.auth_version,u.identity_revision,t.id AS identity_id,t.binding_version,t.telegram_user_id,UTC_TIMESTAMP(6) AS now_utc FROM user u LEFT JOIN telegram_identity t ON t.user_id=u.id WHERE u.id=?", actor.UserID)
	if errors.Is(err, sqlx.ErrNotFound) {
		return v, users.ErrStaleSession
	}
	if err != nil {
		return v, err
	}
	if v.Status != usermodel.StatusActive {
		return v, users.ErrDisabled
	}
	if actor.AuthVersion != v.AuthVersion {
		return v, users.ErrStaleSession
	}
	return v, nil
}
func (s *Service) item(ctx context.Context, d *model.TaskDefinition, v Snapshot) (Item, error) {
	item := Item{Definition: d, Eligibility: Eligible}
	switch {
	case d.Status == StatusPaused:
		item.Eligibility = Paused
	case v.Now.Before(d.StartsAt):
		item.Eligibility = NotStarted
	case !v.Now.Before(d.EndsAt):
		item.Eligibility = Expired
	case !v.IdentityID.Valid:
		item.Eligibility = TelegramRequired
	default:
		var used int64
		if err := s.conn.QueryRowCtx(ctx, &used, "SELECT COUNT(*) FROM task_completion WHERE task_id=? AND telegram_identity_id=?", d.Id, v.IdentityID.Int64); err != nil {
			return Item{}, err
		}
		if used > 0 {
			item.Eligibility = Completed
		}
	}
	return item, nil
}
func (s *Service) List(ctx context.Context, actor users.Actor, before, limit int64) (Page, error) {
	page := Page{Items: []Item{}}
	if before < 0 || limit < 1 || limit > MaxPageSize {
		return page, apperror.New(apperror.InvalidArgument, nil)
	}
	v, err := s.snapshot(ctx, actor)
	if err != nil {
		return page, err
	}
	if s.membership == nil {
		return page, nil
	}
	rows, err := s.definitions.ListVisible(ctx, ModeMock, before, limit+1)
	if err != nil {
		return page, err
	}
	if int64(len(rows)) > limit {
		rows = rows[:limit]
		page.NextID = rows[len(rows)-1].Id
	}
	for _, d := range rows {
		item, err := s.item(ctx, d, v)
		if err != nil {
			return page, err
		}
		page.Items = append(page.Items, item)
	}
	return page, nil
}
func (s *Service) definition(ctx context.Context, id int64) (*model.TaskDefinition, error) {
	if s.membership == nil {
		return nil, apperror.New(apperror.NotFound, nil)
	}
	d, err := s.definitions.FindOne(ctx, id)
	if errors.Is(err, model.ErrNotFound) {
		return nil, apperror.New(apperror.NotFound, nil)
	}
	if err != nil {
		return nil, err
	}
	if d.Status == StatusDraft || d.VerificationMode != ModeMock {
		return nil, apperror.New(apperror.NotFound, nil)
	}
	if d.Kind != KindTelegramChannelFollow {
		return nil, fmt.Errorf("unsupported task kind %q", d.Kind)
	}
	return d, nil
}
func (s *Service) Detail(ctx context.Context, actor users.Actor, id int64) (Item, error) {
	v, err := s.snapshot(ctx, actor)
	if err != nil {
		return Item{}, err
	}
	d, err := s.definition(ctx, id)
	if err != nil {
		return Item{}, err
	}
	return s.item(ctx, d, v)
}
func eligibilityError(value string) error {
	switch value {
	case Eligible:
		return nil
	case TelegramRequired:
		return apperror.New(apperror.TaskTelegramRequired, nil)
	case Completed:
		return apperror.New(apperror.TaskAlreadyCompleted, nil)
	default:
		return apperror.New(apperror.TaskUnavailable, nil)
	}
}

// Check produces no durable proof and no completion. A future claim must verify
// again and recheck identity/task/eligibility in its own reward transaction.
func (s *Service) Check(ctx context.Context, actor users.Actor, id int64) (*CheckResult, error) {
	v, err := s.snapshot(ctx, actor)
	if err != nil {
		return nil, err
	}
	d, err := s.definition(ctx, id)
	if err != nil {
		return nil, err
	}
	item, err := s.item(ctx, d, v)
	if err != nil {
		return nil, err
	}
	if err = eligibilityError(item.Eligibility); err != nil {
		return nil, err
	}
	member, err := s.membership.GetMember(ctx, d.ChannelId, v.TelegramUserID.Int64)
	if err != nil {
		return nil, apperror.New(apperror.TaskVerificationUnavailable, err)
	}
	matches, err := isMember(member, v.TelegramUserID.Int64)
	if err != nil {
		return nil, apperror.New(apperror.TaskVerificationUnavailable, err)
	}
	after, err := s.snapshot(ctx, actor)
	if err != nil {
		return nil, err
	}
	if after.IdentityRevision != v.IdentityRevision || after.IdentityID != v.IdentityID || after.BindingVersion != v.BindingVersion {
		return nil, apperror.New(apperror.TaskBindingChanged, nil)
	}
	current, err := s.definition(ctx, id)
	if err != nil {
		return nil, err
	}
	if current.Revision != d.Revision || !current.UpdatedAt.Equal(d.UpdatedAt) {
		return nil, apperror.New(apperror.TaskUnavailable, nil)
	}
	item, err = s.item(ctx, current, after)
	if err != nil {
		return nil, err
	}
	if err = eligibilityError(item.Eligibility); err != nil {
		return nil, err
	}
	result := &CheckResult{Item: item, Outcome: OutcomeNotMember, CheckedAt: after.Now}
	if matches {
		result.Outcome = OutcomeMember
	}
	return result, nil
}
