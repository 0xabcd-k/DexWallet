package httpapi

import (
	"github.com/0xabcd-k/DexWallet/backend/api/internal/types"
	"github.com/0xabcd-k/DexWallet/backend/idcodec"
	"github.com/0xabcd-k/DexWallet/backend/logic/task"
	"github.com/0xabcd-k/DexWallet/backend/model/user"
)

func TaskData(secret string, item task.Item) (types.TaskData, error) {
	d := item.Definition
	idx, err := EncodeIdx(secret, idcodec.Task, d.Id)
	return types.TaskData{Idx: idx, Kind: d.Kind, Title: d.Title, Description: d.Description, RequiredIdentity: string(user.IdentityTelegram),
		StartsAt: TimeString(d.StartsAt), EndsAt: TimeString(d.EndsAt), Eligibility: item.Eligibility, Sample: true, ClaimAvailable: false}, err
}
