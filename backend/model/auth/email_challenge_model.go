package auth

import (
	"context"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

const (
	ChallengePending  = "pending"
	ChallengeActive   = "active"
	ChallengeFailed   = "failed"
	ChallengeConsumed = "consumed"
)

type EmailChallengeModel interface {
	emailChallengeModel
	WithSession(sqlx.Session) EmailChallengeModel
	Reserve(context.Context, string) error
	Lock(context.Context, string) (*EmailChallenge, error)
	SetDelivery(context.Context, string, string, string) error
}
type customEmailChallengeModel struct{ *defaultEmailChallengeModel }

func NewEmailChallengeModel(conn sqlx.SqlConn) EmailChallengeModel {
	return &customEmailChallengeModel{newEmailChallengeModel(conn)}
}
func (m *customEmailChallengeModel) WithSession(s sqlx.Session) EmailChallengeModel {
	return NewEmailChallengeModel(sqlx.NewSqlConnFromSession(s))
}
func (m *customEmailChallengeModel) Reserve(ctx context.Context, address string) error {
	_, err := m.conn.ExecCtx(ctx, "INSERT INTO email_challenge(email_address) VALUES(?) ON DUPLICATE KEY UPDATE id=LAST_INSERT_ID(id)", address)
	return err
}
func (m *customEmailChallengeModel) Lock(ctx context.Context, address string) (*EmailChallenge, error) {
	var row EmailChallenge
	err := m.conn.QueryRowCtx(ctx, &row, "SELECT "+emailChallengeRows+" FROM email_challenge WHERE email_address=? FOR UPDATE", address)
	if err != nil {
		return nil, err
	}
	return &row, nil
}
func (m *customEmailChallengeModel) SetDelivery(ctx context.Context, address, token, status string) error {
	result, err := m.conn.ExecCtx(ctx, "UPDATE email_challenge SET status=? WHERE email_address=? AND challenge_token=? AND status=?", status, address, token, ChallengePending)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrNotFound
	}
	return nil
}
