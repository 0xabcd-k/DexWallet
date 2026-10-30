package backoffice

import (
	"context"
	"database/sql"
	"strconv"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/apperror"
	model "github.com/0xabcd-k/DexWallet/backend/model/backoffice"
	"github.com/0xabcd-k/DexWallet/backend/readcache"
	"github.com/0xabcd-k/DexWallet/backend/transaction"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
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

type Account struct {
	Id                 int64     `db:"id"`
	Email              string    `db:"email"`
	PasswordHash       string    `db:"password_hash"`
	Status             string    `db:"status"`
	AuthVersion        int64     `db:"auth_version"`
	MustChangePassword int64     `db:"must_change_password"`
	CreatedAt          time.Time `db:"created_at"`
	UpdatedAt          time.Time `db:"updated_at"`
}

func (u *Account) admin() *model.AdminUser {
	return &model.AdminUser{Id: u.Id, Email: u.Email, PasswordHash: u.PasswordHash, Status: u.Status, AuthVersion: u.AuthVersion, MustChangePassword: u.MustChangePassword, CreatedAt: u.CreatedAt, UpdatedAt: u.UpdatedAt}
}
func (u *Account) merchant() *model.MerchantUser {
	return &model.MerchantUser{Id: u.Id, Email: u.Email, PasswordHash: u.PasswordHash, Status: u.Status, AuthVersion: u.AuthVersion, MustChangePassword: u.MustChangePassword, CreatedAt: u.CreatedAt, UpdatedAt: u.UpdatedAt}
}

const accountColumns = "id,email,password_hash,status,auth_version,must_change_password,created_at,updated_at"

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
	cache  *readcache.Cache[Account]
	prefix string
}

func (m *store) session(tx sqlx.Session) *store {
	return &store{conn: sqlx.NewSqlConnFromSession(tx), cache: m.cache, prefix: m.prefix}
}
func (m *store) key(realm Realm, id int64) string {
	return m.prefix + string(realm) + ":" + strconv.FormatInt(id, 10)
}
func (m *store) find(ctx context.Context, realm Realm, id int64, lock bool) (*Account, error) {
	name, err := table(realm)
	if err != nil {
		return nil, err
	}
	q := "SELECT " + accountColumns + " FROM " + name + " WHERE id=?"
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
	q := "SELECT " + accountColumns + " FROM " + name + " WHERE email=?"
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
		result, err = model.NewAdminUserModel(m.conn).Insert(ctx, u.admin())
	case Merchant:
		result, err = model.NewMerchantUserModel(m.conn).Insert(ctx, u.merchant())
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
		m.cache.Delete(ctx, m.key(realm, u.Id))
	}); err != nil {
		return err
	}
	switch realm {
	case Admin:
		return model.NewAdminUserModel(m.conn).Update(ctx, u.admin())
	case Merchant:
		return model.NewMerchantUserModel(m.conn).Update(ctx, u.merchant())
	}
	return apperror.New(apperror.InvalidArgument, nil)
}

// A small administrator set is locked in ID order before any administrator
// status operation. This serializes the last-active-admin invariant.
func (m *store) lockAdmins(ctx context.Context) ([]Account, error) {
	rows := []Account{}
	err := m.conn.QueryRowsCtx(ctx, &rows, "SELECT "+accountColumns+" FROM admin_user ORDER BY id FOR UPDATE")
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
	err = m.conn.QueryRowsCtx(ctx, &rows, "SELECT "+accountColumns+" FROM "+name+" ORDER BY id DESC LIMIT ? OFFSET ?", PageSize, (page-1)*PageSize)
	return rows, err
}
func (m *store) cached(ctx context.Context, r Realm, id int64) (*Account, error) {
	if _, err := table(r); err != nil {
		return nil, err
	}
	return m.cache.Get(ctx, m.key(r, id), func() (*Account, error) {
		u, err := m.find(ctx, r, id, false)
		if err == nil {
			u.PasswordHash = ""
		}
		return u, err
	})
}
