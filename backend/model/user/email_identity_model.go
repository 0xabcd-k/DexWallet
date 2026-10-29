package user

import (
	"context"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

type EmailIdentityModel interface {
	emailIdentityModel
	WithSession(sqlx.Session) EmailIdentityModel
	FindForUpdate(context.Context, int64) (*EmailIdentity, error)
	FindBySubjectForUpdate(ctx context.Context, address string) (*EmailIdentity, error)
}
type customEmailIdentityModel struct{ *defaultEmailIdentityModel }

func NewEmailIdentityModel(conn sqlx.SqlConn) EmailIdentityModel {
	return &customEmailIdentityModel{newEmailIdentityModel(conn)}
}

// WithSession never starts or commits a transaction.
func (m *customEmailIdentityModel) WithSession(session sqlx.Session) EmailIdentityModel {
	return NewEmailIdentityModel(sqlx.NewSqlConnFromSession(session))
}

// FindForUpdate requires WithSession and the caller's user-before-identity lock order.
func (m *customEmailIdentityModel) FindForUpdate(ctx context.Context, id int64) (*EmailIdentity, error) {
	var result EmailIdentity
	err := m.conn.QueryRowCtx(ctx, &result, "select "+emailIdentityRows+" from "+m.table+" where `id` = ? for update", id)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (m *customEmailIdentityModel) FindBySubjectForUpdate(ctx context.Context, address string) (*EmailIdentity, error) {
	var result EmailIdentity
	err := m.conn.QueryRowCtx(ctx, &result, "select "+emailIdentityRows+" from "+m.table+" where `email_address` = ? for update", address)
	if err != nil {
		return nil, err
	}
	return &result, nil
}
