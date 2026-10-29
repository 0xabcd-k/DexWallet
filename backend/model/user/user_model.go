package user

import (
	"context"
	"github.com/0xabcd-k/DexWallet/backend/transaction"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

type UserModel interface {
	userModel
	WithSession(sqlx.Session) UserModel
	FindForUpdate(context.Context, int64) (*User, error)
}
type customUserModel struct {
	*defaultUserModel
	cache         *UserCache
	inTransaction bool
}

func NewUserModel(conn sqlx.SqlConn) UserModel {
	return &customUserModel{defaultUserModel: newUserModel(conn)}
}

func NewUserModelWithCache(conn sqlx.SqlConn, cache *UserCache) UserModel {
	return &customUserModel{defaultUserModel: newUserModel(conn), cache: cache}
}

// WithSession never starts or commits a transaction.
func (m *customUserModel) WithSession(session sqlx.Session) UserModel {
	return &customUserModel{defaultUserModel: newUserModel(sqlx.NewSqlConnFromSession(session)), cache: m.cache, inTransaction: true}
}

func (m *customUserModel) FindOne(ctx context.Context, id int64) (*User, error) {
	if m.cache == nil || m.inTransaction {
		return m.defaultUserModel.FindOne(ctx, id)
	}
	return m.cache.find(ctx, id, m.defaultUserModel.FindOne)
}

func (m *customUserModel) Update(ctx context.Context, data *User) error {
	after, err := m.prepareInvalidation(ctx, data.Id)
	if err != nil {
		return err
	}
	if err = m.defaultUserModel.Update(ctx, data); err != nil {
		return err
	}
	if after != nil {
		after()
	}
	return nil
}

func (m *customUserModel) Delete(ctx context.Context, id int64) error {
	after, err := m.prepareInvalidation(ctx, id)
	if err != nil {
		return err
	}
	if err = m.defaultUserModel.Delete(ctx, id); err != nil {
		return err
	}
	if after != nil {
		after()
	}
	return nil
}

func (m *customUserModel) prepareInvalidation(ctx context.Context, id int64) (func(), error) {
	if m.cache == nil {
		return nil, nil
	}
	after := func() { m.cache.invalidate(ctx, id) }
	if m.inTransaction {
		// Register before writing, so a missing outer transaction context fails
		// without changing the DB. Deduplicate repeated writes to the same user.
		return nil, transaction.AfterCommit(ctx, m.cache.Key(id), after)
	}
	return after, nil
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
