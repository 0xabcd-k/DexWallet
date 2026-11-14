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
	call("POST", "/v1/user/tasks/"+idx+"/claim", token, nil, 404, "")
	var count int
	if err = s.DB.QueryRow("SELECT COUNT(*) FROM task_completion").Scan(&count); err != nil || count != 0 {
		t.Fatal("check completed task", count, err)
	}
	s.Tasks = tasks.New(sqlx.NewSqlConnFromDB(s.DB), nil)
}
