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

// FindCurrentIdentities is a display read. A concurrent binding change is visible on refresh.
func (s *Service) FindCurrentIdentities(ctx context.Context, id int64) (*Identities, error) {
	if _, err := s.FindUserByID(ctx, id); err != nil {
		return nil, err
	}
	owner := sql.NullInt64{Int64: id, Valid: true}
	email, err := s.emails.FindOneByUserId(ctx, owner)
	if err != nil && !errors.Is(err, model.ErrNotFound) {
		return nil, err
	}
	tg, err := s.telegrams.FindOneByUserId(ctx, owner)
	if err != nil && !errors.Is(err, model.ErrNotFound) {
		return nil, err
	}
	return &Identities{Email: email, Telegram: tg}, nil
}

func retryableResolution(err error) bool {
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
