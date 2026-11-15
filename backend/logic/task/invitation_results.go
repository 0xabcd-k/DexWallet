package task

import (
	"context"
	"github.com/0xabcd-k/DexWallet/backend/apperror"
	users "github.com/0xabcd-k/DexWallet/backend/logic/user"
	"github.com/shopspring/decimal"
	"time"
)

const EarningsAll = "all"

type Friend struct {
	UserID   int64     `db:"user_id"`
	Nickname string    `db:"nickname"`
	BoundAt  time.Time `db:"bound_at"`
}
type Earning struct {
	ID           int64           `db:"id"`
	PeerID       int64           `db:"peer_id"`
	PeerNickname string          `db:"peer_nickname"`
	Reason       string          `db:"reason"`
	Points       decimal.Decimal `db:"points"`
	TaskTitle    string          `db:"task_title"`
	CreatedAt    time.Time       `db:"created_at"`
}
type EarningsFilter struct {
	Before, Limit, FriendID int64
	Reason, From, To        string
}

func (s *Service) Friends(ctx context.Context, a users.Actor, before, limit int64) ([]Friend, int64, error) {
	if err := validPage(before, limit); err != nil {
		return nil, 0, err
	}
	if _, err := s.snapshot(ctx, a); err != nil {
		return nil, 0, err
	}
	rows := []Friend{}
	err := s.conn.QueryRowsCtx(ctx, &rows, "SELECT r.user_id,u.nickname,r.created_at AS bound_at FROM user_referral r JOIN user u ON u.id=r.user_id WHERE r.parent_user_id=? AND (?=0 OR r.user_id<?) ORDER BY r.user_id DESC LIMIT ?", a.UserID, before, before, limit+1)
	var next int64
	if int64(len(rows)) > limit {
		rows = rows[:limit]
		next = rows[len(rows)-1].UserID
	}
	return rows, next, err
}
func (s *Service) Earnings(ctx context.Context, a users.Actor, f EarningsFilter) ([]Earning, int64, error) {
	if err := validPage(f.Before, f.Limit); err != nil {
		return nil, 0, err
	}
	if f.FriendID < 0 || (f.Reason != EarningsAll && f.Reason != RewardAssistance && f.Reason != RewardCommission) {
		return nil, 0, apperror.New(apperror.InvalidArgument, nil)
	}
	var from, to time.Time
	var err error
	if f.From != "" {
		from, err = time.Parse(time.DateOnly, f.From)
		if err != nil {
			return nil, 0, apperror.New(apperror.InvalidArgument, err)
		}
	}
	if f.To != "" {
		to, err = time.Parse(time.DateOnly, f.To)
		if err != nil || (!from.IsZero() && to.Before(from)) {
			return nil, 0, apperror.New(apperror.InvalidArgument, err)
		}
		to = to.AddDate(0, 0, 1)
	}
	if _, err = s.snapshot(ctx, a); err != nil {
		return nil, 0, err
	}
	// A commission follows the task income beneficiary. In help mode this is
	// the inviter, whereas the helper is the source of an assistance reward.
	peer := "CASE WHEN g.reason=? OR c.mode=? THEN c.user_id ELSE i.inviter_user_id END"
	q := "SELECT g.id,p.id AS peer_id,p.nickname AS peer_nickname,g.reason,g.points,c.task_title,g.created_at FROM reward_grant g JOIN task_completion c ON c.id=g.completion_id LEFT JOIN task_invitation i ON i.id=c.invitation_id JOIN user p ON p.id=(" + peer + ") WHERE g.user_id=? AND g.reason IN (?,?)"
	args := []any{RewardAssistance, ClaimSelf, a.UserID, RewardAssistance, RewardCommission}
	if f.Before > 0 {
		q += " AND g.id<?"
		args = append(args, f.Before)
	}
	if f.FriendID > 0 {
		q += " AND p.id=?"
		args = append(args, f.FriendID)
	}
	if f.Reason != EarningsAll {
		q += " AND g.reason=?"
		args = append(args, f.Reason)
	}
	if !from.IsZero() {
		q += " AND g.created_at>=?"
		args = append(args, from)
	}
	if !to.IsZero() {
		q += " AND g.created_at<?"
		args = append(args, to)
	}
	q += " ORDER BY g.id DESC LIMIT ?"
	args = append(args, f.Limit+1)
	rows := []Earning{}
	err = s.conn.QueryRowsCtx(ctx, &rows, q, args...)
	var next int64
	if int64(len(rows)) > f.Limit {
		rows = rows[:f.Limit]
		next = rows[len(rows)-1].ID
	}
	return rows, next, err
}
