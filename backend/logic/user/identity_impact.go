package user

import (
	"context"
	"errors"
	model "github.com/0xabcd-k/DexWallet/backend/model/user"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
	"time"
)

const ReplacementRestriction = 24 * time.Hour
const InitialTransferPlanID int64 = 1

var ErrHistoryUnknown = errors.New("identity history requires verified backfill before binding")

type IdentityApproval struct {
	Revision    int64
	Replacement bool
}
type IdentityImpact struct {
	Revision        int64
	Replacement     bool
	RestrictedUntil time.Time
}

func replacement(u *model.User, kind model.IdentityType, targetID int64) (bool, error) {
	last := u.LastEmailIdentityId
	if kind == model.IdentityTelegram {
		last = u.LastTelegramIdentityId
	}
	if last < 0 {
		return false, ErrHistoryUnknown
	}
	return last > 0 && last != targetID, nil
}
func remember(u *model.User, kind model.IdentityType, id int64) {
	value := id
	if kind == model.IdentityEmail {
		u.LastEmailIdentityId = value
	} else {
		u.LastTelegramIdentityId = value
	}
}

// Preview reads a consistent identity baseline. It creates no proof or identity.
func (s *Service) IdentityImpact(ctx context.Context, actor Actor, purpose Purpose, kind model.IdentityType, address EmailAddress, tg *VerifiedTelegram) (*IdentityImpact, error) {
	if (purpose != Bind && purpose != Replace) || (kind != model.IdentityEmail && kind != model.IdentityTelegram) {
		return nil, ErrInvalidInput
	}
	var impact *IdentityImpact
	err := s.tx.TransactCtx(ctx, func(ctx context.Context, tx sqlx.Session) error {
		u, err := s.CheckActorInSession(ctx, tx, actor)
		if err != nil {
			return err
		}
		var targetID int64
		switch kind {
		case model.IdentityEmail:
			row, e := s.emails.WithSession(tx).FindOneByEmailAddress(ctx, address.Address)
			if e != nil && !errors.Is(e, model.ErrNotFound) {
				return e
			}
			if row != nil {
				targetID = row.Id
			}
		case model.IdentityTelegram:
			if tg == nil {
				return ErrInvalidInput
			}
			if err = tg.validate(); err != nil {
				return err
			}
			row, e := s.telegrams.WithSession(tx).FindOneByTelegramUserId(ctx, tg.TelegramUserID)
			if e != nil && !errors.Is(e, model.ErrNotFound) {
				return e
			}
			if row != nil {
				targetID = row.Id
			}
		}
		// Preview reveals only this user's replacement impact. Whether a
		// target belongs to another user is checked after proof verification
		// in the mutation, not exposed to an arbitrary email lookup.

		replace, err := replacement(u, kind, targetID)
		if err != nil {
			return err
		}
		impact = &IdentityImpact{Revision: u.IdentityRevision, Replacement: replace}
		if u.PaymentRestrictedUntil.Valid {
			impact.RestrictedUntil = u.PaymentRestrictedUntil.Time.UTC()
		}
		return nil
	})
	return impact, err
}
