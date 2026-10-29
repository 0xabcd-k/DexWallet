// Package session implements the product-user JWT policy; it stores no sessions.
package session

import (
	"errors"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/idcodec"
	"github.com/golang-jwt/jwt/v4"
)

const (
	Lifetime      = 72 * time.Hour
	RenewBefore   = Lifetime / 2
	Audience      = "product-user"
	TokenHeader   = "X-Access-Token"
	MaxTokenBytes = 4096
)

var ErrInvalid = errors.New("invalid or expired product token")

type Config struct {
	Secret string `json:",optional"`
}

func (c Config) Check() error {
	if len(c.Secret) < 32 {
		return errors.New("session signing secret must contain at least 32 bytes")
	}
	return nil
}

type Claims struct {
	jwt.RegisteredClaims
	AuthVersion int64 `json:"auth_version"`
}

type Tokens struct {
	secret []byte
	issuer string
	idx    *idcodec.Codec
	now    func() time.Time
}

func New(c Config, issuer string, idx *idcodec.Codec) (*Tokens, error) {
	if err := c.Check(); err != nil {
		return nil, err
	}
	if issuer == "" || idx == nil {
		return nil, errors.New("token issuer and idx codec required")
	}
	return &Tokens{secret: []byte(c.Secret), issuer: issuer, idx: idx, now: time.Now}, nil
}

func (s *Tokens) Issue(id, version int64) (string, *Claims, error) {
	if version <= 0 {
		return "", nil, ErrInvalid
	}
	idx, err := s.idx.Encode(id)
	if err != nil {
		return "", nil, err
	}
	now := s.now().Truncate(time.Second)
	c := &Claims{RegisteredClaims: jwt.RegisteredClaims{Issuer: s.issuer, Subject: idx,
		Audience: jwt.ClaimStrings{Audience}, IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(Lifetime))}, AuthVersion: version}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString(s.secret)
	return token, c, err
}

// Parse delegates cryptography/decoding to jwt. Required claims and the product
// lifetime use an instance clock, without jwt's process-global TimeFunc.
func (s *Tokens) Parse(raw string) (*Claims, int64, error) {
	if len(raw) == 0 || len(raw) > MaxTokenBytes {
		return nil, 0, ErrInvalid
	}
	c := new(Claims)
	p := jwt.NewParser(jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithoutClaimsValidation())
	t, err := p.ParseWithClaims(raw, c, func(*jwt.Token) (any, error) { return s.secret, nil })
	if err != nil || !t.Valid {
		return nil, 0, ErrInvalid
	}
	now := s.now()
	if c.Issuer != s.issuer || len(c.Audience) != 1 || c.Audience[0] != Audience || c.AuthVersion <= 0 ||
		c.IssuedAt == nil || c.ExpiresAt == nil || c.IssuedAt.Unix() <= 0 || c.IssuedAt.After(now) ||
		!now.Before(c.ExpiresAt.Time) || c.ExpiresAt.Sub(c.IssuedAt.Time) != Lifetime ||
		(c.NotBefore != nil && now.Before(c.NotBefore.Time)) {
		return nil, 0, ErrInvalid
	}
	id, err := s.idx.Decode(c.Subject)
	if err != nil {
		return nil, 0, ErrInvalid
	}
	return c, id, nil
}

// Called only after ordinary authorization. Preserve the verified version even
// when a cache entry is briefly stale; never upgrade it from a newer user row.
func (s *Tokens) Renewal(c *Claims, id int64) (string, error) {
	now := s.now()
	if !now.Before(c.ExpiresAt.Time) {
		return "", ErrInvalid
	}
	if c.ExpiresAt.Sub(now) >= RenewBefore {
		return "", nil
	}
	token, _, err := s.Issue(id, c.AuthVersion)
	return token, err
}
