//go:build integration

package server

import (
	"context"
	"encoding/json"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/types"
	"github.com/0xabcd-k/DexWallet/backend/logic/draw"
	"github.com/0xabcd-k/DexWallet/backend/logic/task"
	shared "github.com/0xabcd-k/DexWallet/backend/svc"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
	"net/http"
	"testing"
	"time"
)

func checkRewardOperationsHTTP(t *testing.T, s *shared.ServiceContext, admin, user, merchant string, call func(string, string, string, any, int, string) (http.Header, json.RawMessage)) {
	t.Helper()
	conn := sqlx.NewSqlConnFromDB(s.DB)
	member, _ := task.NewMock(nil)
	s.Tasks = task.NewWithRewards(conn, member, s.Tx, s.Accounts)
	s.Draws = draw.New(conn, s.Tx, s.Accounts, true)
	for _, path := range []string{"/v1/admin/tasks", "/v1/admin/draw-versions"} {
		call("GET", path, user, nil, 401, "UNAUTHENTICATED")
		call("GET", path, merchant, nil, 401, "UNAUTHENTICATED")
	}
	req := types.SaveTaskDraftRequest{ExpectedRevision: 0, RequestKey: "http_task_operations01", Title: "运营样例", Description: "本地 mock", BasePoints: "9007199254740993", ChannelId: "-100", StartsAt: time.Now().Add(-time.Hour).UTC().Format(time.RFC3339), EndsAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339), Reason: "isolated test"}
	_, raw := call("POST", "/v1/admin/task-drafts", admin, req, 200, "OK")
	var row types.ManagedTaskData
	if err := json.Unmarshal(raw, &row); err != nil || (row.Status != task.StatusDraft || row.BasePoints != req.BasePoints) {
		t.Fatal(row, err)
	}
	_, replay := call("POST", "/v1/admin/task-drafts", admin, req, 200, "OK")
	if string(replay) != string(raw) {
		t.Fatal("creation replay differs")
	}
	original := req
	req.Title = "changed"
	call("POST", "/v1/admin/task-drafts", admin, req, 409, "CONFLICT")
	req.BasePoints = "100"
	req.Idx = row.Idx
	req.ExpectedRevision = row.Revision
	_, raw = call("POST", "/v1/admin/task-drafts", admin, req, 200, "OK")
	if err := json.Unmarshal(raw, &row); err != nil || row.Revision != 2 {
		t.Fatal(row, err)
	}
	call("POST", "/v1/admin/task-drafts", admin, original, 200, "OK") // original creation remains recoverable after edit
	call("POST", "/v1/admin/task-drafts", admin, req, 200, "OK")
	req.Title = "stale edit"
	call("POST", "/v1/admin/task-drafts", admin, req, 409, "CONFLICT")
	statusPath := "/v1/admin/tasks/" + row.Idx + "/status"
	status := map[string]any{"expectedRevision": 2, "status": "active", "reason": "publish mock"}
	// A failed audit rolls back the task status as well.
	if _, err := s.DB.Exec("ALTER TABLE backoffice_audit ADD CONSTRAINT test_ops_audit CHECK(action <> 'set_task_status')"); err != nil {
		t.Fatal(err)
	}
	call("POST", statusPath, admin, status, 500, "INTERNAL_ERROR")
	if _, err := s.DB.Exec("ALTER TABLE backoffice_audit DROP CHECK test_ops_audit"); err != nil {
		t.Fatal(err)
	}
	_, raw = call("POST", statusPath, admin, status, 200, "OK")
	if err := json.Unmarshal(raw, &row); err != nil || row.Revision != 3 {
		t.Fatal(row, err)
	}
	call("POST", statusPath, admin, status, 200, "OK")
	req.ExpectedRevision = 3
	call("POST", "/v1/admin/task-drafts", admin, req, 409, "CONFLICT")
	call("POST", statusPath, admin, map[string]any{"expectedRevision": 2, "status": "paused", "reason": "stale"}, 409, "CONFLICT")
	call("POST", statusPath, admin, map[string]any{"expectedRevision": 3, "status": "paused", "reason": "pause"}, 200, "OK")
	call("POST", statusPath, admin, status, 409, "CONFLICT")
	call("GET", "/v1/admin/tasks?limit=1", admin, nil, 200, "OK")
	call("GET", "/v1/admin/tasks/"+row.Idx+"/completions", admin, nil, 200, "OK")
	call("GET", "/v1/user/referral-friends", user, nil, 200, "OK")
	call("GET", "/v1/user/invitation-earnings?from=bad", user, nil, 400, "INVALID_ARGUMENT")
	call("GET", "/v1/user/invitation-earnings?reason=commission", user, nil, 200, "OK")
	rules := draw.SampleRules()
	versionReq := types.SaveDrawVersionRequest{Name: "http_ops_v1", Rtp: rules.RTP.String(), Reason: "candidate"}
	for _, p := range rules.Prizes {
		versionReq.Prizes = append(versionReq.Prizes, types.DrawPrizeData{Code: p.Code, RewardUsd: p.RewardUSD.StringFixed(2), Weight: p.Weight, Symbols: p.Symbols[:]})
	}
	_, raw = call("POST", "/v1/admin/draw-versions", admin, versionReq, 200, "OK")
	var v types.ManagedDrawVersion
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	call("POST", "/v1/admin/draw-versions", admin, versionReq, 200, "OK")
	versionReq.Rtp = "0.94"
	call("POST", "/v1/admin/draw-versions", admin, versionReq, 400, "INVALID_ARGUMENT")
	_, raw = call("GET", "/v1/admin/draw-versions", admin, nil, 200, "OK")
	var state types.ManagedDrawData
	if err := json.Unmarshal(raw, &state); err != nil || state.CurrentVersionIdx != "" {
		t.Fatal(state, err)
	}
	activation := types.SetDrawControlRequest{VersionIdx: v.Idx, ExpectedRevision: state.Revision, Reason: "activate"}
	call("POST", "/v1/admin/draw-control", admin, activation, 200, "OK")
	call("POST", "/v1/admin/draw-control", admin, activation, 200, "OK")
	pause := types.SetDrawControlRequest{ExpectedRevision: state.Revision + 1, Reason: "pause"}
	call("POST", "/v1/admin/draw-control", admin, pause, 200, "OK")
	call("POST", "/v1/admin/draw-control", admin, activation, 409, "CONFLICT")
	s.Draws = draw.New(conn, s.Tx, s.Accounts, false)
	versionReq.Rtp = "0.95"
	versionReq.Name = "disabled"
	call("POST", "/v1/admin/draw-versions", admin, versionReq, 409, "DRAW_UNAVAILABLE")
	call("GET", "/v1/admin/audits", admin, nil, 200, "OK")
	var count int
	if err := s.DB.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM backoffice_audit WHERE action IN ('save_task_draft','set_task_status','save_draw_version','set_draw_control')").Scan(&count); err != nil || count != 7 {
		t.Fatal(count, err)
	}
}
