package httpapi

import (
	"github.com/0xabcd-k/DexWallet/backend/api/internal/types"
	"github.com/0xabcd-k/DexWallet/backend/idcodec"
	"github.com/0xabcd-k/DexWallet/backend/logic/draw"
	model "github.com/0xabcd-k/DexWallet/backend/model/task"
	"strconv"
)

func OptionalIdx(secret string, kind idcodec.Kind, id int64) (string, error) {
	if id == 0 {
		return "", nil
	}
	return EncodeIdx(secret, kind, id)
}
func DecodeOptionalIdx(secret string, kind idcodec.Kind, idx string) (int64, error) {
	if idx == "" {
		return 0, nil
	}
	return DecodeIdx(secret, kind, idx)
}
func ManagedTask(secret string, r *model.TaskDefinition) (types.ManagedTaskData, error) {
	idx, err := EncodeIdx(secret, idcodec.Task, r.Id)
	return types.ManagedTaskData{Idx: idx, Title: r.Title, Description: r.Description, BasePoints: strconv.FormatInt(r.BasePoints, 10), ChannelId: strconv.FormatInt(r.ChannelId, 10), StartsAt: TimeString(r.StartsAt), EndsAt: TimeString(r.EndsAt), Status: r.Status, Revision: r.Revision}, err
}
func ManagedVersion(secret string, v *draw.Version) (types.ManagedDrawVersion, error) {
	d, err := DrawStateData(secret, &draw.State{Version: v})
	return types.ManagedDrawVersion{Idx: d.VersionIdx, Name: d.VersionName, Rtp: d.Rtp, Prizes: d.Prizes}, err
}
func ManagedDraw(secret string, m *draw.Management, writable bool) (types.ManagedDrawData, error) {
	d := types.ManagedDrawData{Items: []types.ManagedDrawVersion{}, Revision: m.Control.Revision, Writable: writable}
	var err error
	d.CurrentVersionIdx, err = OptionalIdx(secret, idcodec.DrawVersion, m.Control.VersionId.Int64)
	if err != nil {
		return d, err
	}
	d.NextCursor, err = OptionalIdx(secret, idcodec.DrawVersion, m.Next)
	if err != nil {
		return d, err
	}
	for _, v := range m.Items {
		row, err := ManagedVersion(secret, v)
		if err != nil {
			return d, err
		}
		d.Items = append(d.Items, row)
	}
	return d, nil
}
