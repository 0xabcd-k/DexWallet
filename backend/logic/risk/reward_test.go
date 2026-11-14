package risk

import (
	"context"
	"testing"
)

func TestMockOnlyLocalAndExplicit(t *testing.T) {
	c := Config{Mode: ModeMock, DefaultDecision: Allow, Fixtures: []Fixture{{UserID: 7, Decision: Review}}}
	for _, env := range []string{"production", "staging", "integration"} {
		if c.Check(env, "dev") == nil {
			t.Fatal("mock allowed", env)
		}
	}
	if c.Check("local", "pro") == nil {
		t.Fatal("mock allowed outside dev")
	}
	if err := c.Check("local", "dev"); err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct {
		id   int64
		want string
	}{{7, Review}, {8, Allow}} {
		d, err := c.Assessor().Assess(context.Background(), Request{UserID: row.id})
		if err != nil || d.Action != row.want || d.Version != MockVersion {
			t.Fatal(d, err)
		}
	}
	c.DefaultDecision = ""
	if c.Check("local", "dev") == nil {
		t.Fatal("implicit risk allow")
	}
	if (Config{}).Assessor() != nil {
		t.Fatal("disabled assessor should be unavailable")
	}
}
func TestInvalidDecisionNotAllowed(t *testing.T) {
	for _, d := range []Decision{{}, {Action: "unknown", Version: "v1"}, {Action: Allow}} {
		if d.Validate() == nil {
			t.Fatal(d)
		}
	}
}
