package account

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"slices"

	"github.com/0xabcd-k/DexWallet/backend/idcodec"
	model "github.com/0xabcd-k/DexWallet/backend/model/account"
	"github.com/0xabcd-k/DexWallet/backend/transaction"
	driver "github.com/go-sql-driver/mysql"
	"github.com/shopspring/decimal"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

type Side string

const (
	SideCredit Side = "credit"
	SideDebit  Side = "debit"
)

type BusinessType string

const (
	BusinessTransfer BusinessType = "transfer"
	BusinessDeposit  BusinessType = "deposit"
	BusinessReward   BusinessType = "reward"
)

func (b BusinessType) IDKind() (idcodec.Kind, bool) {
	switch b {
	case BusinessTransfer:
		return idcodec.Transfer, true
	case BusinessDeposit:
		return idcodec.Deposit, true
	case BusinessReward:
		return idcodec.RewardGrant, true
	default:
		return "", false
	}
}

type Action string

const ActionPost Action = "post"

// System accounts have no spendable user balance. Their debit/credit entries
// represent the specified expense, issuance or clearing, not proof of receipt.
type SystemCode string

const (
	SystemUSDClearing   SystemCode = "usd_clearing"
	SystemRewardExpense SystemCode = "reward_expense"
	SystemPointIssuance SystemCode = "point_issuance"
)

func (s SystemCode) definition() (model.Definition, bool) {
	switch s {
	case SystemUSDClearing, SystemRewardExpense:
		return model.Lookup(model.KindUSD)
	case SystemPointIssuance:
		return model.Lookup(model.KindTaskPoints)
	default:
		return model.Definition{}, false
	}
}

var (
	ErrInvalidPosting      = errors.New("invalid ledger posting")
	ErrUnbalanced          = errors.New("ledger entries do not balance by unit")
	ErrAlreadyPosted       = errors.New("business action already posted")
	ErrInsufficientBalance = errors.New("insufficient available balance")
)

type Source struct {
	Type   BusinessType
	ID     int64
	Action Action
}

type Entry struct {
	UserID int64
	Kind   string
	System SystemCode
	Side   Side
	Amount decimal.Decimal
}

// Credit and Debit are positive-amount user balance changes within one posting.
// They do not write or commit independently. User liabilities are credit-normal.
func Credit(userID int64, kind string, amount decimal.Decimal) Entry {
	return Entry{UserID: userID, Kind: kind, Side: SideCredit, Amount: amount}
}
func Debit(userID int64, kind string, amount decimal.Decimal) Entry {
	return Entry{UserID: userID, Kind: kind, Side: SideDebit, Amount: amount}
}

type Posting struct {
	Source            Source
	OriginalVoucherID int64
	Entries           []Entry
}

type accountKey struct {
	userID int64
	kind   string
}

var actionCode = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)

func (p Posting) validate() ([]model.Definition, error) {
	_, known := p.Source.Type.IDKind()
	if !known || p.Source.ID <= 0 || !actionCode.MatchString(string(p.Source.Action)) || p.OriginalVoucherID < 0 || len(p.Entries) < 2 {
		return nil, fmt.Errorf("%w: source, original voucher or entry count", ErrInvalidPosting)
	}
	definitions := make([]model.Definition, len(p.Entries))
	totals := make(map[string]decimal.Decimal)
	for i, line := range p.Entries {
		var definition model.Definition
		var ok bool
		if line.System != "" {
			definition, ok = line.System.definition()
			ok = ok && line.UserID == 0 && line.Kind == ""
		} else {
			definition, ok = model.Lookup(line.Kind)
			ok = ok && line.UserID > 0
		}
		if !ok || (line.Side != SideCredit && line.Side != SideDebit) || !line.Amount.IsPositive() {
			return nil, fmt.Errorf("%w: entry %d target, side or amount", ErrInvalidPosting, i+1)
		}
		if err := definition.ValidateBalances(line.Amount, decimal.Zero); err != nil {
			return nil, fmt.Errorf("%w: entry %d: %v", ErrInvalidPosting, i+1, err)
		}
		definitions[i] = definition
		amount := line.Amount
		if line.Side == SideDebit {
			amount = amount.Neg()
		}
		totals[definition.Unit] = totals[definition.Unit].Add(amount)
	}
	for unit, amount := range totals {
		if !amount.IsZero() {
			return nil, fmt.Errorf("%w: unit %s difference %s", ErrUnbalanced, unit, amount)
		}
	}
	return definitions, nil
}

// Post appends a complete balanced voucher using the business transaction.
// The caller MUST propagate every error to TransactCtx. In particular, duplicate
// posting is an error, not permission to commit other writes or replay a result.
// The business owns authorization, source existence, request deduplication and
// recovery after an indeterminate commit. No public write endpoint calls Post.
func (s *Service) Post(ctx context.Context, session sqlx.Session, p Posting) (int64, error) {
	if err := transaction.RequireSession(ctx, session); err != nil {
		return 0, err
	}
	definitions, err := p.validate()
	if err != nil {
		return 0, err
	}
	conn := sqlx.NewSqlConnFromSession(session)
	vouchers := model.NewLedgerVoucherModel(conn)
	result, err := vouchers.Insert(ctx, &model.LedgerVoucher{
		BusinessType: string(p.Source.Type), BusinessId: p.Source.ID, Action: string(p.Source.Action),
		OriginalVoucherId: sql.NullInt64{Int64: p.OriginalVoucherID, Valid: p.OriginalVoucherID != 0},
	})
	if err != nil {
		var duplicate *driver.MySQLError
		if errors.As(err, &duplicate) && duplicate.Number == 1062 {
			return 0, fmt.Errorf("%w: %+v: %w", ErrAlreadyPosted, p.Source, err)
		}
		return 0, fmt.Errorf("insert voucher %+v: %w", p.Source, err)
	}
	voucherID, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("voucher id: %w", err)
	}
	if p.OriginalVoucherID == voucherID {
		return 0, fmt.Errorf("%w: voucher %d cannot reference itself", ErrInvalidPosting, voucherID)
	}
	// Preparation is also a write/lock, so both preparation and locking follow
	// the same order. Credit targets alone may create a missing zero account.
	credits := make(map[accountKey]bool)
	for _, line := range p.Entries {
		if line.System != "" {
			continue
		}
		key := accountKey{line.UserID, line.Kind}
		credits[key] = credits[key] || line.Side == SideCredit
	}
	keys := make([]accountKey, 0, len(credits))
	for key := range credits {
		keys = append(keys, key)
	}
	slices.SortFunc(keys, func(a, b accountKey) int {
		if n := cmp.Compare(a.userID, b.userID); n != 0 {
			return n
		}
		return cmp.Compare(a.kind, b.kind)
	})
	accounts := model.NewUserAccountModel(conn)
	locked := make(map[accountKey]*model.UserAccount, len(keys))
	for _, key := range keys {
		if credits[key] {
			if err := accounts.PrepareCredit(ctx, key.userID, key.kind); err != nil {
				return 0, fmt.Errorf("prepare account %+v: %w", key, err)
			}
		}
		row, err := accounts.FindForUpdate(ctx, key.userID, key.kind)
		if errors.Is(err, model.ErrNotFound) {
			return 0, fmt.Errorf("%w: account %+v missing", ErrInsufficientBalance, key)
		}
		if err != nil {
			return 0, fmt.Errorf("lock account %+v: %w", key, err)
		}
		definition, _ := model.Lookup(key.kind)
		if err := definition.ValidateBalances(row.Balance, row.HeldBalance); err != nil {
			return 0, fmt.Errorf("stored account %d: %w", row.Id, err)
		}
		locked[key] = row
	}
	entries := make([]*model.LedgerEntry, len(p.Entries))
	for i, line := range p.Entries {
		entry := &model.LedgerEntry{VoucherId: voucherID, LineNo: int64(i + 1), SystemCode: string(line.System), Unit: definitions[i].Unit, Side: string(line.Side), Amount: line.Amount}
		if line.System == "" {
			row := locked[accountKey{line.UserID, line.Kind}]
			if line.Side == SideDebit {
				if row.Balance.LessThan(line.Amount) {
					return 0, fmt.Errorf("%w: account %d available %s, requested %s", ErrInsufficientBalance, row.Id, row.Balance, line.Amount)
				}
				row.Balance = row.Balance.Sub(line.Amount)
			} else {
				row.Balance = row.Balance.Add(line.Amount)
			}
			if err := definitions[i].ValidateBalances(row.Balance, row.HeldBalance); err != nil {
				return 0, fmt.Errorf("%w: account %d: %v", ErrInvalidPosting, row.Id, err)
			}
			entry.AccountId = sql.NullInt64{Int64: row.Id, Valid: true}
			entry.BalanceAfter = decimal.NullDecimal{Decimal: row.Balance, Valid: true}
		}
		entries[i] = entry
	}
	for _, key := range keys {
		row := locked[key]
		if err := accounts.SetBalance(ctx, row.Id, row.Balance); err != nil {
			return 0, fmt.Errorf("update account %d: %w", row.Id, err)
		}
	}
	lines := model.NewLedgerEntryModel(conn)
	for _, entry := range entries {
		if _, err := lines.Insert(ctx, entry); err != nil {
			return 0, fmt.Errorf("voucher %d line %d: %w", voucherID, entry.LineNo, err)
		}
	}
	return voucherID, nil
}
