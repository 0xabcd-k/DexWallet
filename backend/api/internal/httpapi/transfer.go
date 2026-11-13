package httpapi

import (
	"context"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/types"
	"github.com/0xabcd-k/DexWallet/backend/apperror"
	"github.com/0xabcd-k/DexWallet/backend/idcodec"
	"github.com/0xabcd-k/DexWallet/backend/logic/transfer"
	model "github.com/0xabcd-k/DexWallet/backend/model/transfer"
	"time"
)

func EncodeIdx(secret string, kind idcodec.Kind, id int64) (string, error) {
	c, err := idcodec.New(secret, kind)
	if err != nil {
		return "", err
	}
	return c.Encode(id)
}
func DecodeIdx(secret string, kind idcodec.Kind, idx string) (int64, error) {
	c, err := idcodec.New(secret, kind)
	if err != nil {
		return 0, err
	}
	id, err := c.Decode(idx)
	if err != nil {
		return 0, apperror.New(apperror.InvalidArgument, err)
	}
	return id, nil
}
func TimeString(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}
func LimitPlanData(secret string, p *model.TransferLimitPlan) (types.TransferLimitPlan, error) {
	idx, err := EncodeIdx(secret, idcodec.TransferLimitPlan, p.Id)
	return types.TransferLimitPlan{Idx: idx, Name: p.Name, MinAmount: p.MinAmount.StringFixed(2), MaxAmount: p.MaxAmount.StringFixed(2), DailyOutgoingLimit: p.DailyOutgoingLimit.StringFixed(2)}, err
}
func PolicyReply(ctx context.Context, secret string, p *transfer.Policy, err error) (*types.TransferPolicyResponse, error) {
	if err != nil {
		return nil, err
	}
	plan, err := LimitPlanData(secret, p.Plan)
	if err != nil {
		return nil, err
	}
	return &types.TransferPolicyResponse{Code: SuccessCode, Message: "OK", TraceId: TraceID(ctx), Data: types.TransferPolicyData{Plan: plan, UsedToday: p.Used.StringFixed(2), RemainingToday: p.Remaining.StringFixed(2), ResetAt: TimeString(p.ResetAt), RestrictedUntil: TimeString(p.RestrictedUntil)}}, nil
}
func TransferReply(ctx context.Context, secret string, actorID int64, row *model.TransferOrder, err error) (*types.TransferResponse, error) {
	if err != nil {
		return nil, err
	}
	idx, err := EncodeIdx(secret, idcodec.Transfer, row.Id)
	if err != nil {
		return nil, err
	}
	other, name, direction := row.RecipientId, row.RecipientNickname, "out"
	if actorID == row.RecipientId {
		other, name, direction = row.SenderId, row.SenderNickname, "in"
	}
	otherIdx, err := EncodeIdx(secret, idcodec.User, other)
	if err != nil {
		return nil, err
	}
	return &types.TransferResponse{Code: SuccessCode, Message: "OK", TraceId: TraceID(ctx), Data: types.TransferData{Idx: idx, Direction: direction, Counterparty: types.CurrentUserData{Idx: otherIdx, Nickname: name}, Amount: row.Amount.StringFixed(2), Fee: "0.00", Unit: "USD", Status: "completed", CreatedAt: TimeString(row.CreatedAt)}}, nil
}
