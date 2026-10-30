package account

import (
	"context"
	"fmt"

	"github.com/0xabcd-k/DexWallet/backend/apperror"
	model "github.com/0xabcd-k/DexWallet/backend/model/account"
	"github.com/shopspring/decimal"
)

// Keep these bounds aligned with AccountEntriesRequest in platform.api.
const DefaultEntryPageSize = 20
const MaxEntryPageSize = 100

const (
	DirectionIn    = "in"
	DirectionOut   = "out"
	ActivityPosted = "posted"
)

type EntryPage struct {
	Entries []model.StatementEntry
	NextID  int64
}

func (s *Service) Entries(ctx context.Context, userID int64, kind string, before int64, limit int) (EntryPage, error) {
	_, known := model.Lookup(kind)
	if userID <= 0 || (kind != "" && !known) || before < 0 || limit < 1 || limit > MaxEntryPageSize {
		return EntryPage{}, apperror.New(apperror.InvalidArgument, fmt.Errorf("invalid statement owner, filter or page"))
	}
	rows, err := s.accounts.ListEntries(ctx, userID, kind, before, limit+1)
	if err != nil {
		return EntryPage{}, fmt.Errorf("list entries for user %d: %w", userID, err)
	}
	page := EntryPage{Entries: rows}
	if len(rows) > limit {
		page.Entries = rows[:limit]
		page.NextID = page.Entries[limit-1].ID
	}
	for _, row := range page.Entries {
		definition, ok := model.Lookup(row.Kind)
		_, sourceKnown := BusinessType(row.BusinessType).IDKind()
		if !ok || !sourceKnown || definition.Unit != row.Unit || (Side(row.Side) != SideCredit && Side(row.Side) != SideDebit) || !row.Amount.IsPositive() || (row.BalancePart != model.BalanceAvailable && row.BalancePart != model.BalanceHeld) || (row.BalancePart == model.BalanceHeld && !definition.AllowHeld) {
			return EntryPage{}, fmt.Errorf("stored ledger entry %d has invalid kind, source, unit, side or amount", row.ID)
		}
		for _, value := range []decimal.Decimal{row.Amount, row.BalanceAfter} {
			if err := definition.ValidateBalances(value, decimal.Zero); err != nil {
				return EntryPage{}, fmt.Errorf("stored ledger entry %d: %w", row.ID, err)
			}
		}
	}
	return page, nil
}

// A reservation/release is shown once, on its available leg. The model query
// omits the paired held leg; consumption remains visible on the held leg.
func EntryActivity(row model.StatementEntry) string {
	switch {
	case row.Action == string(ActionHold):
		return model.HoldHeld
	case row.Action == string(ActionRelease):
		return model.HoldReleased
	case row.Action == string(ActionConsume) && row.BalancePart == model.BalanceHeld:
		return model.HoldConsumed
	default:
		return ActivityPosted
	}
}
