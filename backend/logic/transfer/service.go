// Package transfer owns internal USD transfers and their durable request identity.
// Callers authenticate the actor; this service rechecks current DB authority.
// HTTP endpoints, client request-key generation and notifications follow in 3.5/3.6.
package transfer

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"

	"github.com/0xabcd-k/DexWallet/backend/logic/account"
	userlogic "github.com/0xabcd-k/DexWallet/backend/logic/user"
	accountmodel "github.com/0xabcd-k/DexWallet/backend/model/account"
	model "github.com/0xabcd-k/DexWallet/backend/model/transfer"
	usermodel "github.com/0xabcd-k/DexWallet/backend/model/user"
	"github.com/0xabcd-k/DexWallet/backend/transaction"
	"github.com/go-sql-driver/mysql"
	"github.com/shopspring/decimal"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

var (
	ErrInvalidRequest       = errors.New("invalid transfer request")
	ErrRequestConflict      = errors.New("transfer request key has different parameters")
	ErrRecipientUnavailable = errors.New("transfer recipient missing or disabled")
	ErrNotFound             = errors.New("transfer not found; absence does not prove rollback")
	ErrBusy                 = errors.New("transfer lock conflict; retry with the same request key")
	ErrOutcomeUnknown       = errors.New("transfer outcome unknown; recover with the same request key")
)

const (
	maxAttempts          = 2
	mysqlDeadlock        = 1213
	mysqlLockWaitTimeout = 1205
)

// Keys are opaque, case-sensitive, unmodified client random tokens, not business idx.
var requestKeyPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{16,64}$`)

type Request struct {
	Key         string
	RecipientID int64
	Amount      decimal.Decimal
}

type Transactor interface {
	TransactCtx(context.Context, func(context.Context, sqlx.Session) error) error
}

type Service struct {
	orders   model.TransferOrderModel
	users    usermodel.UserModel
	tx       Transactor
	accounts *account.Service
}

// conn must read from the same primary as tx, without a replica or cache.
func New(conn sqlx.SqlConn, tx Transactor, accounts *account.Service) *Service {
	return &Service{model.NewTransferOrderModel(conn), usermodel.NewUserModel(conn), tx, accounts}
}

func validate(actor userlogic.Actor, r Request) error {
	if actor.UserID <= 0 || actor.AuthVersion <= 0 || !requestKeyPattern.MatchString(r.Key) || r.RecipientID <= 0 || r.RecipientID == actor.UserID || !r.Amount.IsPositive() {
		return ErrInvalidRequest
	}
	usd, _ := accountmodel.Lookup(accountmodel.KindUSD)
	if err := usd.ValidateBalances(r.Amount, decimal.Zero); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidRequest, err)
	}
	return nil
}

func checkActor(u *usermodel.User, actor userlogic.Actor) error {
	if u.Status != usermodel.StatusActive {
		return userlogic.ErrDisabled
	}
	if u.AuthVersion != actor.AuthVersion {
		return userlogic.ErrStaleSession
	}
	return nil
}

// Submit atomically commits the order, both balances, voucher and entries.
// Failed attempts leave no order/receipt; a retry may reevaluate current balances.
// Never reuse a key with changed parameters, including after an apparent failure.
func (s *Service) Submit(ctx context.Context, actor userlogic.Actor, request Request) (*model.TransferOrder, error) {
	if err := validate(actor, request); err != nil {
		return nil, err
	}
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var result *model.TransferOrder
		err := s.tx.TransactCtx(ctx, func(ctx context.Context, session sqlx.Session) error {
			var err error
			result, err = s.submit(ctx, session, actor, request)
			return err
		})
		if err == nil {
			return result, nil
		}
		var failure *transaction.Failure
		if errors.As(err, &failure) && failure.Unknown() {
			// A miss, cancelled context or read error does not prove nonexecution.
			original, lookupErr := s.FindByRequest(ctx, actor, request.Key)
			if lookupErr == nil {
				lookupErr = match(original, request)
			}
			if lookupErr == nil {
				logx.WithContext(ctx).Errorf("transfer %d recovered after indeterminate commit: %v", original.Id, err)
				return original, nil
			}
			return nil, fmt.Errorf("%w: %w", ErrOutcomeUnknown, errors.Join(err, lookupErr))
		}
		if failure != nil {
			return nil, err
		}
		if attempt+1 < maxAttempts && retryableDeadlock(err) && ctx.Err() == nil {
			logx.WithContext(ctx).Errorf("transfer sender %d retrying rolled-back deadlock: %v", actor.UserID, err)
			continue
		}
		var dbErr *mysql.MySQLError
		if errors.As(err, &dbErr) && (dbErr.Number == mysqlLockWaitTimeout || dbErr.Number == mysqlDeadlock) {
			return nil, fmt.Errorf("%w: %w", ErrBusy, err)
		}
		return nil, err
	}
	panic("unreachable transfer attempt")
}

func retryableDeadlock(err error) bool {
	var failure *transaction.Failure
	// Failed rollback, panic and indeterminate commit must never be rerun here.
	if errors.As(err, &failure) {
		return false
	}
	var dbErr *mysql.MySQLError
	return errors.As(err, &dbErr) && dbErr.Number == mysqlDeadlock
}

func (s *Service) submit(ctx context.Context, session sqlx.Session, actor userlogic.Actor, request Request) (*model.TransferOrder, error) {
	if err := transaction.RequireSession(ctx, session); err != nil {
		return nil, err
	}
	users, orders := s.users.WithSession(session), s.orders.WithSession(session)
	// Lock users before their FK inserts/account locks. Sorting also prevents
	// reciprocal transfers from acquiring user locks in opposite order.
	ids := []int64{actor.UserID, request.RecipientID}
	slices.Sort(ids)
	var recipient *usermodel.User
	for _, id := range ids {
		u, err := users.FindForUpdate(ctx, id)
		if id == request.RecipientID && errors.Is(err, usermodel.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("lock transfer user %d: %w", id, err)
		}
		if id == actor.UserID {
			if err = checkActor(u, actor); err != nil {
				return nil, err
			}
		} else {
			recipient = u
		}
	}
	// This is the transaction's first snapshot read, after the sender lock.
	// A concurrent request from the same sender has now committed or rolled back.
	original, err := orders.FindOneBySenderIdRequestKey(ctx, actor.UserID, request.Key)
	if err == nil {
		if err = match(original, request); err != nil {
			return nil, err
		}
		return original, nil
	}
	if !errors.Is(err, model.ErrNotFound) {
		return nil, err
	}
	if recipient == nil || recipient.Status != usermodel.StatusActive {
		return nil, ErrRecipientUnavailable
	}
	order := &model.TransferOrder{SenderId: actor.UserID, RecipientId: request.RecipientID, RequestKey: request.Key, Amount: request.Amount}
	inserted, err := orders.Insert(ctx, order)
	if err != nil {
		return nil, fmt.Errorf("insert transfer: %w", err)
	}
	order.Id, err = inserted.LastInsertId()
	if err != nil {
		return nil, err
	}
	voucher, err := s.accounts.Post(ctx, session, account.Posting{
		Source:  account.Source{Type: account.BusinessTransfer, ID: order.Id, Action: account.ActionPost},
		Entries: []account.Entry{account.Debit(actor.UserID, accountmodel.KindUSD, request.Amount), account.Credit(request.RecipientID, accountmodel.KindUSD, request.Amount)},
	})
	if err != nil {
		return nil, err
	}
	if err = orders.Complete(ctx, order.Id, voucher); err != nil {
		return nil, fmt.Errorf("complete transfer %d: %w", order.Id, err)
	}
	return orders.FindOne(ctx, order.Id)
}

func match(order *model.TransferOrder, request Request) error {
	if order.RecipientId != request.RecipientID || !order.Amount.Equal(request.Amount) {
		return ErrRequestConflict
	}
	return completed(order)
}

func completed(order *model.TransferOrder) error {
	if !order.VoucherId.Valid || order.VoucherId.Int64 <= 0 {
		return fmt.Errorf("%w: transfer %d has no completed voucher", ErrOutcomeUnknown, order.Id)
	}
	return nil
}

// FindByRequest reads only the authenticated sender's original order from the
// primary. ErrNotFound means not visible now, never proof that a commit failed.
func (s *Service) FindByRequest(ctx context.Context, actor userlogic.Actor, key string) (*model.TransferOrder, error) {
	if actor.UserID <= 0 || actor.AuthVersion <= 0 || !requestKeyPattern.MatchString(key) {
		return nil, ErrInvalidRequest
	}
	u, err := s.users.FindOne(ctx, actor.UserID)
	if err != nil {
		return nil, err
	}
	if err = checkActor(u, actor); err != nil {
		return nil, err
	}
	original, err := s.orders.FindOneBySenderIdRequestKey(ctx, actor.UserID, key)
	if errors.Is(err, model.ErrNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if err = completed(original); err != nil {
		return nil, err
	}
	return original, nil
}
