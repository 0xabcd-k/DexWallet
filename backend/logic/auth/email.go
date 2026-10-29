package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"time"

	userlogic "github.com/0xabcd-k/DexWallet/backend/logic/user"
	model "github.com/0xabcd-k/DexWallet/backend/model/auth"
	usermodel "github.com/0xabcd-k/DexWallet/backend/model/user"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

type Challenge struct {
	Token     string
	ExpiresAt time.Time
}

func (s *Service) SendEmail(ctx context.Context, address, source, locale string, intent Intent) (*Challenge, error) {
	if !s.cfg.EmailEnabled {
		return nil, ErrUnavailable
	}
	if !intent.valid() || intent.Purpose == userlogic.Unbind {
		return nil, ErrInvalid
	}
	normalized, err := userlogic.NormalizeEmail(address)
	if err != nil {
		return nil, err
	}
	ip, err := sourceIP(source)
	if err != nil {
		return nil, err
	}
	if err = s.limit.Allow(ctx, RateSend, normalized.Address, ip); err != nil {
		return nil, err
	}
	number, err := rand.Int(rand.Reader, big.NewInt(CodeSpace))
	if err != nil {
		return nil, err
	}
	code := fmt.Sprintf("%06d", number.Int64())
	challenge, err := token()
	if err != nil {
		return nil, err
	}
	now := s.now()
	expires := now.Add(CodeLifetime)
	err = s.tx.TransactCtx(ctx, func(ctx context.Context, session sqlx.Session) error {
		challenges := s.challenges.WithSession(session)
		if err := challenges.Reserve(ctx, normalized.Address); err != nil {
			return err
		}
		row, err := challenges.Lock(ctx, normalized.Address)
		if err != nil {
			return err
		}
		if now.Before(row.IssuedAt.Add(ResendInterval)) {
			return ErrRateLimited
		}
		if intent.Purpose != userlogic.Login {
			if _, err = s.users.CheckActorInSession(ctx, session, intent.Actor); err != nil {
				return err
			}
		}
		row.ChallengeToken = challenge
		row.CodeDigest = s.digest("code", challenge, code)
		row.Purpose = string(intent.Purpose)
		row.ActorId = intent.Actor.UserID
		row.ActorVersion = intent.Actor.AuthVersion
		row.Status = model.ChallengePending
		row.Attempts = 0
		row.IssuedAt = now
		row.ExpiresAt = expires
		return challenges.Update(ctx, row)
	})
	if err != nil {
		return nil, err
	}
	err = s.mail.SendCode(ctx, normalized.Address, code, locale)
	state := model.ChallengeActive
	if err != nil {
		state = model.ChallengeFailed
	}
	// Finish delivery state even when the request timed out, within a small budget.
	update, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	deliveryErr := s.challenges.SetDelivery(update, normalized.Address, challenge, state)
	if err != nil || deliveryErr != nil {
		return nil, errors.Join(err, deliveryErr)
	}
	return &Challenge{Token: challenge, ExpiresAt: expires}, nil
}
func (s *Service) VerifyEmail(ctx context.Context, request, address, challenge, code, source string, intent Intent) (*Result, error) {
	if !s.cfg.EmailEnabled {
		return nil, ErrUnavailable
	}
	if !intent.valid() || intent.Purpose == userlogic.Unbind || !validToken(challenge) || len(code) != 6 {
		return nil, ErrInvalid
	}
	for _, c := range code {
		if c < '0' || c > '9' {
			return nil, ErrInvalid
		}
	}
	normalized, err := userlogic.NormalizeEmail(address)
	if err != nil {
		return nil, err
	}
	ip, err := sourceIP(source)
	if err != nil {
		return nil, err
	}
	if err = s.limit.Allow(ctx, RateVerify, normalized.Address, ip); err != nil {
		return nil, err
	}
	input := s.digest("email", intent, normalized.Address, challenge, code)
	return s.run(ctx, request, intent, usermodel.IdentityEmail, input, func(ctx context.Context, session sqlx.Session) (*userlogic.Resolved, string, error) {
		challenges := s.challenges.WithSession(session)
		row, err := challenges.Lock(ctx, normalized.Address)
		if errors.Is(err, model.ErrNotFound) {
			return nil, "", ErrCode
		}
		if err != nil {
			return nil, "", err
		}
		if row.ChallengeToken != challenge || row.Purpose != string(intent.Purpose) || row.ActorId != intent.Actor.UserID || row.ActorVersion != intent.Actor.AuthVersion || row.Status != model.ChallengeActive || !s.now().Before(row.ExpiresAt) || row.Attempts >= MaxCodeAttempts {
			return nil, "", ErrCode
		}
		if !hmac.Equal([]byte(row.CodeDigest), []byte(s.digest("code", challenge, code))) {
			row.Attempts++
			if err = challenges.Update(ctx, row); err != nil {
				return nil, "", err
			}
			return nil, "", ErrCode
		}
		var result *userlogic.Resolved
		if intent.Purpose == userlogic.Login {
			result, err = s.users.ResolveOrCreateVerifiedEmailInSession(ctx, session, normalized)
		} else {
			result, err = s.users.ChangeIdentityInSession(ctx, session, intent.Actor, intent.Purpose, usermodel.IdentityEmail, normalized.Original, nil)
		}
		if err != nil {
			return nil, "", err
		}
		row.Status = model.ChallengeConsumed
		if err = challenges.Update(ctx, row); err != nil {
			return nil, "", err
		}
		return result, s.digest("email-proof", challenge), nil
	})
}
