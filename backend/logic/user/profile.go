package user

import (
	"context"
	"strings"

	model "github.com/0xabcd-k/DexWallet/backend/model/user"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

const NicknameMaxRunes = 128

func (s *Service) FindIdentitiesForActor(ctx context.Context, actor Actor) (*Identities, error) {
	if _, err := s.CheckActor(ctx, actor); err != nil {
		return nil, err
	}
	return s.FindCurrentIdentities(ctx, actor.UserID)
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
