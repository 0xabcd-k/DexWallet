package user

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"sort"

	model "github.com/0xabcd-k/DexWallet/backend/model/user"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

type Purpose string

const (
	Login   Purpose = "login"
	Bind    Purpose = "bind"
	Replace Purpose = "replace"
	Unbind  Purpose = "unbind"
)

type Actor struct{ UserID, AuthVersion int64 }

var (
	ErrStaleSession       = errors.New("user session version changed")
	ErrOccupied           = errors.New("external identity belongs to another user")
	ErrNeedSecondIdentity = errors.New("another identity is required")
	ErrUseReplace         = errors.New("identity type already bound; use replace")
)

func (s *Service) CheckActorInSession(ctx context.Context, session sqlx.Session, actor Actor) (*model.User, error) {
	if actor.UserID <= 0 || actor.AuthVersion <= 0 {
		return nil, ErrInvalidInput
	}
	u, err := s.users.WithSession(session).FindForUpdate(ctx, actor.UserID)
	if err != nil {
		return nil, err
	}
	if u.Status != model.StatusActive {
		return nil, ErrDisabled
	}
	if u.AuthVersion != actor.AuthVersion {
		return nil, ErrStaleSession
	}
	return u, nil
}

// ChangeIdentityInSession only accepts authenticated actors and verified targets.
// It never authenticates a caller from the supplied numeric actor ID.
func (s *Service) ChangeIdentityInSession(ctx context.Context, session sqlx.Session, actor Actor, purpose Purpose, kind model.IdentityType, email string, tg *VerifiedTelegram) (*Resolved, error) {
	if session == nil || (purpose != Bind && purpose != Replace && purpose != Unbind) || (kind != model.IdentityEmail && kind != model.IdentityTelegram) {
		return nil, ErrInvalidInput
	}
	u, err := s.CheckActorInSession(ctx, session, actor)
	if err != nil {
		return nil, err
	}
	emails, telegrams := s.emails.WithSession(session), s.telegrams.WithSession(session)
	owner := sql.NullInt64{Int64: u.Id, Valid: true}
	e, err := emails.FindOneByUserId(ctx, owner)
	if err != nil && !errors.Is(err, model.ErrNotFound) {
		return nil, err
	}
	t, err := telegrams.FindOneByUserId(ctx, owner)
	if err != nil && !errors.Is(err, model.ErrNotFound) {
		return nil, err
	}
	count := 0
	var oldID int64
	if e != nil {
		count++
		if kind == model.IdentityEmail {
			oldID = e.Id
		}
	}
	if t != nil {
		count++
		if kind == model.IdentityTelegram {
			oldID = t.Id
		}
	}
	if purpose == Replace && (count < 2 || oldID == 0) {
		return nil, ErrNeedSecondIdentity
	}
	if purpose == Unbind && (count < 2 || oldID == 0) {
		return nil, ErrNeedSecondIdentity
	}
	targetID := oldID
	if purpose != Unbind {
		switch kind {
		case model.IdentityEmail:
			address, err := NormalizeEmail(email)
			if err != nil {
				return nil, err
			}
			target, err := emails.FindOneByEmailAddress(ctx, address.Address)
			if errors.Is(err, model.ErrNotFound) {
				result, err := emails.Insert(ctx, &model.EmailIdentity{EmailAddress: address.Address, EmailLocalPart: address.LocalPart, EmailDomain: address.Domain, EmailOriginal: address.Original})
				if err != nil {
					return nil, insertIdentityError(err)
				}
				targetID, err = result.LastInsertId()
				if err != nil {
					return nil, err
				}
			} else if err != nil {
				return nil, err
			} else {
				targetID = target.Id
			}
		case model.IdentityTelegram:
			if tg == nil {
				return nil, ErrInvalidInput
			}
			if err = tg.validate(); err != nil {
				return nil, err
			}
			target, err := telegrams.FindOneByTelegramUserId(ctx, tg.TelegramUserID)
			if errors.Is(err, model.ErrNotFound) {
				result, err := telegrams.Insert(ctx, &model.TelegramIdentity{TelegramUserId: tg.TelegramUserID, FirstName: tg.FirstName})
				if err != nil {
					return nil, insertIdentityError(err)
				}
				targetID, err = result.LastInsertId()
				if err != nil {
					return nil, err
				}
			} else if err != nil {
				return nil, err
			} else {
				targetID = target.Id
			}
		}
	}
	ids := []int64{targetID}
	if oldID > 0 && oldID != targetID {
		ids = append(ids, oldID)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	locked := make(map[int64]*model.BindingRow, len(ids))
	for _, id := range ids {
		row, err := model.LockBinding(ctx, session, kind, id)
		if err != nil {
			return nil, err
		}
		locked[id] = row
	}
	target := locked[targetID]
	if target.UserId.Valid && target.UserId.Int64 != u.Id {
		return nil, ErrOccupied
	}
	if oldID > 0 && (!locked[oldID].UserId.Valid || locked[oldID].UserId.Int64 != u.Id) {
		return nil, ErrBindingChanged
	}
	if purpose == Bind && oldID > 0 && oldID != targetID {
		return nil, ErrUseReplace
	}
	changed := purpose == Unbind || oldID != targetID
	if changed {
		for _, row := range locked {
			if row.BindingVersion == math.MaxInt64 {
				return nil, ErrVersionExhausted
			}
		}
		if oldID > 0 {
			if err = model.ChangeBinding(ctx, session, kind, locked[oldID], sql.NullInt64{}); err != nil {
				return nil, err
			}
		}
		if purpose != Unbind {
			if err = model.ChangeBinding(ctx, session, kind, target, owner); err != nil {
				return nil, err
			}
		}
		target.BindingVersion++
		if purpose == Replace || purpose == Unbind {
			if u.AuthVersion == math.MaxInt64 {
				return nil, ErrVersionExhausted
			}
			u.AuthVersion++
			if err = s.users.WithSession(session).Update(ctx, u); err != nil {
				return nil, err
			}
		}
	}
	if kind == model.IdentityTelegram && purpose != Unbind {
		profile, err := telegrams.FindOne(ctx, targetID)
		if err != nil {
			return nil, err
		}
		profile.FirstName = tg.FirstName
		profile.Username = nullString(tg.Username)
		profile.LastName = nullString(tg.LastName)
		profile.PhotoUrl = nullString(tg.PhotoURL)
		profile.LanguageCode = nullString(tg.LanguageCode)
		if err = telegrams.Update(ctx, profile); err != nil {
			return nil, err
		}
	}
	return &Resolved{User: u, Binding: Binding{Type: kind, IdentityID: targetID, UserID: u.Id, Version: target.BindingVersion}}, nil
}
