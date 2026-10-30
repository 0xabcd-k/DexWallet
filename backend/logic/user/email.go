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

// ResolveOrCreateVerifiedEmail requires server-verified authentication input.
// It resolves current ownership only; this is not a binding or account-merge API.
func (s *Service) ResolveOrCreateVerifiedEmail(ctx context.Context, original string) (*Resolved, error) {
	address, err := NormalizeEmail(original)
	if err != nil {
		return nil, err
	}
	var last error
	for attempt := 0; attempt < resolveAttempts; attempt++ {
		hint, err := s.emails.FindOneByEmailAddress(ctx, address.Address)
		if err != nil && !errors.Is(err, model.ErrNotFound) {
			return nil, err
		}
		var owner int64
		if hint != nil && hint.UserId.Valid {
			owner = hint.UserId.Int64
		}
		var result Resolved
		err = s.tx.TransactCtx(ctx, func(ctx context.Context, session sqlx.Session) error {
			resolved, e := s.resolveEmailInSession(ctx, session, address, hint, owner)
			if e == nil {
				result = *resolved
			}
			return e
		})
		if err == nil {
			return &result, nil
		}
		if !retryableResolution(err) {
			return nil, err
		}
		last = err
		if err = ctx.Err(); err != nil {
			return nil, err
		}
	}
	return nil, fmt.Errorf("%w: %w", ErrConflict, last)
}

func (s *Service) resolveEmailInSession(ctx context.Context, session sqlx.Session, address EmailAddress, hint *model.EmailIdentity, owner int64) (*Resolved, error) {
	users := s.users.WithSession(session)
	identities := s.emails.WithSession(session)
	var u *model.User
	var e error
	if owner > 0 {
		u, e = users.FindForUpdate(ctx, owner)
	} else {
		u, e = createUser(ctx, users, "", "")
	}
	if e != nil {
		return nil, e
	}
	// Insert a missing subject before a locking read to avoid competing
	// gap locks. A duplicate subject rolls back this candidate user.
	if hint == nil {
		_, e = identities.Insert(ctx, &model.EmailIdentity{EmailAddress: address.Address, EmailLocalPart: address.LocalPart, EmailDomain: address.Domain, EmailOriginal: address.Original})
		if e != nil {
			return nil, insertIdentityError(e)
		}
	}
	// Locking reads see current ownership, even under REPEATABLE READ.
	identity, e := identities.FindBySubjectForUpdate(ctx, address.Address)
	if e != nil {
		return nil, e
	}
	if owner > 0 {
		if !identity.UserId.Valid || identity.UserId.Int64 != owner {
			return nil, errResolveRace
		}
		if u.Status != model.StatusActive {
			return nil, ErrDisabled
		}
	} else {
		if identity.UserId.Valid {
			return nil, errResolveRace
		}
		if identity.BindingVersion == math.MaxInt64 {
			return nil, ErrVersionExhausted
		}
		identity.UserId = sql.NullInt64{Int64: u.Id, Valid: true}
		identity.BindingVersion++
		identity.BoundAt = sql.NullTime{Time: time.Now().UTC(), Valid: true}
		if e = identities.Update(ctx, identity); e != nil {
			return nil, e
		}
	}

	result := Resolved{User: u, Binding: Binding{Type: model.IdentityEmail, IdentityID: identity.Id, UserID: u.Id, Version: identity.BindingVersion}}
	return &result, nil
}
