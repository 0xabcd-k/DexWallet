package account

import (
	"context"
	"errors"
	"reflect"
	"testing"

	model "github.com/0xabcd-k/DexWallet/backend/model/account"
	"github.com/shopspring/decimal"
)

type accountStore struct {
	rows   []*model.UserAccount
	err    error
	calls  int
	userID int64
}

func (s *accountStore) ListByUserID(_ context.Context, id int64) ([]*model.UserAccount, error) {
	s.calls++
	s.userID = id
	return s.rows, s.err
}

func (s *accountStore) ListHolds(context.Context, int64, string, int64, int) ([]model.AccountHold, error) {
	return nil, s.err
}

func (s *accountStore) ListEntries(context.Context, int64, string, int64, int) ([]model.StatementEntry, error) {
	return nil, s.err
}

func TestListBalancesReadsOnceAndFillsOnlyMissingKinds(t *testing.T) {
	store := &accountStore{}
	s := New(store)
	zero := []Balance{
		{model.KindUSD, model.UnitUSD, "0.00", "0.00", "0.00"},
		{model.KindRewardUSD, model.UnitUSD, "0.00", "0.00", "0.00"},
		{model.KindTaskPoints, model.UnitPoints, "0", "0", "0"},
	}
	got, err := s.List(context.Background(), 7)
	if err != nil || !reflect.DeepEqual(got, zero) || store.calls != 1 || store.userID != 7 {
		t.Fatalf("empty accounts: %+v %v, reads=%d owner=%d", got, err, store.calls, store.userID)
	}
	store.rows = []*model.UserAccount{
		{Kind: model.KindTaskPoints, Balance: decimal.RequireFromString("9007199254740993")},
		{Kind: model.KindUSD, Balance: decimal.RequireFromString("9007199254740993.01"), HeldBalance: decimal.RequireFromString("2.09")},
	}
	got, err = s.List(context.Background(), 7)
	want := []Balance{
		{model.KindUSD, model.UnitUSD, "9007199254740993.01", "2.09", "9007199254740995.10"},
		zero[1],
		{model.KindTaskPoints, model.UnitPoints, "9007199254740993", "0", "9007199254740993"},
	}
	if err != nil || !reflect.DeepEqual(got, want) || store.calls != 2 {
		t.Fatalf("partial accounts or cached read: %+v %v, reads=%d", got, err, store.calls)
	}
}

func TestListBalancesDoesNotTurnFailuresIntoZero(t *testing.T) {
	failure := errors.New("database unavailable")
	store := &accountStore{err: failure}
	s := New(store)
	if got, err := s.List(context.Background(), 1); !errors.Is(err, failure) || got != nil {
		t.Fatalf("storage failure hidden: %+v %v", got, err)
	}
	store.err = nil
	for _, row := range []*model.UserAccount{
		{Kind: "unknown"},
		{Kind: model.KindUSD, Balance: decimal.RequireFromString("1.001")},
		{Kind: model.KindTaskPoints, Balance: decimal.NewFromInt(-1)},
		{Kind: model.KindRewardUSD, HeldBalance: decimal.NewFromInt(1)},
	} {
		store.rows = []*model.UserAccount{row}
		if got, err := s.List(context.Background(), 1); err == nil || got != nil {
			t.Fatalf("invalid stored account hidden/rounded: %+v %v", got, err)
		}
	}
	before := store.calls
	if _, err := s.List(context.Background(), 0); err == nil || store.calls != before {
		t.Fatal("invalid owner reached storage")
	}
}
