package account

import (
	"context"
	"errors"
	"testing"

	model "github.com/0xabcd-k/DexWallet/backend/model/account"
	"github.com/shopspring/decimal"
)

func TestPostingRejectsInvalidAndCrossUnitAmounts(t *testing.T) {
	amount := decimal.RequireFromString("2.35")
	valid := func() Posting {
		return Posting{Source: Source{BusinessReward, 1, ActionPost}, Entries: []Entry{
			Credit(1, model.KindRewardUSD, amount), {System: SystemRewardExpense, Side: SideDebit, Amount: amount},
		}}
	}
	for _, tc := range []struct {
		name   string
		change func(*Posting)
		want   error
	}{
		{"zero", func(p *Posting) { p.Entries[0].Amount = decimal.Zero }, ErrInvalidPosting},
		{"negative", func(p *Posting) { p.Entries[0].Amount = amount.Neg() }, ErrInvalidPosting},
		{"precision", func(p *Posting) { p.Entries[0].Amount = decimal.RequireFromString("2.351") }, ErrInvalidPosting},
		{"range", func(p *Posting) { p.Entries[0].Amount = decimal.New(1, 18) }, ErrInvalidPosting},
		{"account", func(p *Posting) { p.Entries[0].Kind = "unknown" }, ErrInvalidPosting},
		{"system", func(p *Posting) { p.Entries[1].System = "unknown" }, ErrInvalidPosting},
		{"two targets", func(p *Posting) { p.Entries[0].System = SystemUSDClearing }, ErrInvalidPosting},
		{"side", func(p *Posting) { p.Entries[0].Side = "unknown" }, ErrInvalidPosting},
		{"owner", func(p *Posting) { p.Entries[0].UserID = 0 }, ErrInvalidPosting},
		{"source", func(p *Posting) { p.Source.ID = 0 }, ErrInvalidPosting},
		{"unknown source", func(p *Posting) { p.Source.Type = "unknown" }, ErrInvalidPosting},
		{"action", func(p *Posting) { p.Source.Action = "" }, ErrInvalidPosting},
		{"original", func(p *Posting) { p.OriginalVoucherID = -1 }, ErrInvalidPosting},
		{"unbalanced", func(p *Posting) { p.Entries[1].Amount = decimal.NewFromInt(3) }, ErrUnbalanced},
		{"cross unit", func(p *Posting) {
			p.Entries = []Entry{Credit(1, model.KindTaskPoints, decimal.NewFromInt(2)), {System: SystemRewardExpense, Side: SideDebit, Amount: decimal.NewFromInt(2)}}
		}, ErrUnbalanced},
		{"fractional points", func(p *Posting) { p.Entries[0].Kind = model.KindTaskPoints }, ErrInvalidPosting},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := valid()
			tc.change(&p)
			if _, err := p.validate(); !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
	p := valid()
	p.Entries = append(p.Entries, Credit(1, model.KindTaskPoints, decimal.NewFromInt(50)), Entry{System: SystemPointIssuance, Side: SideDebit, Amount: decimal.NewFromInt(50)})
	if _, err := p.validate(); err != nil {
		t.Fatal("independently balanced units", err)
	}
	if _, err := New(&accountStore{}).Post(context.Background(), nil, p); err == nil {
		t.Fatal("posting accepted without a transaction")
	}
}

type statementStore struct {
	accountStore
	entries []model.StatementEntry
	owner   int64
	kind    string
	before  int64
	limit   int
}

func (s *statementStore) ListEntries(_ context.Context, id int64, kind string, before int64, limit int) ([]model.StatementEntry, error) {
	s.owner, s.kind, s.before, s.limit = id, kind, before, limit
	return s.entries, s.err
}
func TestStatementValidatesFinancialDataAndPreservesErrors(t *testing.T) {
	row := model.StatementEntry{ID: 9, BalancePart: model.BalanceAvailable, Kind: model.KindUSD, Unit: model.UnitUSD, Side: string(SideCredit), Amount: decimal.RequireFromString("1.01"), BalanceAfter: decimal.RequireFromString("9007199254740993.01"), BusinessType: string(BusinessDeposit)}
	store := &statementStore{entries: []model.StatementEntry{row, row}}
	s := New(store)
	got, err := s.Entries(context.Background(), 7, model.KindUSD, 99, 1)
	if err != nil || len(got.Entries) != 1 || got.NextID != 9 || store.owner != 7 || store.kind != model.KindUSD || store.before != 99 || store.limit != 2 {
		t.Fatalf("page %+v %v, store %+v", got, err, store)
	}
	for _, change := range []func(*model.StatementEntry){
		func(r *model.StatementEntry) { r.Unit = model.UnitPoints }, func(r *model.StatementEntry) { r.Side = "wrong" },
		func(r *model.StatementEntry) { r.Amount = decimal.Zero }, func(r *model.StatementEntry) { r.BalanceAfter = decimal.RequireFromString("1.001") },
	} {
		bad := row
		change(&bad)
		store.entries = []model.StatementEntry{bad}
		if _, err := s.Entries(context.Background(), 7, "", 0, 20); err == nil {
			t.Fatal("invalid stored amount presented")
		}
	}
	store.err = errors.New("offline")
	if _, err := s.Entries(context.Background(), 7, "", 0, 20); !errors.Is(err, store.err) {
		t.Fatal("failure became empty", err)
	}
	for _, kind := range []string{"unknown"} {
		if _, err := s.Entries(context.Background(), 7, kind, 0, 20); err == nil {
			t.Fatal("invalid filter")
		}
	}
	for _, limit := range []int{0, 101} {
		if _, err := s.Entries(context.Background(), 7, "", 0, limit); err == nil {
			t.Fatal("invalid limit")
		}
	}
}
