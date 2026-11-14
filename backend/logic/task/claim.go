package task

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/0xabcd-k/DexWallet/backend/apperror"
	"github.com/0xabcd-k/DexWallet/backend/logic/account"
	users "github.com/0xabcd-k/DexWallet/backend/logic/user"
	accounts "github.com/0xabcd-k/DexWallet/backend/model/account"
	model "github.com/0xabcd-k/DexWallet/backend/model/task"
	usermodel "github.com/0xabcd-k/DexWallet/backend/model/user"
	"github.com/0xabcd-k/DexWallet/backend/transaction"
	"github.com/go-sql-driver/mysql"
	"github.com/shopspring/decimal"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
	"slices"
	"time"
)

const claimAttempts = 2
const mysqlClaimDeadlock = 1213
const mysqlClaimLockTimeout = 1205

func (s *Service) Claim(ctx context.Context, actor users.Actor, r ClaimRequest) (result *Completion, err error) {
	if !claimKeyPattern.MatchString(r.Key) || r.TaskID <= 0 || r.TaskRevision <= 0 || r.IdentityRevision < 0 ||
		(r.Stage != StageFirst && r.Stage != StageReturning) || (r.Mode != ClaimSelf && r.Mode != ClaimHelp) ||
		(r.Mode == ClaimSelf && r.InvitationID != 0) || (r.Mode == ClaimHelp && r.InvitationID <= 0) {
		return nil, apperror.New(apperror.InvalidArgument, nil)
	}
	// A concurrent identical request can commit between the first lookup and a
	// preflight refusal. Prefer its durable result over a now-stale rejection.
	defer func() {
		if err == nil || ctx.Err() != nil {
			return
		}
		if done, lookupErr := s.CompletionByRequest(ctx, actor, r.Key); lookupErr == nil {
			result, err = done, matchClaim(done.Row, r)
		}
	}()
	original, err := s.CompletionByRequest(ctx, actor, r.Key)
	if err == nil {
		return original, matchClaim(original.Row, r)
	}
	if !appCode(err, apperror.NotFound) {
		return nil, err
	}
	if s.tx == nil || s.accounts == nil {
		return nil, apperror.New(apperror.TaskUnavailable, nil)
	}
	c, err := s.Preview(ctx, actor, r.TaskID, r.InvitationID)
	if err != nil {
		return nil, err
	}
	if err = eligibilityError(c.Item.Eligibility); err != nil {
		return nil, err
	}
	if c.Mode != r.Mode || c.Stage != r.Stage || c.Item.Definition.Revision != r.TaskRevision || c.Snapshot.IdentityRevision != r.IdentityRevision {
		return nil, apperror.New(apperror.TaskContextChanged, nil)
	}
	member, err := s.membership.GetMember(ctx, c.Item.Definition.ChannelId, c.Snapshot.TelegramUserID.Int64)
	if err != nil {
		return nil, apperror.New(apperror.TaskVerificationUnavailable, err)
	}
	matched, err := isMember(member, c.Snapshot.TelegramUserID.Int64)
	if err != nil {
		return nil, apperror.New(apperror.TaskVerificationUnavailable, err)
	}
	if !matched {
		return nil, apperror.New(apperror.TaskNotMember, nil)
	}
	verifiedAt, err := databaseNow(ctx, s.conn)
	if err != nil {
		return nil, err
	}
	for attempt := 0; attempt < claimAttempts; attempt++ {
		var result *Completion
		err = s.tx.TransactCtx(ctx, func(ctx context.Context, session sqlx.Session) error {
			var err error
			result, err = s.settle(ctx, session, actor, r, c, member, verifiedAt)
			return err
		})
		if err == nil {
			return result, nil
		}
		var failure *transaction.Failure
		if errors.As(err, &failure) {
			if failure.Unknown() {
				original, lookupErr := s.CompletionByRequest(ctx, actor, r.Key)
				if lookupErr == nil {
					lookupErr = matchClaim(original.Row, r)
				}
				if lookupErr == nil {
					logx.WithContext(ctx).Errorf("task completion %d recovered after unknown commit: %v", original.Row.Id, err)
					return original, nil
				}
				return nil, fmt.Errorf("%w: %w", ErrClaimUnknown, errors.Join(err, lookupErr))
			}
			return nil, err
		}
		var dbErr *mysql.MySQLError
		if errors.As(err, &dbErr) {
			if dbErr.Number == mysqlClaimDeadlock && attempt+1 < claimAttempts && ctx.Err() == nil {
				logx.WithContext(ctx).Errorf("task claim user %d retrying rolled-back deadlock: %v", actor.UserID, err)
				continue
			}
			if dbErr.Number == mysqlClaimDeadlock || dbErr.Number == mysqlClaimLockTimeout {
				return nil, apperror.New(apperror.TaskBusy, err)
			}
		}
		return nil, err
	}
	panic("unreachable task attempt")
}

func (s *Service) settle(ctx context.Context, session sqlx.Session, actor users.Actor, r ClaimRequest, c *ClaimContext, member Member, verifiedAt time.Time) (*Completion, error) {
	if err := transaction.RequireSession(ctx, session); err != nil {
		return nil, err
	}
	conn := sqlx.NewSqlConnFromSession(session)
	ids := []int64{actor.UserID}
	if c.RecipientID != 0 {
		ids = append(ids, c.RecipientID)
	}
	if c.ParentID != 0 {
		ids = append(ids, c.ParentID)
	}
	slices.Sort(ids)
	ids = slices.Compact(ids)
	locked := map[int64]*usermodel.User{}
	for _, id := range ids {
		u, err := usermodel.NewUserModel(conn).FindForUpdate(ctx, id)
		if err != nil {
			return nil, err
		}
		locked[id] = u
	}
	if err := authorize(locked[actor.UserID], actor); err != nil {
		return nil, err
	}
	completions := model.NewTaskCompletionModel(conn)
	// First snapshot read after locking all users: concurrent committed claims are visible.
	original, err := completions.FindOneByUserIdRequestKey(ctx, actor.UserID, r.Key)
	if err == nil {
		if err = matchClaim(original, r); err != nil {
			return nil, err
		}
		return completion(ctx, conn, original)
	}
	if !errors.Is(err, model.ErrNotFound) {
		return nil, err
	}
	states := map[int64]model.RewardState{}
	for _, id := range ids {
		state, err := rewardState(ctx, conn, id)
		if err != nil {
			return nil, err
		}
		states[id] = state
	}
	if participantStage(states[actor.UserID]) != r.Stage || locked[actor.UserID].IdentityRevision != r.IdentityRevision {
		return nil, apperror.New(apperror.TaskContextChanged, nil)
	}
	if c.Mode == ClaimHelp {
		inv, err := model.NewTaskInvitationModel(conn).FindOne(ctx, r.InvitationID)
		if err != nil {
			return nil, err
		}
		if inv.TaskId != r.TaskID || inv.InviterUserId != c.RecipientID || locked[c.RecipientID].Status != usermodel.StatusActive || !states[c.RecipientID].FirstCompletionID.Valid {
			return nil, apperror.New(apperror.TaskInvitationInvalid, nil)
		}
	}
	if c.RecipientID != 0 {
		parent, err := model.Parent(ctx, conn, c.RecipientID)
		if err != nil {
			return nil, err
		}
		if parent != c.ParentID {
			return nil, apperror.New(apperror.TaskContextChanged, nil)
		}
	}
	binding, err := usermodel.LockBinding(ctx, session, usermodel.IdentityTelegram, c.Snapshot.IdentityID.Int64)
	if err != nil {
		return nil, err
	}
	if !binding.UserId.Valid || binding.UserId.Int64 != actor.UserID || binding.BindingVersion != c.Snapshot.BindingVersion.Int64 {
		return nil, apperror.New(apperror.TaskBindingChanged, nil)
	}
	d, err := model.LockDefinition(ctx, conn, r.TaskID)
	if err != nil {
		return nil, err
	}
	now, err := databaseNow(ctx, conn)
	if err != nil {
		return nil, err
	}
	if d.Revision != r.TaskRevision || !d.UpdatedAt.Equal(c.Item.Definition.UpdatedAt) {
		return nil, apperror.New(apperror.TaskContextChanged, nil)
	}
	if d.VerificationMode != ModeMock || !activeTask(d, Snapshot{Now: now}) || verifiedAt.Before(d.StartsAt) || !verifiedAt.Before(d.EndsAt) {
		return nil, apperror.New(apperror.TaskUnavailable, nil)
	}
	var used int64
	if err = conn.QueryRowCtx(ctx, &used, "SELECT COUNT(*) FROM task_completion WHERE task_id=? AND telegram_identity_id=?", d.Id, binding.Id); err != nil {
		return nil, err
	}
	if used != 0 {
		return nil, apperror.New(apperror.TaskAlreadyCompleted, nil)
	}
	inviterID := int64(0)
	if c.Mode == ClaimHelp {
		inviterID = c.RecipientID
	}
	awards, err := CalculateRewards(RewardInput{ParticipantID: actor.UserID, InviterID: inviterID, ParentID: c.ParentID, BasePoints: d.BasePoints, Mode: r.Mode, Stage: r.Stage, RecipientPro: states[c.RecipientID].UnlockedAt.Valid})
	if err != nil {
		return nil, err
	}
	row := &model.TaskCompletion{TaskId: d.Id, TelegramIdentityId: binding.Id, UserId: actor.UserID, RequestKey: r.Key, TaskRevision: d.Revision, BasePoints: d.BasePoints, VerifiedAt: verifiedAt, Mode: r.Mode, ParticipantStage: r.Stage, InvitationId: nullID(r.InvitationID), IdentityRevision: r.IdentityRevision, BindingVersion: binding.BindingVersion, MemberStatus: member.Status, TaskTitle: d.Title, RuleVersion: RewardRuleVersion}
	inserted, err := completions.Insert(ctx, row)
	if err != nil {
		return nil, err
	}
	row.Id, err = inserted.LastInsertId()
	if err != nil {
		return nil, err
	}
	stateModel := model.NewUserRewardStateModel(conn)
	for _, id := range ids {
		state := states[id]
		// Users are already locked, so initialization cannot race another reward writer.
		if _, err = conn.ExecCtx(ctx, "INSERT INTO user_reward_state(user_id) VALUES(?) ON DUPLICATE KEY UPDATE user_id=user_id", id); err != nil {
			return nil, err
		}
		if id == actor.UserID && r.Stage == StageFirst {
			state.FirstCompletionID = nullID(row.Id)
			states[id] = state
		}
	}
	if r.Stage == StageFirst && r.Mode == ClaimHelp {
		if _, err = model.NewUserReferralModel(conn).Insert(ctx, &model.UserReferral{UserId: actor.UserID, ParentUserId: inviterID, CompletionId: row.Id}); err != nil {
			return nil, err
		}
	}
	slices.SortStableFunc(awards, func(a, b Award) int {
		if a.UserID < b.UserID {
			return -1
		}
		if a.UserID > b.UserID {
			return 1
		}
		return 0
	})
	grants := model.NewRewardGrantModel(conn)
	pointDefinition, _ := accounts.Lookup(accounts.KindTaskPoints)
	for _, a := range awards {
		state := states[a.UserID]
		after := state.Progress.Add(a.Progress)
		if err = pointDefinition.ValidateBalances(after, decimal.Zero); err != nil {
			return nil, fmt.Errorf("reward progress user %d: %w", a.UserID, err)
		}
		unlock := !state.UnlockedAt.Valid && after.GreaterThanOrEqual(decimal.NewFromInt(ProThreshold))
		grant := &model.RewardGrant{CompletionId: row.Id, UserId: a.UserID, Reason: a.Reason, RewardUsd: a.USD, Points: a.Points, ProBefore: state.Progress, ProDelta: a.Progress, ProAfter: after, WasPro: flag(state.UnlockedAt.Valid), UnlockedPro: flag(unlock)}
		inserted, err := grants.Insert(ctx, grant)
		if err != nil {
			return nil, err
		}
		grant.Id, err = inserted.LastInsertId()
		if err != nil {
			return nil, err
		}
		entries := []account.Entry{}
		if a.USD.IsPositive() {
			entries = append(entries, account.Entry{System: account.SystemRewardExpense, Side: account.SideDebit, Amount: a.USD}, account.Credit(a.UserID, accounts.KindRewardUSD, a.USD))
		}
		if a.Points.IsPositive() {
			entries = append(entries, account.Entry{System: account.SystemPointIssuance, Side: account.SideDebit, Amount: a.Points}, account.Credit(a.UserID, accounts.KindTaskPoints, a.Points))
		}
		voucher, err := s.accounts.Post(ctx, session, account.Posting{Source: account.Source{Type: account.BusinessReward, ID: grant.Id, Action: account.ActionPost}, Entries: entries})
		if err != nil {
			return nil, err
		}
		grant.VoucherId = nullID(voucher)
		if err = grants.Update(ctx, grant); err != nil {
			return nil, err
		}
		state.Progress = after
		if unlock {
			state.UnlockedAt = sql.NullTime{Time: now, Valid: true}
		}
		states[a.UserID] = state
	}
	for _, id := range ids {
		state := states[id]
		if err = stateModel.Update(ctx, &model.UserRewardState{UserId: id, FirstCompletionId: state.FirstCompletionID, ProProgress: state.Progress, ProUnlockedAt: state.UnlockedAt}); err != nil {
			return nil, err
		}
	}
	row, err = completions.FindOne(ctx, row.Id)
	if err != nil {
		return nil, err
	}
	return completion(ctx, conn, row)
}
