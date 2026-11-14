package backoffice

import (
	"context"
	"github.com/0xabcd-k/DexWallet/backend/apperror"
	"github.com/0xabcd-k/DexWallet/backend/logic/rewardtransfer"
	model "github.com/0xabcd-k/DexWallet/backend/model/rewardtransfer"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

const RewardTransfer Realm = "reward_transfer"
const ActionReviewRewardTransfer = "review_reward_transfer"

func (s *Service) RewardTransferQueue(ctx context.Context, a Actor, r *rewardtransfer.Service, status string, before, limit int64) ([]*model.RewardTransfer, int64, error) {
	if _, err := s.check(ctx, s.store, a, true, false); err != nil {
		return nil, 0, err
	}
	return r.Page(ctx, 0, status, before, limit)
}
func (s *Service) RewardTransferDetail(ctx context.Context, a Actor, r *rewardtransfer.Service, id int64) (*model.RewardTransfer, error) {
	if _, err := s.check(ctx, s.store, a, true, false); err != nil {
		return nil, err
	}
	return r.FindForReview(ctx, id)
}
func (s *Service) ReviewRewardTransfer(ctx context.Context, a Actor, r *rewardtransfer.Service, id int64, decision, reason string) (*model.RewardTransfer, error) {
	if a.Realm != Admin {
		return nil, apperror.New(apperror.PermissionDenied, nil)
	}
	var row *model.RewardTransfer
	err := s.tx.TransactCtx(ctx, func(ctx context.Context, tx sqlx.Session) error {
		m := s.store.session(tx)
		if _, err := s.check(ctx, m, a, true, true); err != nil {
			return err
		}
		var changed bool
		var err error
		row, changed, err = r.ReviewInSession(ctx, tx, id, a.ID, decision, reason)
		if err != nil {
			return err
		}
		if !changed {
			return nil
		}
		return s.audit(ctx, m, a, ActionReviewRewardTransfer, RewardTransfer, &Account{Id: id}, rewardtransfer.PendingReview, row.Status, row.ReviewReason, ResultSuccess)
	})
	if err != nil {
		return nil, err
	}
	return row, nil
}
