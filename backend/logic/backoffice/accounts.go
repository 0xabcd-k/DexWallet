package backoffice

import (
	"context"
	"errors"
	"strings"

	"github.com/0xabcd-k/DexWallet/backend/apperror"
	userlogic "github.com/0xabcd-k/DexWallet/backend/logic/user"
	usermodel "github.com/0xabcd-k/DexWallet/backend/model/user"
	"github.com/go-sql-driver/mysql"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
	"golang.org/x/crypto/bcrypt"
)

func emailAddress(raw string) (string, error) {
	v, err := userlogic.NormalizeEmail(raw)
	if err != nil {
		return "", err
	}
	if len(v.Address) > 254 {
		return "", apperror.New(apperror.InvalidArgument, nil)
	}
	return v.Address, nil
}
func (s *Service) Create(ctx context.Context, a Actor, r Realm, email, password, reason string) (*Account, error) {
	if !reasonOK(reason) {
		return nil, apperror.New(apperror.InvalidArgument, nil)
	}
	if r != Merchant && !(r == Admin && a.Realm == Super) {
		return nil, apperror.New(apperror.PermissionDenied, nil)
	}
	email, err := emailAddress(email)
	if err != nil {
		return nil, err
	}
	hash, err := passwordHash(password)
	if err != nil {
		return nil, err
	}
	u := &Account{Email: email, PasswordHash: hash, Status: usermodel.StatusActive, AuthVersion: 1, MustChangePassword: 1}
	err = s.tx.TransactCtx(ctx, func(ctx context.Context, tx sqlx.Session) error {
		m := s.store.session(tx)
		if r == Admin {
			if _, err := m.lockAdmins(ctx); err != nil {
				return err
			}
		}
		if _, err := s.check(ctx, m, a, true); err != nil {
			return err
		}
		if err := m.insert(ctx, r, u); err != nil {
			var duplicate *mysql.MySQLError
			if errors.As(err, &duplicate) && duplicate.Number == 1062 {
				return apperror.New(apperror.Conflict, err)
			}
			return err
		}
		return s.audit(ctx, m, a, ActionCreate, r, u, "", usermodel.StatusActive, reason, ResultSuccess)
	})
	if err != nil {
		return nil, s.failure(ctx, a, ActionCreate, r, &Account{Email: email}, reason, err)
	}
	return u, nil
}
func (s *Service) ChangePassword(ctx context.Context, a Actor, old, password string) (*Account, error) {
	hash, err := passwordHash(password)
	if err != nil {
		return nil, err
	}
	var u *Account
	err = s.tx.TransactCtx(ctx, func(ctx context.Context, tx sqlx.Session) error {
		m := s.store.session(tx)
		if a.Realm == Admin {
			if _, err := m.lockAdmins(ctx); err != nil {
				return err
			}
		}
		var err error
		u, err = s.check(ctx, m, a, false)
		if err != nil {
			return err
		}
		if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(old)) != nil {
			return apperror.New(apperror.AuthProofInvalid, nil)
		}
		if old == password {
			return apperror.New(apperror.InvalidArgument, nil)
		}
		if err = bump(u); err != nil {
			return err
		}
		u.PasswordHash = hash
		u.MustChangePassword = 0
		if err = m.update(ctx, a.Realm, u); err != nil {
			return err
		}
		return s.audit(ctx, m, a, ActionPassword, a.Realm, u, "", "", "self", ResultSuccess)
	})
	if err != nil {
		return nil, s.failure(ctx, a, ActionPassword, a.Realm, u, "self", err)
	}
	return u, nil
}
func (s *Service) SetStatus(ctx context.Context, a Actor, r Realm, idx, status, reason string) (*Account, error) {
	return s.manage(ctx, a, r, idx, status, "", reason)
}
func (s *Service) ResetPassword(ctx context.Context, a Actor, r Realm, idx, password, reason string) (*Account, error) {
	hash, err := passwordHash(password)
	if err != nil {
		return nil, err
	}
	return s.manage(ctx, a, r, idx, "", hash, reason)
}
func (s *Service) manage(ctx context.Context, a Actor, r Realm, idx, status, hash, reason string) (*Account, error) {
	if !reasonOK(reason) || (hash == "" && status != usermodel.StatusActive && status != usermodel.StatusDisabled) {
		return nil, apperror.New(apperror.InvalidArgument, nil)
	}
	id, err := s.Decode(r, idx)
	if err != nil {
		return nil, apperror.New(apperror.InvalidArgument, err)
	}
	if r != Admin && r != Merchant {
		return nil, apperror.New(apperror.InvalidArgument, nil)
	}
	action := ActionStatus
	if hash != "" {
		action = ActionReset
	}
	var u *Account
	err = s.tx.TransactCtx(ctx, func(ctx context.Context, tx sqlx.Session) error {
		m := s.store.session(tx)
		var admins []Account
		var err error
		if r == Admin {
			admins, err = m.lockAdmins(ctx)
			if err != nil {
				return err
			}
		}
		if _, err = s.check(ctx, m, a, true); err != nil {
			return err
		}
		u, err = m.find(ctx, r, id, true)
		if errors.Is(err, sqlx.ErrNotFound) {
			return apperror.New(apperror.NotFound, err)
		}
		if err != nil {
			return err
		}
		before, after := u.Status, status
		if hash != "" {
			before, after = "", ""
			if err = bump(u); err != nil {
				return err
			}
			u.PasswordHash = hash
			u.MustChangePassword = 1
		} else if u.Status != status {
			if r == Admin && status == usermodel.StatusDisabled {
				active := 0
				for _, row := range admins {
					if row.Status == usermodel.StatusActive {
						active++
					}
				}
				if active <= 1 {
					return apperror.New(apperror.Conflict, errors.New("last active administrator cannot be disabled"))
				}
			}
			if err = bump(u); err != nil {
				return err
			}
			u.Status = status
		} else {
			return s.audit(ctx, m, a, action, r, u, before, after, reason, ResultSuccess)
		}
		if err = m.update(ctx, r, u); err != nil {
			return err
		}
		return s.audit(ctx, m, a, action, r, u, before, after, reason, ResultSuccess)
	})
	if err != nil {
		return nil, s.failure(ctx, a, action, r, &Account{Id: id}, reason, err)
	}
	return u, nil
}
func (s *Service) ProductUser(ctx context.Context, a Actor, idx string) (*usermodel.User, error) {
	id, err := s.UserIdx.Decode(idx)
	if err != nil {
		return nil, apperror.New(apperror.InvalidArgument, err)
	}
	var u *usermodel.User
	err = s.tx.TransactCtx(ctx, func(ctx context.Context, tx sqlx.Session) error {
		if _, err := s.check(ctx, s.store.session(tx), a, true); err != nil {
			return err
		}
		var err error
		u, err = s.users.WithSession(tx).FindOne(ctx, id)
		return err
	})
	if errors.Is(err, sqlx.ErrNotFound) {
		err = apperror.New(apperror.NotFound, err)
	}
	return u, err
}
func (s *Service) SetProductStatus(ctx context.Context, a Actor, idx, status, reason string) (*usermodel.User, error) {
	if !reasonOK(reason) || (status != usermodel.StatusActive && status != usermodel.StatusDisabled) {
		return nil, apperror.New(apperror.InvalidArgument, nil)
	}
	id, err := s.UserIdx.Decode(strings.TrimSpace(idx))
	if err != nil {
		return nil, apperror.New(apperror.InvalidArgument, err)
	}
	var u *usermodel.User
	err = s.tx.TransactCtx(ctx, func(ctx context.Context, tx sqlx.Session) error {
		m := s.store.session(tx)
		if _, err := s.check(ctx, m, a, true); err != nil {
			return err
		}
		users := s.users.WithSession(tx)
		var err error
		u, err = users.FindForUpdate(ctx, id)
		if errors.Is(err, sqlx.ErrNotFound) {
			return apperror.New(apperror.NotFound, err)
		}
		if err != nil {
			return err
		}
		before := u.Status
		if u.Status != status {
			version := &Account{AuthVersion: u.AuthVersion}
			if err = bump(version); err != nil {
				return err
			}
			u.AuthVersion = version.AuthVersion
			u.Status = status
			if err = users.Update(ctx, u); err != nil {
				return err
			}
		}
		return s.audit(ctx, m, a, ActionStatus, Product, &Account{Id: id}, before, status, reason, ResultSuccess)
	})
	if err != nil {
		return nil, s.failure(ctx, a, ActionStatus, Product, &Account{Id: id}, reason, err)
	}
	return u, nil
}
