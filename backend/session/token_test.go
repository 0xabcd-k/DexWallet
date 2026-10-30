package session

import (
	"strings"
	"testing"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/idcodec"
	"github.com/golang-jwt/jwt/v4"
)

func fixture(t *testing.T) (*Tokens, *time.Time) {
	t.Helper()
	idx, err := idcodec.New("test-only-key123", idcodec.User)
	if err != nil {
		t.Fatal(err)
	}
	s, err := New(Config{Secret: strings.Repeat("s", 32)}, "test", idx)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	return s, &now
}
func TestLifetimeRenewalAndNoCumulativeLimit(t *testing.T) {
	s, now := fixture(t)
	raw, c, err := s.Issue(12, 7)
	if err != nil {
		t.Fatal(err)
	}
	if c.ExpiresAt.Sub(c.IssuedAt.Time) != 72*time.Hour {
		t.Fatal("wrong lifetime")
	}
	first := *now
	*now = now.Add(36 * time.Hour)
	if renewed, err := s.Renewal(c, 12); err != nil || renewed != "" {
		t.Fatal("renewed at exact half-life", err)
	}
	*now = now.Add(time.Second)
	for i := 0; i < 24; i++ {
		parsed, id, err := s.Parse(raw)
		if err != nil || id != 12 || parsed.AuthVersion != 7 {
			t.Fatal("valid token rejected", err)
		}
		raw, err = s.Renewal(parsed, id)
		if err != nil || raw == "" {
			t.Fatal("renewal failed", err)
		}
		c, id, err = s.Parse(raw)
		if err != nil || c.ExpiresAt.Unix() != now.Add(72*time.Hour).Unix() || c.AuthVersion != 7 {
			t.Fatal("renewal changed scope/lifetime", err)
		}
		*now = now.Add(36*time.Hour + time.Second)
	}
	if now.Sub(first) < 30*24*time.Hour {
		t.Fatal("test did not cross 30 days")
	}
	*now = c.ExpiresAt.Time
	if _, _, err = s.Parse(raw); err == nil {
		t.Fatal("expired token accepted")
	}
	if _, err = s.Renewal(c, 12); err == nil {
		t.Fatal("expired token renewed")
	}
}
func TestRejectInvalidClaimsAndSigningScope(t *testing.T) {
	s, _ := fixture(t)
	cases := []struct {
		name   string
		change func(*Claims)
		method jwt.SigningMethod
		key    any
	}{
		{name: "missing expiration", change: func(c *Claims) { c.ExpiresAt = nil }},
		{name: "missing issued at", change: func(c *Claims) { c.IssuedAt = nil }},
		{name: "future issued at", change: func(c *Claims) {
			c.IssuedAt = jwt.NewNumericDate(c.IssuedAt.Add(time.Second))
			c.ExpiresAt = jwt.NewNumericDate(c.ExpiresAt.Add(time.Second))
		}},
		{name: "extended lifetime", change: func(c *Claims) { c.ExpiresAt = jwt.NewNumericDate(c.ExpiresAt.Add(time.Second)) }},
		{name: "merchant", change: func(c *Claims) { c.Audience = jwt.ClaimStrings{"merchant"} }},
		{name: "admin", change: func(c *Claims) { c.Audience = jwt.ClaimStrings{"admin"} }},
		{name: "multiple audiences", change: func(c *Claims) { c.Audience = append(c.Audience, "admin") }},
		{name: "other environment", change: func(c *Claims) { c.Issuer = "production" }},
		{name: "numeric subject", change: func(c *Claims) { c.Subject = "12" }},
		{name: "missing version", change: func(c *Claims) { c.AuthVersion = 0 }},
		{name: "not before", change: func(c *Claims) { c.NotBefore = jwt.NewNumericDate(c.IssuedAt.Add(time.Hour)) }},
		{name: "wrong algorithm", method: jwt.SigningMethodHS384},
		{name: "none", method: jwt.SigningMethodNone, key: jwt.UnsafeAllowNoneSignatureType},
		{name: "wrong secret", key: []byte(strings.Repeat("x", 32))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, c, err := s.Issue(12, 7)
			if err != nil {
				t.Fatal(err)
			}
			if tc.change != nil {
				tc.change(c)
			}
			method := tc.method
			if method == nil {
				method = jwt.SigningMethodHS256
			}
			key := tc.key
			if key == nil {
				key = s.secret
			}
			raw, err := jwt.NewWithClaims(method, c).SignedString(key)
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err = s.Parse(raw); err == nil {
				t.Fatal("invalid token accepted")
			}
		})
	}
	for _, raw := range []string{"", "malformed", strings.Repeat("x", MaxTokenBytes+1)} {
		if _, _, err := s.Parse(raw); err == nil {
			t.Fatal("invalid input accepted")
		}
	}
}

// Even accidental signing-key reuse must not allow cross-surface tokens.
func TestAudienceIsolation(t *testing.T) {
	idx, err := idcodec.New("test-only-key123", idcodec.User)
	if err != nil {
		t.Fatal(err)
	}
	var tokens []*Tokens
	for _, aud := range []string{Audience, AdminAudience, MerchantAudience} {
		s, err := NewForAudience(Config{Secret: strings.Repeat("s", 32)}, "test", idx, aud)
		if err != nil {
			t.Fatal(err)
		}
		tokens = append(tokens, s)
	}
	for i, issuer := range tokens {
		raw, _, err := issuer.Issue(1, 1)
		if err != nil {
			t.Fatal(err)
		}
		for j, verifier := range tokens {
			_, _, err := verifier.Parse(raw)
			if (err == nil) != (i == j) {
				t.Fatalf("issuer %d verifier %d: %v", i, j, err)
			}
		}
	}
}
