package httpapi

import (
	"github.com/0xabcd-k/DexWallet/backend/api/internal/types"
	"github.com/0xabcd-k/DexWallet/backend/idcodec"
	"github.com/0xabcd-k/DexWallet/backend/logic/draw"
	"strconv"
)

func DrawStateData(secret string, s *draw.State) (types.DrawStateData, error) {
	d := types.DrawStateData{Points: s.Points, RewardUsd: s.RewardUSD, CostPoints: strconv.FormatInt(draw.CostPoints, 10), Prizes: []types.DrawPrizeData{}}
	if s.Version == nil {
		return d, nil
	}
	v := s.Version
	d.Available = true
	d.VersionName = v.Row.Name
	d.Rtp = v.Rules.RTP.String()
	var err error
	d.VersionIdx, err = EncodeIdx(secret, idcodec.DrawVersion, v.Row.Id)
	for _, p := range v.Rules.Prizes {
		d.Prizes = append(d.Prizes, types.DrawPrizeData{Code: p.Code, RewardUsd: p.RewardUSD.StringFixed(2), Weight: p.Weight, Symbols: p.Symbols[:]})
	}
	return d, err
}
func DrawResultData(secret string, r *draw.Result) (types.DrawResultData, error) {
	row := r.Row
	d := types.DrawResultData{VersionName: r.VersionName, CostPoints: strconv.FormatInt(row.CostPoints, 10), PrizeCode: row.PrizeCode, RewardUsd: row.RewardUsd.StringFixed(2), Symbols: r.Symbols[:], CreatedAt: TimeString(row.CreatedAt)}
	var err error
	d.Idx, err = EncodeIdx(secret, idcodec.PointDraw, row.Id)
	if err != nil {
		return d, err
	}
	d.VersionIdx, err = EncodeIdx(secret, idcodec.DrawVersion, row.VersionId)
	if err != nil {
		return d, err
	}
	d.VoucherIdx, err = EncodeIdx(secret, idcodec.LedgerVoucher, row.VoucherId.Int64)
	return d, err
}
