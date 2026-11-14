//go:build integration

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/0xabcd-k/DexWallet/backend/api/internal/types"
	"github.com/0xabcd-k/DexWallet/backend/idcodec"
	"github.com/0xabcd-k/DexWallet/backend/logic/account"
	"github.com/0xabcd-k/DexWallet/backend/logic/draw"
	users "github.com/0xabcd-k/DexWallet/backend/logic/user"
	accounts "github.com/0xabcd-k/DexWallet/backend/model/account"
	shared "github.com/0xabcd-k/DexWallet/backend/svc"
	"github.com/shopspring/decimal"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

func checkDrawHTTP(t *testing.T, s *shared.ServiceContext, call func(string, string, string, any, int, string) (http.Header, json.RawMessage)) {
	t.Helper()
	ctx := context.Background()
	u, err := s.Users.ResolveOrCreateVerifiedEmail(ctx, normalizedEmail(t, "draw_http@example.com"))
	if err != nil {
		t.Fatal(err)
	}
	actor := users.Actor{UserID: u.User.Id, AuthVersion: u.User.AuthVersion}
	token, _, err := s.Tokens.Issue(actor.UserID, actor.AuthVersion)
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.Users.ResolveOrCreateVerifiedEmail(ctx, normalizedEmail(t, "draw_other@example.com"))
	if err != nil {
		t.Fatal(err)
	}
	otherToken, _, err := s.Tokens.Issue(other.User.Id, other.User.AuthVersion)
	if err != nil {
		t.Fatal(err)
	}
	call("GET", "/v1/user/draw-state", "", nil, 401, "UNAUTHENTICATED")
	_, raw := call("GET", "/v1/user/draw-state", token, nil, 200, "OK")
	var state types.DrawStateData
	if err = json.Unmarshal(raw, &state); err != nil || state.Available {
		t.Fatal(state, err)
	}
	s.Draws = draw.New(sqlx.NewSqlConnFromDB(s.DB), s.Tx, s.Accounts, true)
	v, err := s.Draws.Publish(ctx, "http_candidate", draw.SampleRules())
	if err != nil {
		t.Fatal(err)
	}
	codec, _ := idcodec.New(s.Config.IDSecret, idcodec.DrawVersion)
	versionIdx, _ := codec.Encode(v.Id)
	request := types.DrawRequest{RequestKey: "http_draw_request001", VersionIdx: versionIdx}
	call("POST", "/v1/user/draws", token, request, 422, "INSUFFICIENT_BALANCE")
	err = s.Tx.TransactCtx(ctx, func(ctx context.Context, session sqlx.Session) error {
		_, err := s.Accounts.Post(ctx, session, account.Posting{Source: account.Source{Type: account.BusinessReward, ID: 999900 + actor.UserID, Action: account.ActionPost}, Entries: []account.Entry{account.Credit(actor.UserID, accounts.KindTaskPoints, decimal.NewFromInt(100)), {System: account.SystemPointIssuance, Side: account.SideDebit, Amount: decimal.NewFromInt(100)}}})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	_, raw = call("GET", "/v1/user/draw-state", token, nil, 200, "OK")
	if err = json.Unmarshal(raw, &state); err != nil || !state.Available || state.Points != "100" || len(state.Prizes) != 7 {
		t.Fatal(state, err)
	}
	call("POST", "/v1/user/draws", token, types.DrawRequest{RequestKey: "short", VersionIdx: versionIdx}, 400, "INVALID_ARGUMENT")
	_, raw = call("POST", "/v1/user/draws", token, request, 200, "OK")
	var settled types.DrawResultData
	if err = json.Unmarshal(raw, &settled); err != nil || settled.CostPoints != "100" || settled.VersionIdx != versionIdx || len(settled.Symbols) != 3 {
		t.Fatal(settled, err)
	}
	_, raw = call("GET", "/v1/user/draws/by-request/"+request.RequestKey, token, nil, 200, "OK")
	var original types.DrawResultData
	if err = json.Unmarshal(raw, &original); err != nil || original.Idx != settled.Idx {
		t.Fatal(original, err)
	}
	call("POST", "/v1/user/draws", token, request, 200, "OK")
	call("GET", "/v1/user/draws/"+settled.Idx, token, nil, 200, "OK")
	call("GET", "/v1/user/draws/"+settled.Idx, otherToken, nil, 404, "NOT_FOUND")
	call("GET", "/v1/user/draws/by-request/"+request.RequestKey, otherToken, nil, 404, "NOT_FOUND")
	call("GET", "/v1/user/draws?before=bad", token, nil, 400, "INVALID_ARGUMENT")
	_, raw = call("GET", "/v1/user/draws?limit=1", token, nil, 200, "OK")
	var list types.DrawListData
	if err = json.Unmarshal(raw, &list); err != nil || len(list.Items) != 1 {
		t.Fatal(list, err)
	}
	_, raw = call("GET", "/v1/user/account-entries?kind=task_points", token, nil, 200, "OK")
	var ledger types.AccountEntriesData
	if err = json.Unmarshal(raw, &ledger); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, entry := range ledger.Entries {
		if entry.BusinessType == string(account.BusinessDraw) {
			found = entry.BusinessIdx == settled.Idx
		}
	}
	if !found {
		t.Fatal("draw statement source was not encoded as draw idx")
	}
}
