package task

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/0xabcd-k/DexWallet/backend/apperror"
	users "github.com/0xabcd-k/DexWallet/backend/logic/user"
	model "github.com/0xabcd-k/DexWallet/backend/model/task"
	usermodel "github.com/0xabcd-k/DexWallet/backend/model/user"
	"github.com/shopspring/decimal"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
	"regexp"
	"time"
)

var claimKeyPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{16,64}$`)
var ErrClaimUnknown = errors.New("task claim outcome unknown; recover original request")

type ClaimRequest struct {
	TaskID, InvitationID, TaskRevision, IdentityRevision int64
	Key, Mode, Stage                                     string
}
type ClaimContext struct {
	Snapshot              Snapshot
	Item                  Item
	State                 model.RewardState
	Invitation            *model.TaskInvitation
	Inviter               *usermodel.User
	Mode, Stage           string
	RecipientID, ParentID int64
	RecipientState        model.RewardState
	Awards                []Award
	CanInvite             bool
}

func rewardState(ctx context.Context, conn sqlx.SqlConn, userID int64) (model.RewardState, error) {
	state, err := model.ReadRewardState(ctx, conn, userID)
	if err == nil && state.Legacy {
		err = apperror.New(apperror.TaskHistoryUnknown, fmt.Errorf("user %d has completion history without settled reward state", userID))
	}
	return state, err
}
func participantStage(state model.RewardState) string {
	if state.FirstCompletionID.Valid {
		return StageReturning
	}
	return StageFirst
}
func activeTask(d *model.TaskDefinition, v Snapshot) bool {
	return d.Status == StatusActive && !v.Now.Before(d.StartsAt) && v.Now.Before(d.EndsAt)
}
func (s *Service) Preview(ctx context.Context, actor users.Actor, taskID, invitationID int64) (*ClaimContext, error) {
	v, err := s.snapshot(ctx, actor)
	if err != nil {
		return nil, err
	}
	d, err := s.definition(ctx, taskID)
	if err != nil {
		return nil, err
	}
	item, err := s.item(ctx, d, v)
	if err != nil {
		return nil, err
	}
	state, err := rewardState(ctx, s.conn, actor.UserID)
	if err != nil {
		return nil, err
	}
	c := &ClaimContext{Snapshot: v, Item: item, State: state, Mode: ClaimSelf, Stage: participantStage(state), CanInvite: state.FirstCompletionID.Valid && activeTask(d, v) && s.tx != nil}
	if invitationID != 0 {
		inv, inviter, err := s.invitation(ctx, invitationID)
		if err != nil {
			return nil, err
		}
		if inv.TaskId != taskID || !activeTask(d, v) {
			return nil, apperror.New(apperror.TaskInvitationInvalid, nil)
		}
		if inviter.Id != actor.UserID {
			c.Invitation, c.Inviter, c.Mode = inv, inviter, ClaimHelp
		}
	}
	if c.Mode == ClaimHelp {
		c.RecipientID = c.Inviter.Id
	} else if c.Stage == StageReturning {
		c.RecipientID = actor.UserID
	}
	if c.RecipientID != 0 {
		c.RecipientState, err = rewardState(ctx, s.conn, c.RecipientID)
		if err != nil {
			return nil, err
		}
		c.ParentID, err = model.Parent(ctx, s.conn, c.RecipientID)
		if err != nil {
			return nil, err
		}
	}
	inviterID := int64(0)
	if c.Inviter != nil {
		inviterID = c.Inviter.Id
	}
	c.Awards, err = CalculateRewards(RewardInput{ParticipantID: actor.UserID, InviterID: inviterID, ParentID: c.ParentID, BasePoints: d.BasePoints, Stage: c.Stage, Mode: c.Mode, RecipientPro: c.RecipientState.UnlockedAt.Valid})
	return c, err
}
func (s *Service) invitation(ctx context.Context, id int64) (*model.TaskInvitation, *usermodel.User, error) {
	inv, err := model.NewTaskInvitationModel(s.conn).FindOne(ctx, id)
	if errors.Is(err, model.ErrNotFound) {
		return nil, nil, apperror.New(apperror.TaskInvitationInvalid, nil)
	}
	if err != nil {
		return nil, nil, err
	}
	u, err := usermodel.NewUserModel(s.conn).FindOne(ctx, inv.InviterUserId)
	if err != nil {
		return nil, nil, err
	}
	state, err := rewardState(ctx, s.conn, u.Id)
	if err != nil {
		return nil, nil, err
	}
	if u.Status != usermodel.StatusActive || !state.FirstCompletionID.Valid {
		return nil, nil, apperror.New(apperror.TaskInvitationInvalid, nil)
	}
	return inv, u, nil
}
func (s *Service) Invitation(ctx context.Context, actor users.Actor, id int64) (*model.TaskInvitation, *usermodel.User, error) {
	v, err := s.snapshot(ctx, actor)
	if err != nil {
		return nil, nil, err
	}
	inv, u, err := s.invitation(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	d, err := s.definition(ctx, inv.TaskId)
	if err != nil {
		return nil, nil, err
	}
	if !activeTask(d, v) {
		return nil, nil, apperror.New(apperror.TaskInvitationInvalid, nil)
	}
	return inv, u, nil
}
func (s *Service) CreateInvitation(ctx context.Context, actor users.Actor, taskID int64) (*model.TaskInvitation, *usermodel.User, error) {
	if s.tx == nil || s.membership == nil {
		return nil, nil, apperror.New(apperror.TaskUnavailable, nil)
	}
	var result *model.TaskInvitation
	var user *usermodel.User
	err := s.tx.TransactCtx(ctx, func(ctx context.Context, session sqlx.Session) error {
		conn := sqlx.NewSqlConnFromSession(session)
		var err error
		user, err = usermodel.NewUserModel(conn).FindForUpdate(ctx, actor.UserID)
		if err != nil {
			return err
		}
		if err = authorize(user, actor); err != nil {
			return err
		}
		state, err := rewardState(ctx, conn, actor.UserID)
		if err != nil {
			return err
		}
		if !state.FirstCompletionID.Valid {
			return apperror.New(apperror.TaskInvitationRequiredFirst, nil)
		}
		d, err := model.LockDefinition(ctx, conn, taskID)
		if errors.Is(err, model.ErrNotFound) {
			return apperror.New(apperror.NotFound, nil)
		}
		if err != nil {
			return err
		}
		now, err := databaseNow(ctx, conn)
		if err != nil {
			return err
		}
		if d.VerificationMode != ModeMock || !activeTask(d, Snapshot{Now: now}) {
			return apperror.New(apperror.TaskUnavailable, nil)
		}
		invitations := model.NewTaskInvitationModel(conn)
		result, err = invitations.FindOneByTaskIdInviterUserId(ctx, taskID, actor.UserID)
		if err == nil {
			return nil
		}
		if !errors.Is(err, model.ErrNotFound) {
			return err
		}
		inserted, err := invitations.Insert(ctx, &model.TaskInvitation{TaskId: taskID, InviterUserId: actor.UserID})
		if err != nil {
			return err
		}
		id, err := inserted.LastInsertId()
		if err != nil {
			return err
		}
		result, err = invitations.FindOne(ctx, id)
		return err
	})
	return result, user, err
}

type Summary struct {
	State  model.RewardState
	Totals model.RewardTotals
}

func (s *Service) Summary(ctx context.Context, actor users.Actor) (*Summary, error) {
	if _, err := s.snapshot(ctx, actor); err != nil {
		return nil, err
	}
	state, err := rewardState(ctx, s.conn, actor.UserID)
	if err != nil {
		return nil, err
	}
	totals, err := model.ReadRewardTotals(ctx, s.conn, actor.UserID)
	return &Summary{state, totals}, err
}

type Completion struct {
	Row    *model.TaskCompletion
	Grants []*model.RewardGrant
}

func completion(ctx context.Context, conn sqlx.SqlConn, row *model.TaskCompletion) (*Completion, error) {
	if row.RuleVersion != RewardRuleVersion {
		return nil, apperror.New(apperror.TaskHistoryUnknown, nil)
	}
	grants, err := model.CompletionGrants(ctx, conn, row.Id, row.UserId)
	if err != nil {
		return nil, err
	}
	for _, g := range grants {
		if !g.VoucherId.Valid {
			return nil, fmt.Errorf("%w: grant %d has no voucher", ErrClaimUnknown, g.Id)
		}
	}
	return &Completion{row, grants}, nil
}
func (s *Service) CompletionByRequest(ctx context.Context, actor users.Actor, key string) (*Completion, error) {
	if !claimKeyPattern.MatchString(key) {
		return nil, apperror.New(apperror.InvalidArgument, nil)
	}
	if _, err := s.snapshot(ctx, actor); err != nil {
		return nil, err
	}
	row, err := model.NewTaskCompletionModel(s.conn).FindOneByUserIdRequestKey(ctx, actor.UserID, key)
	if errors.Is(err, model.ErrNotFound) {
		return nil, apperror.New(apperror.NotFound, nil)
	}
	if err != nil {
		return nil, err
	}
	return completion(ctx, s.conn, row)
}
func (s *Service) Completion(ctx context.Context, actor users.Actor, id int64) (*Completion, error) {
	if _, err := s.snapshot(ctx, actor); err != nil {
		return nil, err
	}
	row, err := model.NewTaskCompletionModel(s.conn).FindOne(ctx, id)
	if errors.Is(err, model.ErrNotFound) || (err == nil && row.UserId != actor.UserID) {
		return nil, apperror.New(apperror.NotFound, nil)
	}
	if err != nil {
		return nil, err
	}
	return completion(ctx, s.conn, row)
}
func (s *Service) Completions(ctx context.Context, actor users.Actor, taskID, before, limit int64) ([]*Completion, int64, error) {
	if taskID < 0 || before < 0 || limit < 1 || limit > MaxPageSize {
		return nil, 0, apperror.New(apperror.InvalidArgument, nil)
	}
	if _, err := s.snapshot(ctx, actor); err != nil {
		return nil, 0, err
	}
	rows, err := model.CompletionPage(ctx, s.conn, actor.UserID, taskID, before, limit+1)
	if err != nil {
		return nil, 0, err
	}
	next := int64(0)
	if int64(len(rows)) > limit {
		rows = rows[:limit]
		next = rows[len(rows)-1].Id
	}
	results := []*Completion{}
	for _, row := range rows {
		result, err := completion(ctx, s.conn, row)
		if err != nil {
			return nil, 0, err
		}
		results = append(results, result)
	}
	return results, next, nil
}
func (s *Service) Grant(ctx context.Context, actor users.Actor, id int64) (*model.RewardGrant, *model.TaskCompletion, error) {
	if _, err := s.snapshot(ctx, actor); err != nil {
		return nil, nil, err
	}
	g, err := model.NewRewardGrantModel(s.conn).FindOne(ctx, id)
	if errors.Is(err, model.ErrNotFound) || (err == nil && g.UserId != actor.UserID) {
		return nil, nil, apperror.New(apperror.NotFound, nil)
	}
	if err != nil {
		return nil, nil, err
	}
	if !g.VoucherId.Valid {
		return nil, nil, ErrClaimUnknown
	}
	c, err := model.NewTaskCompletionModel(s.conn).FindOne(ctx, g.CompletionId)
	return g, c, err
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
func matchClaim(row *model.TaskCompletion, r ClaimRequest) error {
	if row.TaskId != r.TaskID || row.Mode != r.Mode || row.InvitationId.Int64 != r.InvitationID || row.TaskRevision != r.TaskRevision || row.IdentityRevision != r.IdentityRevision || row.ParticipantStage != r.Stage {
		return apperror.New(apperror.TaskRequestConflict, nil)
	}
	return nil
}
func appCode(err error, code apperror.Code) bool {
	var app *apperror.Error
	return errors.As(err, &app) && app.Code == code
}
func nullID(id int64) sql.NullInt64 { return sql.NullInt64{Int64: id, Valid: id != 0} }
func flag(v bool) int64 {
	if v {
		return 1
	}
	return 0
}
func (c *ClaimContext) OwnAwards() (usd, points, commission, progress decimal.Decimal) {
	for _, a := range c.Awards {
		if a.UserID == c.Snapshot.UserID {
			usd = usd.Add(a.USD)
			progress = progress.Add(a.Progress)
			if a.Reason == RewardCommission {
				commission = commission.Add(a.Points)
			} else {
				points = points.Add(a.Points)
			}
		}
	}
	return
}

func databaseNow(ctx context.Context, conn sqlx.SqlConn) (time.Time, error) {
	var row struct {
		Now time.Time `db:"now_utc"`
	}
	err := conn.QueryRowCtx(ctx, &row, "SELECT UTC_TIMESTAMP(6) AS now_utc")
	return row.Now, err
}
