// Package auth validates and consumes external proofs before product identity operations.
// Product tokens and HTTP authentication live at the API boundary.
package auth

import (
	"context"
	"encoding/hex"
	"errors"
	"net/netip"
	"time"

	userlogic "github.com/0xabcd-k/DexWallet/backend/logic/user"
	usermodel "github.com/0xabcd-k/DexWallet/backend/model/user"
)

var (
	ErrInvalid     = errors.New("invalid authentication input")
	ErrCode        = errors.New("invalid or expired verification code")
	ErrTelegram    = errors.New("invalid or expired Telegram authorization")
	ErrReplay      = errors.New("authorization material already used")
	ErrRateLimited = errors.New("authentication rate limited")
	ErrUnavailable = errors.New("authentication provider unavailable")
)

type Result = userlogic.Resolved

type Intent struct {
	Purpose userlogic.Purpose
	Actor   userlogic.Actor
}

func (i Intent) valid() bool {
	if i.Purpose == userlogic.Login {
		return i.Actor.UserID == 0 && i.Actor.AuthVersion == 0
	}
	return (i.Purpose == userlogic.Bind || i.Purpose == userlogic.Replace || i.Purpose == userlogic.Unbind) && i.Actor.UserID > 0 && i.Actor.AuthVersion > 0
}

type Mailer interface {
	SendCode(context.Context, string, string, string) error
}
type Limiter interface {
	Allow(context.Context, RateAction, string, string) error
}
type Service struct {
	cfg    Config
	users  *userlogic.Service
	proofs *Proofs
	mail   Mailer
	limit  Limiter
	now    func() time.Time
}

func New(cfg Config, users *userlogic.Service, proofs *Proofs, mail Mailer, limit Limiter) (*Service, error) {
	if err := cfg.Check(); err != nil {
		return nil, err
	}
	if users == nil || proofs == nil || limit == nil || (cfg.EmailEnabled && mail == nil) {
		return nil, ErrUnavailable
	}
	return &Service{cfg: cfg, users: users, proofs: proofs, mail: mail, limit: limit, now: time.Now}, nil
}
func validToken(value string) bool {
	if len(value) != 64 {
		return false
	}
	b, err := hex.DecodeString(value)
	return err == nil && hex.EncodeToString(b) == value
}
func sourceIP(value string) (string, error) {
	ip, err := netip.ParseAddr(value)
	if err != nil {
		return "", ErrInvalid
	}
	return ip.Unmap().String(), nil
}

func (s *Service) Unbind(ctx context.Context, actor userlogic.Actor, kind usermodel.IdentityType) (*Result, error) {
	return s.users.ChangeIdentity(ctx, actor, userlogic.Unbind, kind, userlogic.EmailAddress{}, nil)
}
