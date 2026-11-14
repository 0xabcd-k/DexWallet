package task

import (
	"github.com/shopspring/decimal"
	"testing"
)

func TestRewardRules(t *testing.T) {
	tests := []struct {
		name  string
		input RewardInput
		want  []Award
	}{
		{"first self", RewardInput{ParticipantID: 1, Stage: StageFirst, Mode: ClaimSelf, BasePoints: 200}, []Award{{UserID: 1, Reason: RewardFirst, USD: decimal.NewFromInt(1), Progress: decimal.NewFromInt(1000)}}},
		{"first help pro inviter", RewardInput{ParticipantID: 1, InviterID: 2, ParentID: 3, Stage: StageFirst, Mode: ClaimHelp, BasePoints: 200, RecipientPro: true}, []Award{{UserID: 1, Reason: RewardFirst, USD: decimal.NewFromInt(1), Progress: decimal.NewFromInt(1000)}, {UserID: 2, Reason: RewardAssistance, Points: decimal.NewFromInt(400), Progress: decimal.NewFromInt(400)}, {UserID: 3, Reason: RewardCommission, Points: decimal.NewFromInt(80)}}},
		{"old helper is parent", RewardInput{ParticipantID: 1, InviterID: 2, ParentID: 1, Stage: StageReturning, Mode: ClaimHelp, BasePoints: 200}, []Award{{UserID: 2, Reason: RewardAssistance, Points: decimal.NewFromInt(200), Progress: decimal.NewFromInt(200)}, {UserID: 1, Reason: RewardCommission, Points: decimal.NewFromInt(40)}}},
		{"floor commission", RewardInput{ParticipantID: 1, ParentID: 2, Stage: StageReturning, Mode: ClaimSelf, BasePoints: 7}, []Award{{UserID: 1, Reason: RewardOwn, Points: decimal.NewFromInt(7), Progress: decimal.NewFromInt(7)}, {UserID: 2, Reason: RewardCommission, Points: decimal.NewFromInt(1)}}},
		{"omit zero", RewardInput{ParticipantID: 1, ParentID: 2, Stage: StageReturning, Mode: ClaimSelf, BasePoints: 4}, []Award{{UserID: 1, Reason: RewardOwn, Points: decimal.NewFromInt(4), Progress: decimal.NewFromInt(4)}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := CalculateRewards(tt.input)
			if err != nil || len(got) != len(tt.want) {
				t.Fatal(got, err)
			}
			for i, w := range tt.want {
				g := got[i]
				if g.UserID != w.UserID || g.Reason != w.Reason || !g.USD.Equal(w.USD) || !g.Points.Equal(w.Points) || !g.Progress.Equal(w.Progress) {
					t.Fatalf("%+v != %+v", g, w)
				}
			}
		})
	}
	_, err := CalculateRewards(RewardInput{ParticipantID: 1, Stage: StageReturning, Mode: ClaimSelf, BasePoints: 999999999999999999, RecipientPro: true})
	if err == nil {
		t.Fatal("overflow accepted")
	}
	_, err = CalculateRewards(RewardInput{ParticipantID: 1, InviterID: 1, Stage: StageFirst, Mode: ClaimHelp, BasePoints: 100})
	if err == nil {
		t.Fatal("self invitation accepted")
	}
}
