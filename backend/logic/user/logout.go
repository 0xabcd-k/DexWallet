package user

import (
	"context"
	"math"

	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

// LogoutAll checks the latest actor under a row lock, including when ordinary
// request authentication accepted a briefly stale cache entry.
func (s *Service) LogoutAll(ctx context.Context, actor Actor) error {
	return s.tx.TransactCtx(ctx, func(ctx context.Context, tx sqlx.Session) error {
		u, err := s.CheckActorInSession(ctx, tx, actor)
		if err != nil {
			return err
		}
		if u.AuthVersion == math.MaxInt64 {
			return ErrVersionExhausted
		}
		u.AuthVersion++
		return s.users.WithSession(tx).Update(ctx, u)
	})
}
