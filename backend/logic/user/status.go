package user

import (
	"context"
	"errors"
	"math"

	"github.com/0xabcd-k/DexWallet/backend/apperror"
	model "github.com/0xabcd-k/DexWallet/backend/model/user"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

// SetStatusInSession participates in the caller's transaction and audit write.
func (s *Service) SetStatusInSession(ctx context.Context, tx sqlx.Session, id int64, status string) (*model.User, string, error) {
	if tx == nil || id <= 0 || (status != model.StatusActive && status != model.StatusDisabled) {
		return nil, "", ErrInvalidInput
	}
	users := s.users.WithSession(tx)
	u, err := users.FindForUpdate(ctx, id)
	if errors.Is(err, sqlx.ErrNotFound) {
		return nil, "", apperror.New(apperror.NotFound, err)
	}
	if err != nil {
		return nil, "", err
	}
	before := u.Status
	if before != status {
		if u.AuthVersion == math.MaxInt64 {
			return nil, "", ErrVersionExhausted
		}
		u.Status = status
		u.AuthVersion++
		if err = users.Update(ctx, u); err != nil {
			return nil, "", err
		}
	}
	return u, before, nil
}
