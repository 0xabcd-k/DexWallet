package account

import (
	"context"
	"fmt"

	"github.com/0xabcd-k/DexWallet/backend/apperror"
	model "github.com/0xabcd-k/DexWallet/backend/model/account"
)

type Store interface {
	ListByUserID(context.Context, int64) ([]*model.UserAccount, error)
	ListEntries(context.Context, int64, string, int64, int) ([]model.StatementEntry, error)
}

type Service struct{ accounts Store }

func New(accounts Store) *Service { return &Service{accounts: accounts} }

type Balance struct {
	Kind         string
	Unit         string
	Balance      string
	HeldBalance  string
	TotalBalance string
}

// List reads once without caching, locking or creating accounts. Missing known
// kinds are zero until their first posting; storage failures remain failures.
func (s *Service) List(ctx context.Context, userID int64) ([]Balance, error) {
	if userID <= 0 {
		return nil, apperror.New(apperror.InvalidArgument, fmt.Errorf("invalid account owner %d", userID))
	}
	rows, err := s.accounts.ListByUserID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list accounts for user %d: %w", userID, err)
	}
	byKind := make(map[string]*model.UserAccount, len(rows))
	for _, row := range rows {
		definition, ok := model.Lookup(row.Kind)
		if !ok {
			return nil, fmt.Errorf("account %d has unknown kind %q", row.Id, row.Kind)
		}
		if err := definition.ValidateBalances(row.Balance, row.HeldBalance); err != nil {
			return nil, fmt.Errorf("account %d: %w", row.Id, err)
		}
		byKind[row.Kind] = row
	}
	definitions := model.Definitions()
	result := make([]Balance, 0, len(definitions))
	for _, definition := range definitions {
		row := byKind[definition.Kind]
		if row == nil {
			row = &model.UserAccount{}
		}
		result = append(result, Balance{
			Kind: definition.Kind, Unit: definition.Unit,
			Balance: row.Balance.StringFixed(definition.Scale), HeldBalance: row.HeldBalance.StringFixed(definition.Scale),
			TotalBalance: row.Balance.Add(row.HeldBalance).StringFixed(definition.Scale),
		})
	}
	return result, nil
}
