package task

import (
	"context"
	"testing"
)

func TestMembershipResultsAndFailures(t *testing.T) {
	for _, status := range []string{MemberCreator, MemberAdministrator, MemberRegular, MemberLeft, MemberKicked} {
		m, err := NewMock([]Fixture{{ChannelID: -100, TelegramUserID: 7, Status: status}})
		if err != nil {
			t.Fatal(err)
		}
		result, err := m.GetMember(context.Background(), -100, 7)
		if err != nil {
			t.Fatal(err)
		}
		member, err := isMember(result, 7)
		if err != nil || member != (status != MemberLeft && status != MemberKicked) {
			t.Fatal(status, member, err)
		}
		if _, err = m.GetMember(context.Background(), -101, 7); err == nil {
			t.Fatal("unconfigured channel passed")
		}
		if _, err = m.GetMember(context.Background(), -100, 8); err == nil {
			t.Fatal("unconfigured identity passed")
		}
	}
	for _, failure := range []string{FailureTimeout, FailureRateLimited, FailurePermission, FailureInvalidResponse} {
		m, err := NewMock([]Fixture{{ChannelID: -100, TelegramUserID: 7, Failure: failure}})
		if err != nil {
			t.Fatal(err)
		}
		result, err := m.GetMember(context.Background(), -100, 7)
		if err == nil {
			_, err = isMember(result, 7)
		}
		if err == nil {
			t.Fatal("failure passed", failure)
		}
	}
	for _, result := range []Member{{UserID: 8, Status: MemberRegular}, {UserID: 7, Status: "restricted"}, {UserID: 7, Status: "unknown"}} {
		if _, err := isMember(result, 7); err == nil {
			t.Fatal("invalid response passed", result)
		}
	}
}
func TestMockConfigurationIsExplicitAndLocal(t *testing.T) {
	c := Config{Mode: ModeMock, Fixtures: []Fixture{{ChannelID: -100, TelegramUserID: 7, Status: MemberRegular}}}
	if err := c.Check("local", "dev"); err != nil {
		t.Fatal(err)
	}
	for _, env := range [][2]string{{"production", "dev"}, {"local", "pro"}, {"test", "test"}} {
		if err := c.Check(env[0], env[1]); err == nil {
			t.Fatal("mock outside local development")
		}
	}
	for _, bad := range []Config{{Mode: "telegram"}, {Mode: ModeDisabled, Fixtures: c.Fixtures}, {Mode: ModeMock, Fixtures: append(c.Fixtures, c.Fixtures...)}, {Mode: ModeMock, Fixtures: []Fixture{{ChannelID: -100, TelegramUserID: 7}}}} {
		if err := bad.Check("local", "dev"); err == nil {
			t.Fatal("invalid config accepted", bad)
		}
	}
	m, _ := NewMock(c.Fixtures)
	c.Fixtures[0].Status = MemberLeft
	result, _ := m.GetMember(context.Background(), -100, 7)
	if result.Status != MemberRegular {
		t.Fatal("caller mutated fixture")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := m.GetMember(ctx, -100, 7); err == nil {
		t.Fatal("cancel ignored")
	}
}
