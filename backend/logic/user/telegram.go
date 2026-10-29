package user

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"time"

	model "github.com/0xabcd-k/DexWallet/backend/model/user"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

// ResolveOrCreateVerifiedTelegram requires server-verified authentication input.
// It resolves current ownership only; this is not a binding or account-merge API.
func (s *Service) ResolveOrCreateVerifiedTelegram(ctx context.Context, profile VerifiedTelegram) (*Resolved, error) {
	if err := profile.validate(); err != nil {
		return nil, err
	}
	var last error
	for attempt := 0; attempt < resolveAttempts; attempt++ {
		hint, err := s.telegrams.FindOneByTelegramUserId(ctx, profile.TelegramUserID)
		if err != nil && !errors.Is(err, model.ErrNotFound) {
			return nil, err
		}
		var owner int64
		if hint != nil && hint.UserId.Valid {
			owner = hint.UserId.Int64
		}
		var result Resolved
		err = s.tx.TransactCtx(ctx, func(ctx context.Context, session sqlx.Session) error {
			users := s.users.WithSession(session)
			identities := s.telegrams.WithSession(session)
			var u *model.User
			var e error
			if owner > 0 {
				u, e = users.FindForUpdate(ctx, owner)
			} else {
				u, e = createUser(ctx, users, initialNickname(profile.FirstName), stringValue(profile.PhotoURL))
			}
			if e != nil {
				return e
			}
			// Insert a missing subject before a locking read to avoid competing
			// gap locks. A duplicate subject rolls back this candidate user.
			if hint == nil {
				_, e = identities.Insert(ctx, &model.TelegramIdentity{TelegramUserId: profile.TelegramUserID, FirstName: profile.FirstName})
				if e != nil {
					return insertIdentityError(e)
				}
			}
			// Locking reads see current ownership, even under REPEATABLE READ.
			identity, e := identities.FindBySubjectForUpdate(ctx, profile.TelegramUserID)
			if e != nil {
				return e
			}
			if owner > 0 {
				if !identity.UserId.Valid || identity.UserId.Int64 != owner {
					return errResolveRace
				}
				if u.Status != model.StatusActive {
					return ErrDisabled
				}
			} else {
				if identity.UserId.Valid {
					return errResolveRace
				}
				if identity.BindingVersion == math.MaxInt64 {
					return ErrVersionExhausted
				}
				identity.UserId = sql.NullInt64{Int64: u.Id, Valid: true}
				identity.BindingVersion++
				identity.BoundAt = sql.NullTime{Time: time.Now().UTC(), Valid: true}
			}

			identity.FirstName = profile.FirstName
			identity.Username = nullString(profile.Username)
			identity.LastName = nullString(profile.LastName)
			identity.PhotoUrl = nullString(profile.PhotoURL)
			identity.LanguageCode = nullString(profile.LanguageCode)
			if e = identities.Update(ctx, identity); e != nil {
				return e
			}

			result = Resolved{User: u, Binding: Binding{Type: model.IdentityTelegram, IdentityID: identity.Id, UserID: u.Id, Version: identity.BindingVersion}}
			return nil
		})
		if err == nil {
			return &result, nil
		}
		if !retryable(err) {
			return nil, err
		}
		last = err
		if err = ctx.Err(); err != nil {
			return nil, err
		}
	}
	return nil, fmt.Errorf("%w: %w", ErrConflict, last)
}

func nullString(value *string) sql.NullString {
	if value == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *value, Valid: true}
}
func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// Product nickname capacity is smaller than the external first-name capacity.
// Only this derived initial display value is shortened; external data is intact.
func initialNickname(value string) string {
	runes := []rune(value)
	if len(runes) > 128 {
		runes = runes[:128]
	}
	return string(runes)
}
