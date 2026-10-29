// Package user implements trusted internal identity resolution. No entry here
// authenticates a client, issues a session or bypasses an identity's current owner.
package user

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	model "github.com/0xabcd-k/DexWallet/backend/model/user"
	"github.com/0xabcd-k/DexWallet/backend/transaction"
	"github.com/go-sql-driver/mysql"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

var (
	ErrVersionExhausted = errors.New("identity binding version exhausted")
	ErrInvalidInput     = errors.New("invalid user input")
	ErrDisabled         = errors.New("user disabled")
	ErrIdentityUnbound  = errors.New("identity unbound")
	ErrBindingChanged   = errors.New("identity binding changed")
	ErrConflict         = errors.New("identity changed repeatedly; retry authentication")
	errResolveRace      = errors.New("identity resolution raced")
)

const resolveAttempts = 3
const mysqlDuplicateKey = 1062
const mysqlDeadlock = 1213

type Transactor interface {
	TransactCtx(context.Context, func(context.Context, sqlx.Session) error) error
}
type Service struct {
	users     model.UserModel
	emails    model.EmailIdentityModel
	telegrams model.TelegramIdentityModel
	tx        Transactor
}

func New(users model.UserModel, emails model.EmailIdentityModel, telegrams model.TelegramIdentityModel, tx Transactor) *Service {
	return &Service{users: users, emails: emails, telegrams: telegrams, tx: tx}
}

type Binding struct {
	Type                        model.IdentityType
	IdentityID, UserID, Version int64
}
type Resolved struct {
	User    *model.User
	Binding Binding
}
type Identities struct {
	Email    *model.EmailIdentity
	Telegram *model.TelegramIdentity
}

func (s *Service) FindUserByID(ctx context.Context, id int64) (*model.User, error) {
	if id <= 0 {
		return nil, ErrInvalidInput
	}
	return s.users.FindOne(ctx, id)
}

// FindCurrentIdentities is a coherent read under the user lock. Absence is nil;
// database failures and a missing user remain errors.
func (s *Service) FindCurrentIdentities(ctx context.Context, id int64) (*Identities, error) {
	if id <= 0 {
		return nil, ErrInvalidInput
	}
	var result Identities
	err := s.tx.TransactCtx(ctx, func(ctx context.Context, session sqlx.Session) error {
		if _, err := s.users.WithSession(session).FindForUpdate(ctx, id); err != nil {
			return err
		}
		owner := sql.NullInt64{Int64: id, Valid: true}
		var err error
		result.Email, err = s.emails.WithSession(session).FindOneByUserId(ctx, owner)
		if err != nil && !errors.Is(err, model.ErrNotFound) {
			return err
		}
		result.Telegram, err = s.telegrams.WithSession(session).FindOneByUserId(ctx, owner)
		if err != nil && !errors.Is(err, model.ErrNotFound) {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// CheckCurrentBindingInSession keeps locks until the outer transaction finishes.
// Multi-user callers must lock the complete user set in ascending ID order first.
func (s *Service) CheckCurrentBindingInSession(ctx context.Context, session sqlx.Session, b Binding) error {
	if session == nil || b.UserID <= 0 || b.IdentityID <= 0 || b.Version <= 0 || (b.Type != model.IdentityEmail && b.Type != model.IdentityTelegram) {
		return ErrInvalidInput
	}
	u, err := s.users.WithSession(session).FindForUpdate(ctx, b.UserID)
	if err != nil {
		return err
	}
	if u.Status != model.StatusActive {
		return ErrDisabled
	}
	var owner sql.NullInt64
	var version int64
	switch b.Type {
	case model.IdentityEmail:
		identity, e := s.emails.WithSession(session).FindForUpdate(ctx, b.IdentityID)
		if e != nil {
			return e
		}
		owner, version = identity.UserId, identity.BindingVersion
	case model.IdentityTelegram:
		identity, e := s.telegrams.WithSession(session).FindForUpdate(ctx, b.IdentityID)
		if e != nil {
			return e
		}
		owner, version = identity.UserId, identity.BindingVersion
	}
	if !owner.Valid {
		return ErrIdentityUnbound
	}
	if owner.Int64 != b.UserID || version != b.Version {
		return ErrBindingChanged
	}
	return nil
}

func RetryableResolution(err error) bool {
	var failure *transaction.Failure
	if errors.As(err, &failure) {
		return false
	} // includes unknown commit/failed cleanup
	if errors.Is(err, errResolveRace) {
		return true
	}
	var mysqlError *mysql.MySQLError
	return errors.As(err, &mysqlError) && mysqlError.Number == mysqlDeadlock
}
func insertIdentityError(err error) error {
	var mysqlError *mysql.MySQLError
	// Insert callers create an unbound row, so only its unique subject can collide.
	if errors.As(err, &mysqlError) && mysqlError.Number == mysqlDuplicateKey {
		return fmt.Errorf("%w: %w", errResolveRace, err)
	}
	return err
}
func createUser(ctx context.Context, users model.UserModel, nickname, avatar string) (*model.User, error) {
	u := &model.User{Status: model.StatusActive, AuthVersion: model.InitialAuthVersion, Nickname: nickname, AvatarUrl: avatar}
	result, err := users.Insert(ctx, u)
	if err != nil {
		return nil, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}
	return users.FindOne(ctx, id)
}
