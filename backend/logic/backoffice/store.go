package backoffice

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/apperror"
	model "github.com/0xabcd-k/DexWallet/backend/model/backoffice"
	usermodel "github.com/0xabcd-k/DexWallet/backend/model/user"
	"github.com/0xabcd-k/DexWallet/backend/transaction"
	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
	"github.com/zeromicro/go-zero/core/syncx"
)

type Realm string

const (
	Admin              Realm = "admin"
	Merchant           Realm = "merchant"
	Product            Realm = "user"
	Super              Realm = "super"
	PageSize                 = 50
	accountCachePrefix       = ":backoffice:account:"
)

type Account = model.AdminUser

func table(realm Realm) (string, error) {
	switch realm {
	case Admin:
		return "admin_user", nil
	case Merchant:
		return "merchant_user", nil
	}
	return "", apperror.New(apperror.InvalidArgument, nil)
}

type store struct {
	conn   sqlx.SqlConn
	redis  *redis.Client
	prefix string
	flight syncx.SingleFlight
}

func (m *store) session(tx sqlx.Session) *store {
	return &store{conn: sqlx.NewSqlConnFromSession(tx), redis: m.redis, prefix: m.prefix, flight: m.flight}
}
func (m *store) key(realm Realm, id int64) string {
	return m.prefix + string(realm) + ":" + strconv.FormatInt(id, 10)
}
func (m *store) find(ctx context.Context, realm Realm, id int64, lock bool) (*Account, error) {
	name, err := table(realm)
	if err != nil {
		return nil, err
	}
	q := "SELECT * FROM " + name + " WHERE id=?"
	if lock {
		q += " FOR UPDATE"
	}
	var u Account
	err = m.conn.QueryRowCtx(ctx, &u, q, id)
	return &u, err
}
func (m *store) email(ctx context.Context, realm Realm, email string, lock bool) (*Account, error) {
	name, err := table(realm)
	if err != nil {
		return nil, err
	}
	q := "SELECT * FROM " + name + " WHERE email=?"
	if lock {
		q += " FOR UPDATE"
	}
	var u Account
	err = m.conn.QueryRowCtx(ctx, &u, q, email)
	return &u, err
}
func (m *store) insert(ctx context.Context, realm Realm, u *Account) error {
	var result sql.Result
	var err error
	switch realm {
	case Admin:
		result, err = model.NewAdminUserModel(m.conn).Insert(ctx, u)
	case Merchant:
		result, err = model.NewMerchantUserModel(m.conn).Insert(ctx, (*model.MerchantUser)(u))
	default:
		return apperror.New(apperror.InvalidArgument, nil)
	}
	if err != nil {
		return err
	}
	u.Id, err = result.LastInsertId()
	return err
}
func (m *store) update(ctx context.Context, realm Realm, u *Account) error {
	if err := transaction.AfterCommit(ctx, m.key(realm, u.Id), func() {
		call, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer cancel()
		if err := m.redis.Del(call, m.key(realm, u.Id)).Err(); err != nil {
			logx.WithContext(ctx).Errorw("backoffice cache invalidation failed", logx.Field("realm", realm), logx.Field("id", u.Id), logx.Field("cause", err))
		}
	}); err != nil {
		return err
	}
	switch realm {
	case Admin:
		return model.NewAdminUserModel(m.conn).Update(ctx, u)
	case Merchant:
		return model.NewMerchantUserModel(m.conn).Update(ctx, (*model.MerchantUser)(u))
	}
	return apperror.New(apperror.InvalidArgument, nil)
}

// A small administrator set is locked in ID order before any administrator
// state/create/reset operation. This serializes the last-active-admin invariant.
func (m *store) lockAdmins(ctx context.Context) ([]Account, error) {
	rows := []Account{}
	err := m.conn.QueryRowsCtx(ctx, &rows, "SELECT * FROM admin_user ORDER BY id FOR UPDATE")
	return rows, err
}
func (m *store) list(ctx context.Context, realm Realm, page int64) ([]Account, error) {
	name, err := table(realm)
	if err != nil {
		return nil, err
	}
	if page < 1 || page > 1000000 {
		return nil, apperror.New(apperror.InvalidArgument, nil)
	}
	rows := []Account{}
	err = m.conn.QueryRowsCtx(ctx, &rows, "SELECT * FROM "+name+" ORDER BY id DESC LIMIT ? OFFSET ?", PageSize, (page-1)*PageSize)
	return rows, err
}
func (m *store) cached(ctx context.Context, realm Realm, id int64) (*Account, error) {
	if _, err := table(realm); err != nil {
		return nil, err
	}
	type entry struct {
		Account Account
		Expires time.Time
	}
	value, err := m.flight.Do(m.key(realm, id), func() (any, error) {
		raw, err := m.redis.Get(ctx, m.key(realm, id)).Bytes()
		var e entry
		if err != nil && !errors.Is(err, redis.Nil) {
			return nil, fmt.Errorf("%w: %w", usermodel.ErrCacheUnavailable, err)
		}
		if err == nil {
			if err = json.Unmarshal(raw, &e); err != nil || e.Account.Id != id || e.Expires.IsZero() {
				return nil, usermodel.ErrCacheUnavailable
			}
			if time.Now().Before(e.Expires) {
				return e, nil
			}
		}
		expiry := time.Now().Add(usermodel.UserCacheTTL)
		u, err := m.find(ctx, realm, id, false)
		if err != nil {
			return nil, err
		}
		u.PasswordHash = ""
		e = entry{Account: *u, Expires: expiry}
		raw, err = json.Marshal(e)
		if err != nil {
			return nil, err
		}
		ttl := time.Until(expiry)
		if ttl <= 0 {
			return nil, usermodel.ErrCacheUnavailable
		}
		if err = m.redis.Set(ctx, m.key(realm, id), raw, ttl).Err(); err != nil {
			return nil, fmt.Errorf("%w: %w", usermodel.ErrCacheUnavailable, err)
		}
		return e, nil
	})
	if err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	e := value.(entry)
	if !time.Now().Before(e.Expires) {
		return nil, usermodel.ErrCacheUnavailable
	}
	return &e.Account, nil
}
