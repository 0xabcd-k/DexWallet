package account

import (
	"context"
	"errors"
	"fmt"

	"github.com/0xabcd-k/DexWallet/backend/apperror"
	model "github.com/0xabcd-k/DexWallet/backend/model/account"
	"github.com/0xabcd-k/DexWallet/backend/transaction"
	driver "github.com/go-sql-driver/mysql"
	"github.com/shopspring/decimal"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

const (
	ActionHold          Action = "hold"
	ActionConsume       Action = "consume"
	ActionRelease       Action = "release"
	DefaultHoldPageSize        = 20
	MaxHoldPageSize            = 100
)

var (
	ErrInvalidHold  = errors.New("invalid account hold")
	ErrAlreadyHeld  = errors.New("business already has a hold")
	ErrHoldNotFound = errors.New("account hold not found")
	ErrHoldResolved = errors.New("account hold already resolved")
	ErrAccountBusy  = errors.New("account write lock conflict")
)

const mysqlLockWaitTimeout = 1205
const mysqlDeadlock = 1213

// Preserve the database cause. The caller must roll back, not swallow this error
// and commit its other changes. Only the business can decide whether to retry.
func classifyWriteError(err error) error {
	var dbErr *driver.MySQLError
	if !errors.Is(err, ErrAccountBusy) && errors.As(err, &dbErr) && (dbErr.Number == mysqlLockWaitTimeout || dbErr.Number == mysqlDeadlock) {
		return fmt.Errorf("%w: %w", ErrAccountBusy, err)
	}
	return err
}

func holdSource(t BusinessType) bool { return t == BusinessWithdrawal || t == BusinessPayment }

type HoldRequest struct {
	UserID       int64
	BusinessType BusinessType
	BusinessID   int64
	Amount       decimal.Decimal
}

// Hold reserves the full amount of a business's USD component. The caller owns
// the real business record and authorization. No public mutation API calls it.
func (s *Service) Hold(ctx context.Context, session sqlx.Session, request HoldRequest) (id int64, err error) {
	defer func() { err = classifyWriteError(err) }()
	if err = transaction.RequireSession(ctx, session); err != nil {
		return 0, err
	}
	definition, _ := model.Lookup(model.KindUSD)
	if request.UserID <= 0 || request.BusinessID <= 0 || !holdSource(request.BusinessType) || !request.Amount.IsPositive() {
		return 0, ErrInvalidHold
	}
	if err = definition.ValidateBalances(request.Amount, decimal.Zero); err != nil {
		return 0, fmt.Errorf("%w: %w", ErrInvalidHold, err)
	}
	credit := Credit(request.UserID, model.KindUSD, request.Amount)
	credit.part = model.BalanceHeld
	voucher, err := s.post(ctx, session, Posting{
		Source:  Source{request.BusinessType, request.BusinessID, ActionHold},
		Entries: []Entry{Debit(request.UserID, model.KindUSD, request.Amount), credit},
	})
	if errors.Is(err, ErrAlreadyPosted) {
		return 0, fmt.Errorf("%w: %w", ErrAlreadyHeld, err)
	}
	if err != nil {
		return 0, err
	}
	conn := sqlx.NewSqlConnFromSession(session)
	account, err := model.NewUserAccountModel(conn).FindOneByUserIdKind(ctx, request.UserID, model.KindUSD)
	if err != nil {
		return 0, err
	}
	result, err := model.NewAccountHoldModel(conn).Insert(ctx, &model.AccountHold{
		AccountId: account.Id, BusinessType: string(request.BusinessType), BusinessId: request.BusinessID,
		Amount: request.Amount, Status: model.HoldHeld, HoldVoucherId: voucher,
	})
	if err != nil {
		return 0, fmt.Errorf("insert hold for %+v: %w", request, err)
	}
	return result.LastInsertId()
}

// Consume debits the entire held amount and credits the business's counterpart
// entries in the same voucher. Proof of external settlement belongs to the caller.
func (s *Service) Consume(ctx context.Context, session sqlx.Session, id int64, credits []Entry) (int64, error) {
	return s.resolveHold(ctx, session, id, model.HoldConsumed, model.HoldCompleted, credits)
}

// Release is allowed only after the business has established that execution did
// not occur and cannot later occur. Timeouts alone are not such evidence.
func (s *Service) Release(ctx context.Context, session sqlx.Session, id int64, reason string) (int64, error) {
	if !model.ValidReleaseReason(reason) {
		return 0, fmt.Errorf("%w: release reason", ErrInvalidHold)
	}
	return s.resolveHold(ctx, session, id, model.HoldReleased, reason, nil)
}

func (s *Service) resolveHold(ctx context.Context, session sqlx.Session, id int64, status, reason string, credits []Entry) (voucher int64, err error) {
	defer func() { err = classifyWriteError(err) }()
	if err = transaction.RequireSession(ctx, session); err != nil {
		return 0, err
	}
	if id <= 0 {
		return 0, ErrInvalidHold
	}
	conn := sqlx.NewSqlConnFromSession(session)
	holds := model.NewAccountHoldModel(conn)
	hold, err := holds.FindForUpdate(ctx, id)
	if errors.Is(err, model.ErrNotFound) {
		return 0, ErrHoldNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("lock hold %d: %w", id, err)
	}
	if hold.Status != model.HoldHeld {
		return 0, fmt.Errorf("%w: hold %d is %s", ErrHoldResolved, id, hold.Status)
	}
	// Ownership/kind never change. Actual balances are re-read under the sorted
	// account locks in post; this lookup does not acquire another account lock.
	account, err := model.NewUserAccountModel(conn).FindOne(ctx, hold.AccountId)
	if err != nil {
		return 0, err
	}
	if account.Kind != model.KindUSD || !holdSource(BusinessType(hold.BusinessType)) {
		return 0, fmt.Errorf("%w: stored hold %d account/source", ErrInvalidHold, id)
	}
	action := ActionConsume
	if status == model.HoldReleased {
		action = ActionRelease
		credits = []Entry{Credit(account.UserId, model.KindUSD, hold.Amount)}
	} else {
		total := decimal.Zero
		for _, line := range credits {
			if line.Side != SideCredit || line.part != "" || (line.System == "" && (line.Kind != model.KindUSD || line.UserID == account.UserId)) {
				return 0, fmt.Errorf("%w: consume requires USD counterpart credits", ErrInvalidHold)
			}
			total = total.Add(line.Amount)
		}
		if len(credits) == 0 || !total.Equal(hold.Amount) {
			return 0, fmt.Errorf("%w: consume total must equal held amount", ErrInvalidHold)
		}
	}
	debit := Debit(account.UserId, model.KindUSD, hold.Amount)
	debit.part = model.BalanceHeld
	entries := append([]Entry{debit}, credits...)
	voucher, err = s.post(ctx, session, Posting{
		Source:            Source{BusinessType(hold.BusinessType), hold.BusinessId, action},
		OriginalVoucherID: hold.HoldVoucherId, Entries: entries,
	})
	if err != nil {
		return 0, err
	}
	if err = holds.Resolve(ctx, id, status, reason, voucher); err != nil {
		return 0, fmt.Errorf("resolve hold %d: %w", id, err)
	}
	return voucher, nil
}

type HoldPage struct {
	Holds  []model.AccountHold
	NextID int64
}

func (s *Service) Holds(ctx context.Context, userID int64, status string, before int64, limit int) (HoldPage, error) {
	if userID <= 0 || (status != "" && status != model.HoldHeld && status != model.HoldConsumed && status != model.HoldReleased) || before < 0 || limit < 1 || limit > MaxHoldPageSize {
		return HoldPage{}, apperror.New(apperror.InvalidArgument, fmt.Errorf("invalid hold owner, status or page"))
	}
	rows, err := s.accounts.ListHolds(ctx, userID, status, before, limit+1)
	if err != nil {
		return HoldPage{}, fmt.Errorf("list holds for user %d: %w", userID, err)
	}
	page := HoldPage{Holds: rows}
	if len(rows) > limit {
		page.Holds = rows[:limit]
		page.NextID = page.Holds[limit-1].Id
	}
	return page, nil
}
