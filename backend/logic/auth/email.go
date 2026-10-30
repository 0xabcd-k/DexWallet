package auth

import (
	"context"
	"fmt"

	userlogic "github.com/0xabcd-k/DexWallet/backend/logic/user"
	usermodel "github.com/0xabcd-k/DexWallet/backend/model/user"
)

const ProductRealm = "user"

func intentBinding(i Intent) string {
	return fmt.Sprintf("%s:%d:%d", i.Purpose, i.Actor.UserID, i.Actor.AuthVersion)
}
func (s *Service) emailInput(ctx context.Context, address, source string, intent Intent, phase RateAction) (userlogic.EmailAddress, error) {
	if !s.cfg.EmailEnabled {
		return userlogic.EmailAddress{}, ErrUnavailable
	}
	if !intent.valid() || intent.Purpose == userlogic.Unbind {
		return userlogic.EmailAddress{}, ErrInvalid
	}
	email, err := userlogic.NormalizeEmail(address)
	if err != nil {
		return email, err
	}
	ip, err := sourceIP(source)
	if err != nil {
		return email, err
	}
	return email, s.limit.Allow(ctx, phase, email.Address, ip)
}
func (s *Service) SendEmail(ctx context.Context, address, source, locale string, intent Intent) (*Challenge, error) {
	email, err := s.emailInput(ctx, address, source, intent, RateSend)
	if err != nil {
		return nil, err
	}
	if intent.Purpose != userlogic.Login {
		if _, err = s.users.CheckActor(ctx, intent.Actor); err != nil {
			return nil, err
		}
	}
	return s.proofs.Send(ctx, ProductRealm, email.Address, intentBinding(intent), locale, s.mail)
}
func (s *Service) VerifyEmail(ctx context.Context, address, challenge, code, source string, intent Intent) (*Result, error) {
	email, err := s.emailInput(ctx, address, source, intent, RateVerify)
	if err != nil {
		return nil, err
	}
	if err = s.proofs.Verify(ctx, ProductRealm, email.Address, intentBinding(intent), challenge, code); err != nil {
		return nil, err
	}
	// A consumed code is not restored after a database error. Request a new code.
	if intent.Purpose == userlogic.Login {
		return s.users.ResolveOrCreateVerifiedEmail(ctx, email)
	}
	return s.users.ChangeIdentity(ctx, intent.Actor, intent.Purpose, usermodel.IdentityEmail, email, nil)
}
