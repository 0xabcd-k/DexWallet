package user

import (
	"context"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

type TelegramIdentityModel interface {
	telegramIdentityModel
	WithSession(sqlx.Session) TelegramIdentityModel
	FindForUpdate(context.Context, int64) (*TelegramIdentity, error)
	FindBySubjectForUpdate(ctx context.Context, telegramID int64) (*TelegramIdentity, error)
}
type customTelegramIdentityModel struct{ *defaultTelegramIdentityModel }

func NewTelegramIdentityModel(conn sqlx.SqlConn) TelegramIdentityModel {
	return &customTelegramIdentityModel{newTelegramIdentityModel(conn)}
}

// WithSession never starts or commits a transaction.
func (m *customTelegramIdentityModel) WithSession(session sqlx.Session) TelegramIdentityModel {
	return NewTelegramIdentityModel(sqlx.NewSqlConnFromSession(session))
}

// FindForUpdate requires WithSession and the caller's user-before-identity lock order.
func (m *customTelegramIdentityModel) FindForUpdate(ctx context.Context, id int64) (*TelegramIdentity, error) {
	var result TelegramIdentity
	err := m.conn.QueryRowCtx(ctx, &result, "select "+telegramIdentityRows+" from "+m.table+" where `id` = ? for update", id)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (m *customTelegramIdentityModel) FindBySubjectForUpdate(ctx context.Context, telegramID int64) (*TelegramIdentity, error) {
	var result TelegramIdentity
	err := m.conn.QueryRowCtx(ctx, &result, "select "+telegramIdentityRows+" from "+m.table+" where `telegram_user_id` = ? for update", telegramID)
	if err != nil {
		return nil, err
	}
	return &result, nil
}
