package httpapi

import (
	"github.com/0xabcd-k/DexWallet/backend/api/internal/types"
	"github.com/0xabcd-k/DexWallet/backend/idcodec"
	model "github.com/0xabcd-k/DexWallet/backend/model/rewardtransfer"
)

func RewardTransferData(secret string, r *model.RewardTransfer) (types.RewardTransferData, error) {
	d := types.RewardTransferData{Amount: r.Amount.StringFixed(2), Fee: "0.00", Status: r.Status, ReviewReason: r.ReviewReason, CreatedAt: TimeString(r.CreatedAt)}
	var err error
	d.Idx, err = EncodeIdx(secret, idcodec.RewardTransfer, r.Id)
	if err != nil {
		return d, err
	}
	d.UserIdx, err = EncodeIdx(secret, idcodec.User, r.UserId)
	if r.ResolvedAt.Valid {
		d.ResolvedAt = TimeString(r.ResolvedAt.Time)
	}
	return d, err
}
func RewardTransferAdminData(secret string, r *model.RewardTransfer) (types.RewardTransferAdminData, error) {
	d := types.RewardTransferAdminData{RiskDecision: r.RiskDecision, RiskVersion: r.RiskVersion, RiskReason: r.RiskReason}
	var err error
	d.Transfer, err = RewardTransferData(secret, r)
	if err != nil {
		return d, err
	}
	if r.ReviewerId.Valid {
		d.ReviewerIdx, err = EncodeIdx(secret, idcodec.Admin, r.ReviewerId.Int64)
	}
	return d, err
}
func RewardTransferListData(secret string, rows []*model.RewardTransfer, next int64) (types.RewardTransferListData, error) {
	d := types.RewardTransferListData{Items: []types.RewardTransferData{}}
	for _, r := range rows {
		item, err := RewardTransferData(secret, r)
		if err != nil {
			return d, err
		}
		d.Items = append(d.Items, item)
	}
	var err error
	if next != 0 {
		d.NextCursor, err = EncodeIdx(secret, idcodec.RewardTransfer, next)
	}
	return d, err
}
