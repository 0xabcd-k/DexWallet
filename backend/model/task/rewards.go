package task

import (
	"context"
	"database/sql"
	"github.com/shopspring/decimal"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

type RewardState struct {
	UserID            int64           `db:"user_id"`
	FirstCompletionID sql.NullInt64   `db:"first_completion_id"`
	Progress          decimal.Decimal `db:"progress"`
	UnlockedAt        sql.NullTime    `db:"unlocked_at"`
	Legacy            bool            `db:"legacy"`
}

// One statement keeps an absent state and the legacy-history check consistent.
func ReadRewardState(ctx context.Context, conn sqlx.SqlConn, userID int64) (RewardState, error) {
	var result RewardState
	err := conn.QueryRowCtx(ctx, &result, "SELECT u.id AS user_id,r.first_completion_id,COALESCE(r.pro_progress,0) AS progress,r.pro_unlocked_at AS unlocked_at,EXISTS(SELECT 1 FROM task_completion c WHERE c.user_id=u.id AND (c.rule_version=0 OR r.first_completion_id IS NULL)) AS legacy FROM user u LEFT JOIN user_reward_state r ON r.user_id=u.id WHERE u.id=?", userID)
	return result, err
}
func Parent(ctx context.Context, conn sqlx.SqlConn, userID int64) (int64, error) {
	var parent int64
	err := conn.QueryRowCtx(ctx, &parent, "SELECT COALESCE((SELECT parent_user_id FROM user_referral WHERE user_id=?),0)", userID)
	return parent, err
}
func LockDefinition(ctx context.Context, conn sqlx.SqlConn, id int64) (*TaskDefinition, error) {
	var row TaskDefinition
	err := conn.QueryRowCtx(ctx, &row, "SELECT "+taskDefinitionRows+" FROM task_definition WHERE id=? FOR SHARE", id)
	return &row, err
}
func CompletionPage(ctx context.Context, conn sqlx.SqlConn, userID, taskID, before, limit int64) ([]*TaskCompletion, error) {
	rows := []*TaskCompletion{}
	query := "SELECT " + taskCompletionRows + " FROM task_completion WHERE user_id=? AND rule_version>0"
	args := []any{userID}
	if taskID > 0 {
		query += " AND task_id=?"
		args = append(args, taskID)
	}
	if before > 0 {
		query += " AND id<?"
		args = append(args, before)
	}
	query += " ORDER BY id DESC LIMIT ?"
	args = append(args, limit)
	err := conn.QueryRowsCtx(ctx, &rows, query, args...)
	return rows, err
}
func CompletionGrants(ctx context.Context, conn sqlx.SqlConn, completionID, userID int64) ([]*RewardGrant, error) {
	rows := []*RewardGrant{}
	err := conn.QueryRowsCtx(ctx, &rows, "SELECT "+rewardGrantRows+" FROM reward_grant WHERE completion_id=? AND user_id=? ORDER BY id", completionID, userID)
	return rows, err
}

type RewardTotals struct {
	Friends          int64           `db:"friends"`
	AssistanceCount  int64           `db:"assistance_count"`
	AssistancePoints decimal.Decimal `db:"assistance_points"`
	CommissionPoints decimal.Decimal `db:"commission_points"`
}

func ReadRewardTotals(ctx context.Context, conn sqlx.SqlConn, userID int64) (RewardTotals, error) {
	var result RewardTotals
	err := conn.QueryRowCtx(ctx, &result, "SELECT (SELECT COUNT(*) FROM user_referral WHERE parent_user_id=?) AS friends,COALESCE(SUM(reason='assistance'),0) AS assistance_count,COALESCE(SUM(IF(reason='assistance',points,0)),0) AS assistance_points,COALESCE(SUM(IF(reason='commission',points,0)),0) AS commission_points FROM reward_grant WHERE user_id=?", userID, userID)
	return result, err
}
