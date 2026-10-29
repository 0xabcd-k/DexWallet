package user

import (
	"context"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

type UserModel interface {
	userModel
	WithSession(sqlx.Session) UserModel
	FindForUpdate(context.Context, int64) (*User, error)
}
type customUserModel struct{ *defaultUserModel }

func NewUserModel(conn sqlx.SqlConn) UserModel {
	return &customUserModel{newUserModel(conn)}
}

// WithSession never starts or commits a transaction.
func (m *customUserModel) WithSession(session sqlx.Session) UserModel {
	return NewUserModel(sqlx.NewSqlConnFromSession(session))
}

// FindForUpdate requires WithSession and the caller's user-before-identity lock order.
func (m *customUserModel) FindForUpdate(ctx context.Context, id int64) (*User, error) {
	var result User
	err := m.conn.QueryRowCtx(ctx, &result, "select "+userRows+" from "+m.table+" where `id` = ? for update", id)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// 产品账户状态，外部身份缺项不改变账户状态。
const (
	StatusActive             = "active"
	StatusDisabled           = "disabled"
	InitialAuthVersion int64 = 1
)

type IdentityType string

const (
	IdentityEmail    IdentityType = "email"
	IdentityTelegram IdentityType = "telegram"
)
