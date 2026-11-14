// Package risk owns review routing, independently of reward eligibility.
package risk

import (
	"context"
	"fmt"
	"unicode/utf8"

	"github.com/shopspring/decimal"
)

const (
	Allow       = "allow"
	Review      = "review"
	ModeMock    = "mock"
	MockVersion = "local_mock_v1"
)

type Request struct {
	UserID int64
	Amount decimal.Decimal
}
type Decision struct{ Action, Version, Reason string }

func (d Decision) Validate() error {
	if (d.Action != Allow && d.Action != Review) || d.Version == "" || utf8.RuneCountInString(d.Version) > 64 || utf8.RuneCountInString(d.Reason) > 512 {
		return fmt.Errorf("invalid reward risk decision")
	}
	return nil
}

type Assessor interface {
	Assess(context.Context, Request) (Decision, error)
}

// Mock inputs are server configuration, never supplied by a payment client.
type Fixture struct {
	UserID   int64
	Decision string
}
type Config struct {
	Mode            string    `json:",optional"`
	DefaultDecision string    `json:",optional"`
	Fixtures        []Fixture `json:",optional"`
}

func (c Config) Check(environment, mode string) error {
	if c.Mode == "" && c.DefaultDecision == "" && len(c.Fixtures) == 0 {
		return nil
	}
	if c.Mode != ModeMock || environment != "local" || mode != "dev" {
		return fmt.Errorf("reward risk mock requires local/dev")
	}
	if c.DefaultDecision != Allow && c.DefaultDecision != Review {
		return fmt.Errorf("reward risk mock requires explicit default decision")
	}
	seen := map[int64]bool{}
	for _, f := range c.Fixtures {
		if f.UserID <= 0 || seen[f.UserID] || (f.Decision != Allow && f.Decision != Review) {
			return fmt.Errorf("invalid reward risk fixture")
		}
		seen[f.UserID] = true
	}
	return nil
}
func (c Config) Assessor() Assessor {
	if c.Mode == "" {
		return nil
	}
	return mock{c}
}

type mock struct{ config Config }

func (m mock) Assess(ctx context.Context, r Request) (Decision, error) {
	if err := ctx.Err(); err != nil {
		return Decision{}, err
	}
	action := m.config.DefaultDecision
	for _, f := range m.config.Fixtures {
		if f.UserID == r.UserID {
			action = f.Decision
			break
		}
	}
	return Decision{Action: action, Version: MockVersion, Reason: "local mock fixture; no risk score calculated"}, nil
}
