//go:build integration

package server

import (
	"context"
	"encoding/json"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/types"
	"github.com/0xabcd-k/DexWallet/backend/idcodec"
	"github.com/0xabcd-k/DexWallet/backend/logic/account"
	rt "github.com/0xabcd-k/DexWallet/backend/logic/rewardtransfer"
	"github.com/0xabcd-k/DexWallet/backend/logic/risk"
	accounts "github.com/0xabcd-k/DexWallet/backend/model/account"
	shared "github.com/0xabcd-k/DexWallet/backend/svc"
	"github.com/shopspring/decimal"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
	"net/http"
	"testing"
)

func checkRewardTransferHTTP(t *testing.T, s *shared.ServiceContext, adminToken, productToken, merchantToken, productIdx string, call func(string, string, string, any, int, string) (http.Header, json.RawMessage)) {
	t.Helper()
	ctx := context.Background()
	codec, _ := idcodec.New(s.Config.IDSecret, idcodec.User)
	id, err := codec.Decode(productIdx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.Exec("INSERT INTO user_reward_state(user_id,pro_progress,pro_unlocked_at) VALUES(?,2000,UTC_TIMESTAMP(6))", id); err != nil {
		t.Fatal(err)
	}
	err = s.Tx.TransactCtx(ctx, func(ctx context.Context, tx sqlx.Session) error {
		_, err := s.Accounts.Post(ctx, tx, account.Posting{Source: account.Source{Type: account.BusinessReward, ID: 999710 + id, Action: account.ActionPost}, Entries: []account.Entry{account.Credit(id, accounts.KindRewardUSD, decimal.NewFromInt(10)), {System: account.SystemRewardExpense, Side: account.SideDebit, Amount: decimal.NewFromInt(10)}}})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg := risk.Config{Mode: risk.ModeMock, DefaultDecision: risk.Review}
	s.RewardTransfers = rt.New(sqlx.NewSqlConnFromDB(s.DB), s.Tx, s.Accounts, cfg.Assessor())
	call("GET", "/v1/user/reward-transfer-state", "", nil, 401, "UNAUTHENTICATED")
	call("GET", "/v1/admin/reward-transfers", productToken, nil, 401, "UNAUTHENTICATED")
	call("GET", "/v1/admin/reward-transfers", merchantToken, nil, 401, "UNAUTHENTICATED")
	_, raw := call("GET", "/v1/user/reward-transfer-state", productToken, nil, 200, "OK")
	var state types.RewardTransferStateData
	if err = json.Unmarshal(raw, &state); err != nil || !state.Pro || state.RewardUsd != "10.00" {
		t.Fatal(state, err)
	}
	{
		key := "http_reward_approve1"
		decision := rt.Approve
		req := types.RewardTransferRequest{RequestKey: key, Amount: "2.00"}
		_, raw = call("POST", "/v1/user/reward-transfers", productToken, req, 200, "OK")
		var result types.RewardTransferData
		if err = json.Unmarshal(raw, &result); err != nil || result.Status != rt.PendingReview || result.UserIdx != productIdx {
			t.Fatal(result, err)
		}
		call("POST", "/v1/user/reward-transfers", productToken, req, 200, "OK")
		call("GET", "/v1/user/reward-transfers/by-request/"+key, productToken, nil, 200, "OK")
		call("GET", "/v1/user/reward-transfers/"+result.Idx, productToken, nil, 200, "OK")
		call("GET", "/v1/admin/reward-transfers/"+result.Idx, adminToken, nil, 200, "OK")
		path := "/v1/admin/reward-transfers/" + result.Idx + "/review"
		call("POST", path, productToken, map[string]string{"decision": decision, "reason": "checked"}, 401, "UNAUTHENTICATED")
		call("POST", path, adminToken, map[string]string{"decision": decision, "reason": ""}, 400, "INVALID_ARGUMENT")
		// Audit failure must roll back resolution and wallet credit/refund.
		if _, err = s.DB.Exec("ALTER TABLE backoffice_audit ADD CONSTRAINT test_review_audit_fault CHECK (action <> 'review_reward_transfer')"); err != nil {
			t.Fatal(err)
		}
		call("POST", path, adminToken, map[string]string{"decision": decision, "reason": "checked"}, 500, "INTERNAL_ERROR")
		if _, err = s.DB.Exec("ALTER TABLE backoffice_audit DROP CHECK test_review_audit_fault"); err != nil {
			t.Fatal(err)
		}
		_, raw = call("POST", path, adminToken, map[string]string{"decision": decision, "reason": "checked"}, 200, "OK")
		var reviewed types.RewardTransferAdminData
		if err = json.Unmarshal(raw, &reviewed); err != nil || reviewed.Transfer.Status == rt.PendingReview || reviewed.ReviewerIdx == "" {
			t.Fatal(reviewed, err)
		}
		call("POST", path, adminToken, map[string]string{"decision": decision, "reason": "checked"}, 200, "OK")
		opposite := rt.Approve
		if decision == rt.Approve {
			opposite = rt.Reject
		}
		call("POST", path, adminToken, map[string]string{"decision": opposite, "reason": "checked"}, 409, "CONFLICT")
	}
	// Rejection through HTTP, with original outcome safely recoverable.
	_, raw = call("POST", "/v1/user/reward-transfers", productToken, types.RewardTransferRequest{RequestKey: "http_reward_reject02", Amount: "2.00"}, 200, "OK")
	var rejected types.RewardTransferData
	if err = json.Unmarshal(raw, &rejected); err != nil {
		t.Fatal(err)
	}
	call("POST", "/v1/admin/reward-transfers/"+rejected.Idx+"/review", adminToken, map[string]string{"decision": "reject", "reason": "risk review rejected"}, 200, "OK")
	call("GET", "/v1/admin/reward-transfers?status=all&limit=1", adminToken, nil, 200, "OK")
	call("GET", "/v1/user/reward-transfers?limit=1", productToken, nil, 200, "OK")
	var count int
	if err = s.DB.QueryRow("SELECT COUNT(*) FROM backoffice_audit WHERE action='review_reward_transfer'").Scan(&count); err != nil || count != 2 {
		t.Fatal(count, err)
	}
	var value string
	if err = s.DB.QueryRow("SELECT balance FROM user_account WHERE user_id=? AND kind='reward_usd'", id).Scan(&value); err != nil || !decimal.RequireFromString(value).Equal(decimal.NewFromInt(8)) {
		t.Fatal(value, err)
	}
}
