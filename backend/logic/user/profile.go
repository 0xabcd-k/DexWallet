package user

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	model "github.com/0xabcd-k/DexWallet/backend/model/user"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

const NicknameMaxRunes = 128

func (s *Service) FindIdentitiesForActor(ctx context.Context, actor Actor) (*Identities, error) {
	var result *Identities
	err := s.tx.TransactCtx(ctx, func(ctx context.Context, tx sqlx.Session) error {
		if _, err := s.CheckActorInSession(ctx, tx, actor); err != nil {
			return err
		}
		var err error
		result, err = s.identitiesInSession(ctx, tx, actor.UserID)
		return err
	})
	return result, err
}

func (s *Service) identitiesInSession(ctx context.Context, tx sqlx.Session, id int64) (*Identities, error) {
	owner := sql.NullInt64{Int64: id, Valid: true}
	email, err := s.emails.WithSession(tx).FindOneByUserId(ctx, owner)
	if err != nil && !errors.Is(err, model.ErrNotFound) {
		return nil, err
	}
	telegram, err := s.telegrams.WithSession(tx).FindOneByUserId(ctx, owner)
	if err != nil && !errors.Is(err, model.ErrNotFound) {
		return nil, err
	}
	return &Identities{Email: email, Telegram: telegram}, nil
}

// Profile changes preserve external identity snapshots and authentication version.
func (s *Service) UpdateNickname(ctx context.Context, actor Actor, nickname string) (*model.User, error) {
	nickname = strings.TrimSpace(nickname)
	if nickname == "" || !validText(nickname, NicknameMaxRunes) {
		return nil, ErrInvalidInput
	}
	var result *model.User
	err := s.tx.TransactCtx(ctx, func(ctx context.Context, tx sqlx.Session) error {
		u, err := s.CheckActorInSession(ctx, tx, actor)
		if err != nil {
			return err
		}
		u.Nickname = nickname
		if err = s.users.WithSession(tx).Update(ctx, u); err != nil {
			return err
		}
		result = u
		return nil
	})
	return result, err
}
