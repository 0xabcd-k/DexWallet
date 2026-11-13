package backoffice

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/0xabcd-k/DexWallet/backend/apperror"
	"github.com/0xabcd-k/DexWallet/backend/logic/transfer"
	model "github.com/0xabcd-k/DexWallet/backend/model/transfer"
	usermodel "github.com/0xabcd-k/DexWallet/backend/model/user"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

const (
	ActionCreateLimitPlan       = "create_limit_plan"
	ActionAssignLimitPlan       = "assign_limit_plan"
	LimitPlan             Realm = "limit_plan"
)

func planSnapshot(p *model.TransferLimitPlan) string {
	b, _ := json.Marshal(map[string]any{"id": p.Id, "name": p.Name, "min": p.MinAmount.StringFixed(2), "max": p.MaxAmount.StringFixed(2), "daily": p.DailyOutgoingLimit.StringFixed(2)})
	return string(b)
}
func (s *Service) LimitPlans(ctx context.Context, a Actor, page int64) ([]model.TransferLimitPlan, error) {
	if page < 1 || page > 1000000 {
		return nil, apperror.New(apperror.InvalidArgument, nil)
	}
	if _, err := s.check(ctx, s.store, a, true, false); err != nil {
		return nil, err
	}
	rows := []model.TransferLimitPlan{}
	err := s.store.conn.QueryRowsCtx(ctx, &rows, "SELECT * FROM transfer_limit_plan ORDER BY id DESC LIMIT ? OFFSET ?", PageSize, (page-1)*PageSize)
	return rows, err
}
func (s *Service) CreateLimitPlan(ctx context.Context, a Actor, p model.TransferLimitPlan) (*model.TransferLimitPlan, error) {
	p.Name = strings.TrimSpace(p.Name)
	if a.Realm != Admin || !transfer.ValidRequestKey(p.RequestKey) {
		return nil, apperror.New(apperror.InvalidArgument, nil)
	}
	if err := transfer.ValidatePlan(&p); err != nil {
		return nil, err
	}
	p.CreatorId = a.ID
	var result *model.TransferLimitPlan
	err := s.tx.TransactCtx(ctx, func(ctx context.Context, tx sqlx.Session) error {
		m := s.store.session(tx)
		if _, err := s.check(ctx, m, a, true, true); err != nil {
			return err
		}
		plans := model.NewTransferLimitPlanModel(m.conn)
		original, err := plans.FindOneByCreatorIdRequestKey(ctx, a.ID, p.RequestKey)
		if err == nil {
			if original.Name != p.Name || !original.MinAmount.Equal(p.MinAmount) || !original.MaxAmount.Equal(p.MaxAmount) || !original.DailyOutgoingLimit.Equal(p.DailyOutgoingLimit) {
				return apperror.New(apperror.Conflict, nil)
			}
			result = original
			return nil
		}
		if !errors.Is(err, model.ErrNotFound) {
			return err
		}
		inserted, err := plans.Insert(ctx, &p)
		if err != nil {
			return err
		}
		p.Id, err = inserted.LastInsertId()
		if err != nil {
			return err
		}
		result = &p
		return s.audit(ctx, m, a, ActionCreateLimitPlan, LimitPlan, &Account{Id: p.Id}, "", planSnapshot(&p), "", ResultSuccess)
	})
	return result, err
}
func (s *Service) UserLimitPolicy(ctx context.Context, a Actor, id int64) (*transfer.Policy, error) {
	var p *transfer.Policy
	err := s.tx.TransactCtx(ctx, func(ctx context.Context, tx sqlx.Session) error {
		m := s.store.session(tx)
		if _, err := s.check(ctx, m, a, true, true); err != nil {
			return err
		}
		u, err := usermodel.NewUserModel(m.conn).FindForUpdate(ctx, id)
		if errors.Is(err, sqlx.ErrNotFound) {
			return apperror.New(apperror.NotFound, err)
		}
		if err != nil {
			return err
		}
		p, _, err = transfer.ReadPolicy(ctx, m.conn, u)
		return err
	})
	return p, err
}
func (s *Service) AssignLimitPlan(ctx context.Context, a Actor, id, planID, expectedID int64, reason string) (*transfer.Policy, error) {
	if !reasonOK(reason) || planID <= 0 || expectedID <= 0 {
		return nil, apperror.New(apperror.InvalidArgument, nil)
	}
	var p *transfer.Policy
	err := s.tx.TransactCtx(ctx, func(ctx context.Context, tx sqlx.Session) error {
		m := s.store.session(tx)
		if _, err := s.check(ctx, m, a, true, true); err != nil {
			return err
		}
		u, old, err := s.users.AssignLimitPlanInSession(ctx, tx, id, planID, expectedID)
		if errors.Is(err, sqlx.ErrNotFound) {
			return apperror.New(apperror.NotFound, err)
		}
		if err != nil {
			return err
		}
		p, _, err = transfer.ReadPolicy(ctx, m.conn, u)
		if err != nil {
			return err
		}
		if old == planID {
			return nil
		}
		before, err := model.NewTransferLimitPlanModel(m.conn).FindOne(ctx, old)
		if err != nil {
			return err
		}
		return s.audit(ctx, m, a, ActionAssignLimitPlan, Product, &Account{Id: id}, planSnapshot(before), planSnapshot(p.Plan), reason, ResultSuccess)
	})
	return p, err
}
