package task

import (
	"fmt"
	accounts "github.com/0xabcd-k/DexWallet/backend/model/account"
	"github.com/shopspring/decimal"
)

const (
	ClaimSelf               = "self"
	ClaimHelp               = "help"
	StageFirst              = "first"
	StageReturning          = "returning"
	RewardFirst             = "first_task"
	RewardOwn               = "own_task"
	RewardAssistance        = "assistance"
	RewardCommission        = "commission"
	RewardRuleVersion int64 = 1
	FirstProgress     int64 = 1000
	ProThreshold      int64 = 2000
	ProMultiplier     int64 = 2
	CommissionPercent int64 = 20
)

type RewardInput struct {
	ParticipantID, InviterID, ParentID, BasePoints int64
	Stage, Mode                                    string
	RecipientPro                                   bool
}
type Award struct {
	UserID                int64
	Reason                string
	USD, Points, Progress decimal.Decimal
}

// CalculateRewards has no IO. Rights and relationships are supplied by the
// locked settlement snapshot; commissions never recurse or grow Pro progress.
func CalculateRewards(in RewardInput) ([]Award, error) {
	if in.ParticipantID <= 0 || in.BasePoints <= 0 || (in.Stage != StageFirst && in.Stage != StageReturning) ||
		(in.Mode != ClaimSelf && in.Mode != ClaimHelp) || (in.Mode == ClaimHelp && (in.InviterID <= 0 || in.InviterID == in.ParticipantID)) ||
		(in.Mode == ClaimSelf && in.InviterID != 0) || in.ParentID < 0 {
		return nil, fmt.Errorf("invalid reward input")
	}
	awards := []Award{}
	if in.Stage == StageFirst {
		awards = append(awards, Award{UserID: in.ParticipantID, Reason: RewardFirst, USD: decimal.NewFromInt(1), Progress: decimal.NewFromInt(FirstProgress)})
	}
	if in.Stage == StageFirst && in.Mode == ClaimSelf {
		return awards, nil
	}
	id, reason := in.ParticipantID, RewardOwn
	if in.Mode == ClaimHelp {
		id, reason = in.InviterID, RewardAssistance
	}
	points := decimal.NewFromInt(in.BasePoints)
	if in.RecipientPro {
		points = points.Mul(decimal.NewFromInt(ProMultiplier))
	}
	definition, _ := accounts.Lookup(accounts.KindTaskPoints)
	if err := definition.ValidateBalances(points, decimal.Zero); err != nil {
		return nil, err
	}
	awards = append(awards, Award{UserID: id, Reason: reason, Points: points, Progress: points})
	commission := points.Mul(decimal.NewFromInt(CommissionPercent)).Div(decimal.NewFromInt(100)).Floor()
	if in.ParentID != 0 && commission.IsPositive() {
		awards = append(awards, Award{UserID: in.ParentID, Reason: RewardCommission, Points: commission})
	}
	return awards, nil
}
