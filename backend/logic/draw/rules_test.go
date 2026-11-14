package draw

import (
	"encoding/json"
	"github.com/shopspring/decimal"
	"testing"
)

func TestCandidateExactDistribution(t *testing.T) {
	r := SampleRules()
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	counts := map[string]int64{}
	sum := decimal.Zero
	for ticket := int64(0); ticket < TotalWeight; ticket++ {
		p, err := r.Pick(ticket)
		if err != nil {
			t.Fatal(err)
		}
		counts[p.Code]++
		sum = sum.Add(p.RewardUSD)
		if code, err := SymbolPrize(p.Symbols); err != nil || code != p.Code {
			t.Fatal(p, err)
		}
	}
	for _, p := range r.Prizes {
		if counts[p.Code] != p.Weight {
			t.Fatal(p.Code, counts)
		}
	}
	if !sum.Div(decimal.NewFromInt(TotalWeight)).Equal(decimal.New(95, -3)) {
		t.Fatal(sum)
	}
	for _, invalid := range []int64{-1, TotalWeight} {
		if _, err := r.Pick(invalid); err == nil {
			t.Fatal(invalid)
		}
	}
	raw, _ := json.Marshal(r)
	parsed, err := ParseRules(string(raw))
	if err != nil || !parsed.RTP.Equal(r.RTP) {
		t.Fatal(parsed, err)
	}
}
func TestRejectInvalidRules(t *testing.T) {
	cases := map[string]func(*Rules){
		"weight sum":         func(r *Rules) { r.Prizes[0].Weight++ },
		"negative weight":    func(r *Rules) { r.Prizes[0].Weight = -1 },
		"duplicate code":     func(r *Rules) { r.Prizes[0].Code = r.Prizes[1].Code },
		"missing tier":       func(r *Rules) { r.Prizes = r.Prizes[:6] },
		"mismatched symbols": func(r *Rules) { r.Prizes[0].Symbols = [3]int64{1, 1, 1} },
		"unknown symbols":    func(r *Rules) { r.Prizes[0].Symbols = [3]int64{8, 8, 8} },
		"money precision":    func(r *Rules) { r.Prizes[0].RewardUSD = decimal.New(801, -4) },
		"empty prize":        func(r *Rules) { r.Prizes[0].RewardUSD = decimal.Zero },
		"false RTP":          func(r *Rules) { r.RTP = decimal.New(950001, -6) },
		"capacity":           func(r *Rules) { r.Prizes[0].RewardUSD = decimal.New(1, 18) },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			r := SampleRules()
			change(&r)
			if r.Validate() == nil {
				t.Fatal("accepted invalid configuration")
			}
		})
	}
	r := SampleRules()
	r.Prizes[0].Weight = 6500
	r.Prizes[1].Weight = 0
	r.RTP = decimal.New(90, -2)
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	for ticket := int64(0); ticket < TotalWeight; ticket++ {
		p, _ := r.Pick(ticket)
		if p.Code == PrizeTwo {
			t.Fatal("selected zero-weight prize")
		}
	}
}
