//go:build integration

package server

import (
	"context"
	"encoding/json"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/types"
	"github.com/0xabcd-k/DexWallet/backend/idcodec"
	account "github.com/0xabcd-k/DexWallet/backend/logic/account"
	transfer "github.com/0xabcd-k/DexWallet/backend/logic/transfer"
	model "github.com/0xabcd-k/DexWallet/backend/model/account"
	shared "github.com/0xabcd-k/DexWallet/backend/svc"
	"github.com/shopspring/decimal"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
	"net/http"
	"strings"
	"testing"
)

func checkPublicTransferHTTP(t *testing.T, s *shared.ServiceContext, call func(string, string, string, any, int, string) (http.Header, json.RawMessage)) {
	t.Helper()
	ctx := context.Background()
	tokens := make([]string, 3)
	ids := make([]int64, 3)
	idxs := make([]string, 3)
	for i, email := range []string{"payer@example.com", "receiver@example.com", "observer@example.com"} {
		u, err := s.Users.ResolveOrCreateVerifiedEmail(ctx, normalizedEmail(t, email))
		if err != nil {
			t.Fatal(err)
		}
		ids[i] = u.User.Id
		idxs[i], _ = s.UserIdx.Encode(ids[i])
		tokens[i], _, err = s.Tokens.Issue(ids[i], u.User.AuthVersion)
		if err != nil {
			t.Fatal(err)
		}
	}
	s.ReceiveCodes, _ = transfer.NewCodes(transfer.Config{DefaultLimitPlanID: 1, ReceiveSecret: strings.Repeat("r", 32), WebURL: "https://wallet.example/", TelegramURL: "https://t.me/example_bot/pay"}, s.Config.Environment)
	err := s.Tx.TransactCtx(ctx, func(ctx context.Context, tx sqlx.Session) error {
		_, err := s.Accounts.Post(ctx, tx, account.Posting{Source: account.Source{Type: account.BusinessDeposit, ID: 444, Action: account.ActionPost}, Entries: []account.Entry{account.Credit(ids[0], model.KindUSD, decimal.NewFromInt(20)), {System: account.SystemUSDClearing, Side: account.SideDebit, Amount: decimal.NewFromInt(20)}}})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	// Earlier ledger-only fixtures deliberately used a synthetic transfer source.
	if _, err = s.DB.Exec("ALTER TABLE transfer_order AUTO_INCREMENT=1000"); err != nil {
		t.Fatal(err)
	}
	call("POST", "/v1/user/transfers", "", nil, 401, "UNAUTHENTICATED")
	_, raw := call("GET", "/v1/user/receive-code", tokens[1], nil, 200, "OK")
	var code types.ReceiveCodeData
	if err = json.Unmarshal(raw, &code); err != nil || code.User.Idx != idxs[1] {
		t.Fatal(code, err)
	}
	for _, value := range []string{code.Code, code.Url, code.TelegramUrl} {
		_, raw = call("POST", "/v1/user/transfer-targets/resolve", tokens[0], map[string]string{"receiveCode": value}, 200, "OK")
		var target types.CurrentUserData
		if err = json.Unmarshal(raw, &target); err != nil || target.Idx != idxs[1] {
			t.Fatal(target, err)
		}
	}
	call("POST", "/v1/user/transfer-targets/resolve", tokens[0], map[string]string{"receiveCode": code.Url + "&amount=1"}, 400, "INVALID_ARGUMENT")
	call("GET", "/v1/user/transfer-policy", tokens[0], nil, 200, "OK")
	input := map[string]any{"recipientIdx": idxs[1], "amount": "10.00", "requestKey": "public_http_request_00", "senderIdx": idxs[2], "confirmed": true}
	_, raw = call("POST", "/v1/user/transfers", tokens[0], input, 200, "OK")
	var first types.TransferData
	if err = json.Unmarshal(raw, &first); err != nil || first.Amount != "10.00" || first.Fee != "0.00" || first.Counterparty.Idx != idxs[1] {
		t.Fatal(first, err)
	}
	_, raw = call("POST", "/v1/user/transfers", tokens[0], input, 200, "OK")
	var same types.TransferData
	if err = json.Unmarshal(raw, &same); err != nil || same.Idx != first.Idx {
		t.Fatal(same, err)
	}
	input["amount"] = "11.00"
	call("POST", "/v1/user/transfers", tokens[0], input, 409, "TRANSFER_REQUEST_CONFLICT")
	call("GET", "/v1/user/transfers/by-request/public_http_request_00", tokens[0], nil, 200, "OK")
	call("GET", "/v1/user/transfers/by-request/public_http_request_00", tokens[1], nil, 404, "NOT_FOUND")
	call("GET", "/v1/user/transfers/"+first.Idx, tokens[1], nil, 200, "OK")
	call("GET", "/v1/user/transfers/"+first.Idx, tokens[2], nil, 404, "NOT_FOUND")
	input["requestKey"] = "public_http_request_01"
	input["amount"] = "100.01"
	call("POST", "/v1/user/transfers", tokens[0], input, 422, "TRANSFER_AMOUNT_LIMIT")
	input["amount"] = "11"
	call("POST", "/v1/user/transfers", tokens[0], input, 422, "INSUFFICIENT_BALANCE")
	if _, err = s.DB.Exec("UPDATE user SET payment_restricted_until=DATE_ADD(UTC_TIMESTAMP(),INTERVAL 1 DAY) WHERE id=?", ids[0]); err != nil {
		t.Fatal(err)
	}
	input["amount"] = "1"
	call("POST", "/v1/user/transfers", tokens[0], input, 423, "PAYMENT_RESTRICTED")
	input["requestKey"] = "public_http_request_00"
	input["amount"] = "10.00"
	call("POST", "/v1/user/transfers", tokens[0], input, 200, "OK")
	if _, err = s.DB.Exec("UPDATE user SET auth_version=auth_version+1 WHERE id=?", ids[0]); err != nil {
		t.Fatal(err)
	}
	call("POST", "/v1/user/transfers", tokens[0], input, 401, "UNAUTHENTICATED")
	for i, want := range []string{"10.00", "10.00", "0.00"} {
		balances, err := s.Accounts.List(ctx, ids[i])
		if err != nil || balances[0].Balance != want {
			t.Fatal("sender spoof or partial transfer", balances, err)
		}
	}
}
func checkTransferPlanHTTP(t *testing.T, s *shared.ServiceContext, admin, product, merchant, idx string, call func(string, string, string, any, int, string) (http.Header, json.RawMessage)) {
	t.Helper()
	call("GET", "/v1/admin/transfer-limit-plans", product, nil, 401, "UNAUTHENTICATED")
	call("GET", "/v1/admin/transfer-limit-plans", merchant, nil, 401, "UNAUTHENTICATED")
	input := types.CreateLimitPlanRequest{Name: "Reviewed plan", RequestKey: "admin_plan_request_00", MinAmount: "0.01", MaxAmount: "200.00", DailyOutgoingLimit: "800.00"}
	_, raw := call("POST", "/v1/admin/transfer-limit-plans", admin, input, 200, "OK")
	var plan types.TransferLimitPlan
	if err := json.Unmarshal(raw, &plan); err != nil {
		t.Fatal(err)
	}
	_, raw = call("POST", "/v1/admin/transfer-limit-plans", admin, input, 200, "OK")
	var same types.TransferLimitPlan
	if err := json.Unmarshal(raw, &same); err != nil || same.Idx != plan.Idx {
		t.Fatal(same, err)
	}
	input.MaxAmount = "201"
	call("POST", "/v1/admin/transfer-limit-plans", admin, input, 409, "CONFLICT")
	call("GET", "/v1/admin/transfer-limit-plans", admin, nil, 200, "OK")
	_, raw = call("GET", "/v1/admin/users/"+idx+"/transfer-limit-plan", admin, nil, 200, "OK")
	var policy types.TransferPolicyData
	if err := json.Unmarshal(raw, &policy); err != nil {
		t.Fatal(err)
	}
	assignment := map[string]string{"planIdx": plan.Idx, "expectedPlanIdx": policy.Plan.Idx, "reason": "reviewed test allocation"}
	path := "/v1/admin/users/" + idx + "/transfer-limit-plan"
	call("PUT", path, admin, assignment, 200, "OK")
	call("PUT", path, admin, assignment, 200, "OK")
	assignment["planIdx"] = policy.Plan.Idx
	call("PUT", path, admin, assignment, 409, "CONFLICT")
	assignment["expectedPlanIdx"] = plan.Idx
	call("PUT", path, admin, assignment, 200, "OK")
	var count int
	if err := s.DB.QueryRow("SELECT COUNT(*) FROM backoffice_audit WHERE action='assign_limit_plan'").Scan(&count); err != nil || count != 2 {
		t.Fatal("duplicate or missing audit", count, err)
	}
	call("GET", "/v1/admin/audits", admin, nil, 200, "OK")
	codec, _ := idcodec.New(s.Config.IDSecret, idcodec.TransferLimitPlan)
	if _, err := codec.Decode(plan.Idx); err != nil {
		t.Fatal(err)
	}
}
