// Package auth validates external credentials and commits their use with the
// product identity. Public sessions and HTTP authentication belong to phase 2.3.
package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"time"

	userlogic "github.com/0xabcd-k/DexWallet/backend/logic/user"
	model "github.com/0xabcd-k/DexWallet/backend/model/auth"
	usermodel "github.com/0xabcd-k/DexWallet/backend/model/user"
	"github.com/go-sql-driver/mysql"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

var (
	ErrInvalid         = errors.New("invalid authentication input")
	ErrCode            = errors.New("invalid or expired verification code")
	ErrTelegram        = errors.New("invalid or expired Telegram authorization")
	ErrReplay          = errors.New("authorization material already used")
	ErrRequestConflict = errors.New("request token reused with different input")
	ErrExpired         = errors.New("authentication operation expired")
	ErrRateLimited     = errors.New("authentication rate limited")
	ErrUnavailable     = errors.New("authentication provider unavailable")
)

// Result carries the stable operation ID and original event time for phase 2.3
// to issue/recover one session without treating a retry as new authentication.
type Result struct {
	*userlogic.Resolved
	OperationID int64
	CreatedAt   time.Time
}

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
	cfg        Config
	users      *userlogic.Service
	challenges model.EmailChallengeModel
	operations model.AuthOperationModel
	tx         userlogic.Transactor
	mail       Mailer
	limit      Limiter
	now        func() time.Time
}

func New(cfg Config, users *userlogic.Service, challenges model.EmailChallengeModel, operations model.AuthOperationModel, tx userlogic.Transactor, mail Mailer, limit Limiter) (*Service, error) {
	if err := cfg.Check(); err != nil {
		return nil, err
	}
	if users == nil || challenges == nil || operations == nil || tx == nil || limit == nil || (cfg.EmailEnabled && mail == nil) {
		return nil, ErrUnavailable
	}
	return &Service{cfg: cfg, users: users, challenges: challenges, operations: operations, tx: tx, mail: mail, limit: limit, now: func() time.Time { return time.Now().UTC() }}, nil
}
func (s *Service) digest(parts ...any) string {
	data, _ := json.Marshal(parts)
	h := hmac.New(sha256.New, []byte(s.cfg.Secret))
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}
func token() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
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

// run commits credential consumption, identity changes and the recovery receipt
// together. Failed-code counts are the one domain failure intentionally committed.
func (s *Service) run(ctx context.Context, request string, intent Intent, kind usermodel.IdentityType, input string, work func(context.Context, sqlx.Session) (*userlogic.Resolved, string, error)) (*Result, error) {
	if !validToken(request) || !intent.valid() {
		return nil, ErrInvalid
	}
	var last error
	for attempt := 0; attempt < resolveAttempts; attempt++ {
		var result *Result
		var denied error
		err := s.tx.TransactCtx(ctx, func(ctx context.Context, session sqlx.Session) error {
			ops := s.operations.WithSession(session)
			op := &model.AuthOperation{RequestDigest: s.digest("request", request), InputDigest: input, Purpose: string(intent.Purpose), IdentityType: string(kind), ActorId: intent.Actor.UserID, ActorVersion: intent.Actor.AuthVersion, ExpiresAt: s.now().Add(OperationLifetime)}
			if err := ops.Reserve(ctx, op); err != nil {
				return err
			}
			existing, err := ops.Lock(ctx, op.RequestDigest)
			if err != nil {
				return err
			}
			if existing.InputDigest != input {
				return ErrRequestConflict
			}
			if !s.now().Before(existing.ExpiresAt) {
				return ErrExpired
			}
			if existing.Completed {
				u, err := s.users.CheckActorInSession(ctx, session, userlogic.Actor{UserID: existing.UserId, AuthVersion: existing.AuthVersion})
				if err != nil {
					return err
				}
				binding := userlogic.Binding{Type: kind, IdentityID: existing.IdentityId, UserID: u.Id, Version: existing.BindingVersion}
				if intent.Purpose != userlogic.Unbind {
					if err = s.users.CheckCurrentBindingInSession(ctx, session, binding); err != nil {
						return err
					}
				}
				result = &Result{Resolved: &userlogic.Resolved{User: u, Binding: binding}, OperationID: existing.Id, CreatedAt: existing.CreatedAt}
				return nil
			}
			value, proof, err := work(ctx, session)
			if errors.Is(err, ErrCode) {
				denied = err
				return ops.Delete(ctx, existing.Id)
			}
			if err != nil {
				return err
			}
			existing.UserId = value.User.Id
			existing.IdentityId = value.Binding.IdentityID
			existing.BindingVersion = value.Binding.Version
			existing.AuthVersion = value.User.AuthVersion
			existing.Completed = true
			if proof != "" {
				existing.ProofDigest.String = proof
				existing.ProofDigest.Valid = true
			}
			if err = ops.Update(ctx, existing); err != nil {
				var duplicate *mysql.MySQLError
				if errors.As(err, &duplicate) && duplicate.Number == 1062 {
					return ErrReplay
				}
				return err
			}
			result = &Result{Resolved: value, OperationID: existing.Id, CreatedAt: existing.CreatedAt}
			return nil
		})
		if err == nil {
			if denied != nil {
				return nil, denied
			}
			return result, nil
		}
		last = err
		if !userlogic.RetryableResolution(err) {
			return nil, err
		}
	}
	return nil, fmt.Errorf("%w: %w", userlogic.ErrConflict, last)
}
func (s *Service) Unbind(ctx context.Context, request string, actor userlogic.Actor, kind usermodel.IdentityType) (*Result, error) {
	if kind != usermodel.IdentityEmail && kind != usermodel.IdentityTelegram {
		return nil, ErrInvalid
	}
	intent := Intent{Purpose: userlogic.Unbind, Actor: actor}
	return s.run(ctx, request, intent, kind, s.digest("unbind", intent, kind), func(ctx context.Context, session sqlx.Session) (*userlogic.Resolved, string, error) {
		result, err := s.users.ChangeIdentityInSession(ctx, session, actor, userlogic.Unbind, kind, "", nil)
		return result, "", err
	})
}
