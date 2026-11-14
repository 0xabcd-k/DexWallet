//go:build integration

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/0xabcd-k/DexWallet/backend/api/internal/types"
	"github.com/0xabcd-k/DexWallet/backend/idcodec"
	tasks "github.com/0xabcd-k/DexWallet/backend/logic/task"
	users "github.com/0xabcd-k/DexWallet/backend/logic/user"
	shared "github.com/0xabcd-k/DexWallet/backend/svc"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

func checkTaskHTTP(t *testing.T, s *shared.ServiceContext, call func(string, string, string, any, int, string) (http.Header, json.RawMessage)) {
	t.Helper()
	ctx := context.Background()
	u, err := s.Users.ResolveOrCreateVerifiedTelegram(ctx, users.VerifiedTelegram{TelegramUserID: 88001, FirstName: "Task sample"})
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := s.Tokens.Issue(u.User.Id, u.User.AuthVersion)
	if err != nil {
		t.Fatal(err)
	}
	email, err := s.Users.ResolveOrCreateVerifiedEmail(ctx, normalizedEmail(t, "task_http@example.com"))
	if err != nil {
		t.Fatal(err)
	}
	unbound, _, err := s.Tokens.Issue(email.User.Id, email.User.AuthVersion)
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.DB.Exec("INSERT INTO task_definition(kind,status,verification_mode,title,description,base_points,channel_id,starts_at,ends_at) VALUES('telegram_channel_follow','active','mock','Sample channel','No real subscription',100,-100,DATE_SUB(UTC_TIMESTAMP(),INTERVAL 1 HOUR),DATE_ADD(UTC_TIMESTAMP(),INTERVAL 1 HOUR))")
	if err != nil {
		t.Fatal(err)
	}
	id, _ := result.LastInsertId()
	codec, _ := idcodec.New(s.Config.IDSecret, idcodec.Task)
	idx, _ := codec.Encode(id)
	// Disabled by default: stored mock tasks cannot leak into ordinary deployments.
	_, raw := call("GET", "/v1/user/tasks", token, nil, 200, "OK")
	var page types.TaskListData
	if err = json.Unmarshal(raw, &page); err != nil || len(page.Items) != 0 {
		t.Fatal(page, err)
	}
	mock, _ := tasks.NewMock([]tasks.Fixture{{ChannelID: -100, TelegramUserID: 88001, Status: tasks.MemberRegular}})
	s.Tasks = tasks.New(sqlx.NewSqlConnFromDB(s.DB), mock)
	call("GET", "/v1/user/tasks", "", nil, 401, "UNAUTHENTICATED")
	_, raw = call("GET", "/v1/user/tasks?limit=1", token, nil, 200, "OK")
	if err = json.Unmarshal(raw, &page); err != nil || len(page.Items) != 1 || page.Items[0].Idx != idx || page.Items[0].ClaimAvailable {
		t.Fatal(page, err)
	}
	call("GET", "/v1/user/tasks?before=bad", token, nil, 400, "INVALID_ARGUMENT")
	call("GET", "/v1/user/tasks?limit=0", token, nil, 400, "INVALID_ARGUMENT")
	call("GET", "/v1/user/tasks/bad", token, nil, 404, "NOT_FOUND")
	call("POST", "/v1/user/tasks/"+idx+"/check", unbound, map[string]any{"telegramUserID": 88001, "outcome": "member"}, 422, "TASK_TELEGRAM_REQUIRED")
	for range 2 {
		_, raw = call("POST", "/v1/user/tasks/"+idx+"/check", token, map[string]any{"telegramUserID": 88002, "channelID": -999, "status": "left"}, 200, "OK")
		var checked types.TaskCheckData
		if err = json.Unmarshal(raw, &checked); err != nil || checked.Outcome != tasks.OutcomeMember || checked.Task.ClaimAvailable {
			t.Fatal(checked, err)
		}
		if strings.Contains(string(raw), "88001") || strings.Contains(string(raw), "88002") {
			t.Fatal("exposed numeric Telegram identity")
		}
	}
	failed, _ := tasks.NewMock([]tasks.Fixture{{ChannelID: -100, TelegramUserID: 88001, Failure: tasks.FailureTimeout}})
	s.Tasks = tasks.New(sqlx.NewSqlConnFromDB(s.DB), failed)
	call("POST", "/v1/user/tasks/"+idx+"/check", token, nil, 503, "TASK_VERIFICATION_UNAVAILABLE")
	call("POST", "/v1/user/tasks/"+idx+"/claim", token, nil, 400, "INVALID_ARGUMENT")
	var count int
	if err = s.DB.QueryRow("SELECT COUNT(*) FROM task_completion").Scan(&count); err != nil || count != 0 {
		t.Fatal("check completed task", count, err)
	}

	// The preceding ledger-only scenario used synthetic reward source ID 1.
	// Reserve a disjoint range for real reward-grant records in this shared fixture.
	if _, err = s.DB.Exec("ALTER TABLE reward_grant AUTO_INCREMENT=1000"); err != nil {
		t.Fatal(err)
	}
	s.Tasks = tasks.NewWithRewards(sqlx.NewSqlConnFromDB(s.DB), mock, s.Tx, s.Accounts)
	call("POST", "/v1/user/tasks/"+idx+"/invitation", token, nil, 422, "TASK_INVITATION_REQUIRED_FIRST")
	_, raw = call("GET", "/v1/user/tasks/"+idx+"/claim-preview", token, nil, 200, "OK")
	var preview types.ClaimPreviewData
	if err = json.Unmarshal(raw, &preview); err != nil || preview.RewardUsd != "1.00" || preview.ProDelta != "1000" || preview.TaskPoints != "0" || !preview.Task.ClaimAvailable {
		t.Fatal(preview, err)
	}
	request := types.TaskClaimRequest{RequestKey: "http_task_claim_0001", Mode: "self", ExpectedTaskRevision: preview.TaskRevision, ExpectedIdentityRevision: preview.IdentityRevision, ExpectedParticipantStage: preview.ParticipantStage}
	_, raw = call("POST", "/v1/user/tasks/"+idx+"/claim", token, request, 200, "OK")
	var completed types.TaskCompletionData
	if err = json.Unmarshal(raw, &completed); err != nil || len(completed.Grants) != 1 || completed.Grants[0].RewardUsd != "1.00" || completed.Grants[0].Points != "0" {
		t.Fatal(completed, err)
	}
	_, raw = call("GET", "/v1/user/task-completions/by-request/"+request.RequestKey, token, nil, 200, "OK")
	var recovered types.TaskCompletionData
	if err = json.Unmarshal(raw, &recovered); err != nil || recovered.Idx != completed.Idx {
		t.Fatal(recovered, err)
	}
	call("GET", "/v1/user/task-completions/"+completed.Idx, unbound, nil, 404, "NOT_FOUND")
	call("GET", "/v1/user/reward-grants/"+completed.Grants[0].Idx, unbound, nil, 404, "NOT_FOUND")
	call("GET", "/v1/user/reward-grants/"+completed.Grants[0].Idx, token, nil, 200, "OK")
	call("GET", "/v1/user/task-completions?taskIdx="+idx, token, nil, 200, "OK")
	call("GET", "/v1/user/reward-summary", token, nil, 200, "OK")
	_, raw = call("POST", "/v1/user/tasks/"+idx+"/invitation", token, nil, 200, "OK")
	var invitation types.TaskInvitationData
	if err = json.Unmarshal(raw, &invitation); err != nil || invitation.TaskIdx != idx || !strings.HasPrefix(invitation.StartParam, "task_") {
		t.Fatal(invitation, err)
	}
	call("GET", "/v1/user/task-invitations/"+invitation.Idx, unbound, nil, 200, "OK")
	call("POST", "/v1/user/tasks/"+idx+"/claim", token, request, 200, "OK")
	request.ExpectedParticipantStage = "returning"
	call("POST", "/v1/user/tasks/"+idx+"/claim", token, request, 409, "TASK_REQUEST_CONFLICT")
	s.Tasks = tasks.New(sqlx.NewSqlConnFromDB(s.DB), nil)
}
