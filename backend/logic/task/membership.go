package task

import (
	"context"
	"errors"
	"fmt"
)

const (
	ModeDisabled           = "disabled"
	ModeMock               = "mock"
	MemberCreator          = "creator"
	MemberAdministrator    = "administrator"
	MemberRegular          = "member"
	MemberLeft             = "left"
	MemberKicked           = "kicked"
	FailureTimeout         = "timeout"
	FailureRateLimited     = "rate_limited"
	FailurePermission      = "permission_denied"
	FailureInvalidResponse = "invalid_response"
)

type Member struct {
	UserID int64
	Status string
}

// Membership is the external Telegram boundary. It never decides task eligibility.
type Membership interface {
	GetMember(context.Context, int64, int64) (Member, error)
}

type Fixture struct {
	ChannelID      int64
	TelegramUserID int64
	Status         string `json:",optional"`
	Failure        string `json:",optional"`
}
type Config struct {
	Mode     string    `json:",default=disabled"`
	Fixtures []Fixture `json:",optional"`
}

func (c Config) Check(environment, mode string) error {
	if c.Mode == "" || c.Mode == ModeDisabled {
		if len(c.Fixtures) != 0 {
			return errors.New("disabled tasks must not configure mock fixtures")
		}
		return nil
	}
	if c.Mode != ModeMock || environment != "local" || mode != "dev" {
		return errors.New("task mock requires Environment=local and Mode=dev; real Telegram verification is not enabled")
	}
	_, err := NewMock(c.Fixtures)
	return err
}

type memberKey struct{ channel, user int64 }
type Mock struct{ fixtures map[memberKey]Fixture }

// NewMock copies fixtures; concurrent reads cannot alter the configured scenario.
func NewMock(fixtures []Fixture) (*Mock, error) {
	m := &Mock{fixtures: make(map[memberKey]Fixture, len(fixtures))}
	for _, f := range fixtures {
		key := memberKey{f.ChannelID, f.TelegramUserID}
		if f.ChannelID >= 0 || f.TelegramUserID <= 0 {
			return nil, errors.New("invalid task fixture identity")
		}
		if _, exists := m.fixtures[key]; exists {
			return nil, errors.New("duplicate task fixture")
		}
		if f.Failure != "" {
			switch f.Failure {
			case FailureTimeout, FailureRateLimited, FailurePermission, FailureInvalidResponse:
			default:
				return nil, errors.New("unknown task fixture failure")
			}
			if f.Status != "" {
				return nil, errors.New("fixture must specify status or failure, not both")
			}
		} else if _, err := isMember(Member{UserID: f.TelegramUserID, Status: f.Status}, f.TelegramUserID); err != nil {
			return nil, err
		}
		m.fixtures[key] = f
	}
	return m, nil
}
func (m *Mock) GetMember(ctx context.Context, channel, user int64) (Member, error) {
	if err := ctx.Err(); err != nil {
		return Member{}, err
	}
	f, ok := m.fixtures[memberKey{channel, user}]
	if !ok {
		return Member{}, fmt.Errorf("no task mock fixture for channel %d user %d", channel, user)
	}
	switch f.Failure {
	case FailureTimeout:
		return Member{}, context.DeadlineExceeded
	case FailureRateLimited, FailurePermission:
		return Member{}, fmt.Errorf("task mock: %s", f.Failure)
	case FailureInvalidResponse:
		return Member{UserID: user, Status: "invalid"}, nil
	}
	return Member{UserID: user, Status: f.Status}, nil
}
func isMember(m Member, user int64) (bool, error) {
	if m.UserID != user {
		return false, errors.New("membership response identity mismatch")
	}
	switch m.Status {
	case MemberCreator, MemberAdministrator, MemberRegular:
		return true, nil
	case MemberLeft, MemberKicked:
		return false, nil
	default:
		return false, fmt.Errorf("unexpected channel membership status %q", m.Status)
	}
}
