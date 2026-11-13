package user

import (
	"context"
	"errors"
	"github.com/0xabcd-k/DexWallet/backend/apperror"
	planmodel "github.com/0xabcd-k/DexWallet/backend/model/transfer"
	model "github.com/0xabcd-k/DexWallet/backend/model/user"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

// Caller is an authorized administrator in the same transaction.
func (s *Service) AssignLimitPlanInSession(ctx context.Context, tx sqlx.Session, id, planID, expectedID int64) (*model.User, int64, error) {
	m := s.users.WithSession(tx)
	u, err := m.FindForUpdate(ctx, id)
	if err != nil {
		return nil, 0, err
	}
	old := u.TransferLimitPlanId
	if old == planID {
		return u, old, nil
	}
	if old != expectedID {
		return nil, old, apperror.New(apperror.Conflict, nil)
	}
	if _, err := planmodel.NewTransferLimitPlanModel(sqlx.NewSqlConnFromSession(tx)).FindOne(ctx, planID); err != nil {
		if errors.Is(err, sqlx.ErrNotFound) {
			return nil, old, apperror.New(apperror.NotFound, err)
		}
		return nil, old, err
	}
	u.TransferLimitPlanId = planID
	return u, old, m.Update(ctx, u)
}
