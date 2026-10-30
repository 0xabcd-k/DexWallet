package backoffice

import (
	"context"
	"errors"
	"fmt"
	auth "github.com/0xabcd-k/DexWallet/backend/logic/auth"
	usermodel "github.com/0xabcd-k/DexWallet/backend/model/user"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
	"golang.org/x/crypto/bcrypt"
	"net/netip"
)

func (s *Service) password(ctx context.Context, m *store, r Realm, email, password string) (*Account, error) {
	u, err := m.email(ctx, r, email, false)
	if errors.Is(err, sqlx.ErrNotFound) {
		_ = bcrypt.CompareHashAndPassword(s.dummy, []byte(password))
		return nil, auth.ErrCode
	}
	if err != nil {
		return nil, err
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) != nil || u.Status != usermodel.StatusActive {
		return nil, auth.ErrCode
	}
	return u, nil
}
func (s *Service) loginInput(ctx context.Context, r Realm, email, password, source string, phase auth.RateAction) (string, error) {
	if s.mail == nil || s.limits[r] == nil {
		return "", auth.ErrUnavailable
	}
	email, err := emailAddress(email)
	if err != nil {
		return "", err
	}
	if len(password) == 0 || len(password) > PasswordMaxBytes {
		return "", auth.ErrCode
	}
	ip, err := netip.ParseAddr(source)
	if err != nil {
		return "", auth.ErrInvalid
	}
	if err = s.limits[r].Allow(ctx, phase, email, ip.String()); err != nil {
		return "", err
	}
	return email, nil
}

func (s *Service) SendCode(ctx context.Context, r Realm, email, password, source, locale, trace string) (*auth.Challenge, error) {
	email, err := s.loginInput(ctx, r, email, password, source, auth.RateSend)
	if err != nil {
		return nil, err
	}
	a := Actor{Realm: r, TraceID: trace}
	u, err := s.password(ctx, s.store, r, email, password)
	if err != nil {
		return nil, s.failure(ctx, a, ActionCode, r, &Account{Email: email}, "", err)
	}
	a.ID = u.Id
	result, err := s.proofs.Send(ctx, string(r), email, fmt.Sprint(u.AuthVersion), locale, s.mail)
	if err != nil {
		return nil, s.failure(ctx, a, ActionCode, r, u, "", err)
	}
	return result, nil
}
func (s *Service) Login(ctx context.Context, r Realm, email, password, challenge, code, source, trace string) (*Account, error) {
	email, err := s.loginInput(ctx, r, email, password, source, auth.RateVerify)
	if err != nil {
		return nil, err
	}
	a := Actor{Realm: r, TraceID: trace}
	u, err := s.password(ctx, s.store, r, email, password)
	if err != nil {
		return nil, s.failure(ctx, a, ActionLogin, r, &Account{Email: email}, "", err)
	}
	a.ID = u.Id
	if err = s.proofs.Verify(ctx, string(r), email, fmt.Sprint(u.AuthVersion), challenge, code); err != nil {
		return nil, s.failure(ctx, a, ActionLogin, r, u, "", err)
	}
	s.record(ctx, a, ActionLogin, r, u, "", ResultSuccess)
	return u, nil
}
