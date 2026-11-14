package httpapi

import (
	"fmt"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/types"
	"github.com/0xabcd-k/DexWallet/backend/idcodec"
	"github.com/0xabcd-k/DexWallet/backend/logic/task"
	model "github.com/0xabcd-k/DexWallet/backend/model/task"
	"github.com/0xabcd-k/DexWallet/backend/model/user"
)

func ClaimPreviewData(secret string, c *task.ClaimContext) (types.ClaimPreviewData, error) {
	item, err := TaskData(secret, c.Item)
	if err != nil {
		return types.ClaimPreviewData{}, err
	}
	d := types.ClaimPreviewData{Task: item, Mode: c.Mode, ParticipantStage: c.Stage, TaskRevision: c.Item.Definition.Revision, IdentityRevision: c.Snapshot.IdentityRevision, ProProgress: c.State.Progress.StringFixed(0), Pro: c.State.UnlockedAt.Valid, CanInvite: c.CanInvite}
	usd, points, commission, progress := c.OwnAwards()
	d.RewardUsd, d.TaskPoints, d.CommissionPoints, d.ProDelta = usd.StringFixed(2), points.StringFixed(0), commission.StringFixed(0), progress.StringFixed(0)
	if c.Invitation != nil {
		d.InvitationIdx, err = EncodeIdx(secret, idcodec.TaskInvitation, c.Invitation.Id)
		if err != nil {
			return d, err
		}
		d.InviterIdx, err = EncodeIdx(secret, idcodec.User, c.Inviter.Id)
		d.InviterNickname = c.Inviter.Nickname
	}
	return d, err
}
func InvitationData(secret string, inv *model.TaskInvitation, u *user.User, webURL, telegramURL string) (types.TaskInvitationData, error) {
	d := types.TaskInvitationData{InviterNickname: u.Nickname, InviterAvatarUrl: u.AvatarUrl}
	var err error
	d.Idx, err = EncodeIdx(secret, idcodec.TaskInvitation, inv.Id)
	if err != nil {
		return d, err
	}
	d.TaskIdx, err = EncodeIdx(secret, idcodec.Task, inv.TaskId)
	if err != nil {
		return d, err
	}
	d.InviterIdx, err = EncodeIdx(secret, idcodec.User, u.Id)
	if err != nil {
		return d, err
	}
	d.Path = fmt.Sprintf("/tasks/%s?invitationIdx=%s", d.TaskIdx, d.Idx)
	d.StartParam = "task_" + d.Idx
	if webURL != "" {
		d.Url = webURL + "#" + d.Path
	}
	if telegramURL != "" {
		d.TelegramUrl = telegramURL + "?startapp=" + d.StartParam
	}
	return d, nil
}
func RewardSummaryData(s *task.Summary) types.RewardSummaryData {
	d := types.RewardSummaryData{FirstCompleted: s.State.FirstCompletionID.Valid, ProProgress: s.State.Progress.StringFixed(0), Pro: s.State.UnlockedAt.Valid, FriendCount: s.Totals.Friends, AssistanceCount: s.Totals.AssistanceCount, AssistancePoints: s.Totals.AssistancePoints.StringFixed(0), CommissionPoints: s.Totals.CommissionPoints.StringFixed(0)}
	if s.State.UnlockedAt.Valid {
		d.ProUnlockedAt = TimeString(s.State.UnlockedAt.Time)
	}
	return d
}
func GrantData(secret string, g *model.RewardGrant, c *model.TaskCompletion) (types.RewardGrantData, error) {
	d := types.RewardGrantData{TaskTitle: c.TaskTitle, Reason: g.Reason, RewardUsd: g.RewardUsd.StringFixed(2), Points: g.Points.StringFixed(0), ProDelta: g.ProDelta.StringFixed(0), ProBefore: g.ProBefore.StringFixed(0), ProAfter: g.ProAfter.StringFixed(0), WasPro: g.WasPro != 0, UnlockedPro: g.UnlockedPro != 0, CreatedAt: TimeString(g.CreatedAt)}
	var err error
	d.Idx, err = EncodeIdx(secret, idcodec.RewardGrant, g.Id)
	if err != nil {
		return d, err
	}
	d.CompletionIdx, err = EncodeIdx(secret, idcodec.TaskCompletion, g.CompletionId)
	if err != nil {
		return d, err
	}
	d.TaskIdx, err = EncodeIdx(secret, idcodec.Task, c.TaskId)
	if err != nil {
		return d, err
	}
	d.VoucherIdx, err = EncodeIdx(secret, idcodec.LedgerVoucher, g.VoucherId.Int64)
	return d, err
}
func CompletionData(secret string, c *task.Completion) (types.TaskCompletionData, error) {
	d := types.TaskCompletionData{TaskTitle: c.Row.TaskTitle, Mode: c.Row.Mode, ParticipantStage: c.Row.ParticipantStage, CompletedAt: TimeString(c.Row.CreatedAt), Grants: []types.RewardGrantData{}}
	var err error
	d.Idx, err = EncodeIdx(secret, idcodec.TaskCompletion, c.Row.Id)
	if err != nil {
		return d, err
	}
	d.TaskIdx, err = EncodeIdx(secret, idcodec.Task, c.Row.TaskId)
	if err != nil {
		return d, err
	}
	for _, g := range c.Grants {
		grant, err := GrantData(secret, g, c.Row)
		if err != nil {
			return d, err
		}
		d.Grants = append(d.Grants, grant)
	}
	return d, nil
}
