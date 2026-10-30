//go:build integration

package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/api/internal/types"
	"github.com/0xabcd-k/DexWallet/backend/idcodec"
	account "github.com/0xabcd-k/DexWallet/backend/logic/account"
	model "github.com/0xabcd-k/DexWallet/backend/model/account"
	shared "github.com/0xabcd-k/DexWallet/backend/svc"
	"github.com/shopspring/decimal"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

func checkHoldsHTTP(t *testing.T, s *shared.ServiceContext, userID, otherID int64, token string, call func(string, string, string, any, int, string) (http.Header, json.RawMessage)) {
	t.Helper()
	ctx := context.Background()
	call("GET", "/v1/user/account-holds", "", nil, 401, "UNAUTHENTICATED")
	read := func(path string) types.AccountHoldsData {
		t.Helper()
		_, body := call("GET", path, token, nil, 200, "OK")
		var data types.AccountHoldsData
		if err := json.Unmarshal(body, &data); err != nil {
			t.Fatal(err)
		}
		return data
	}
	if page := read("/v1/user/account-holds"); len(page.Holds) != 0 || page.NextCursor != "" {
		t.Fatal(page)
	}
	for _, q := range []string{"status=bad", "before=1", "limit=0", "limit=101", "limit=bad"} {
		call("GET", "/v1/user/account-holds?"+q, token, nil, 400, "INVALID_ARGUMENT")
	}
	ids := make([]int64, 3)
	for i := range ids {
		owner := userID
		if i == 2 {
			owner = otherID
		}
		err := s.Tx.TransactCtx(ctx, func(ctx context.Context, tx sqlx.Session) error {
			var err error
			ids[i], err = s.Accounts.Hold(ctx, tx, account.HoldRequest{UserID: owner, BusinessType: account.BusinessWithdrawal, BusinessID: int64(i + 1), Amount: decimal.RequireFromString("1.25")})
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Tx.TransactCtx(ctx, func(ctx context.Context, tx sqlx.Session) error {
		_, err := s.Accounts.Release(ctx, tx, ids[0], model.HoldRejected)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	page := read(fmt.Sprintf("/v1/user/account-holds?userId=%d&limit=1", otherID))
	if len(page.Holds) != 1 || page.NextCursor == "" || page.Holds[0].Status != model.HoldHeld || page.Holds[0].Amount != "1.25" || page.Holds[0].ResolvedAt != "" || page.Holds[0].ResolvedVoucherIdx != "" {
		t.Fatal(page)
	}
	codec, _ := idcodec.New(s.Config.IDSecret, idcodec.AccountHold)
	if id, err := codec.Decode(page.Holds[0].Idx); err != nil || id != ids[1] {
		t.Fatal("owner/idx", id, err)
	}
	next := read("/v1/user/account-holds?before=" + page.NextCursor)
	if len(next.Holds) != 1 || next.NextCursor != "" || next.Holds[0].Status != model.HoldReleased || next.Holds[0].Reason != model.HoldRejected || next.Holds[0].ResolvedVoucherIdx == "" || next.Holds[0].HoldVoucherIdx == next.Holds[0].ResolvedVoucherIdx {
		t.Fatal(next)
	}
	if _, err := time.Parse(time.RFC3339Nano, next.Holds[0].ResolvedAt); err != nil {
		t.Fatal(err)
	}
	foreign, _ := codec.Encode(ids[2])
	if page := read("/v1/user/account-holds?before=" + foreign); len(page.Holds) != 2 {
		t.Fatal("foreign cursor crossed owner", page)
	}
	if page := read("/v1/user/account-holds?status=held"); len(page.Holds) != 1 || page.Holds[0].Idx != nextCursorForHold(t, codec, ids[1]) {
		t.Fatal(page)
	}
	if page := read("/v1/user/account-holds?status=consumed"); len(page.Holds) != 0 {
		t.Fatal(page)
	}
	_, body := call("GET", "/v1/user/account-entries?kind=usd_balance", token, nil, 200, "OK")
	var entries types.AccountEntriesData
	if err := json.Unmarshal(body, &entries); err != nil {
		t.Fatal(err)
	}
	if len(entries.Entries) != 5 || entries.Entries[0].Activity != model.HoldReleased || entries.Entries[0].BalancePart != model.BalanceAvailable {
		t.Fatal("duplicate reserve/release legs", entries)
	}
	if err := s.Tx.TransactCtx(ctx, func(ctx context.Context, tx sqlx.Session) error {
		_, err := s.Accounts.Consume(ctx, tx, ids[1], []account.Entry{{System: account.SystemUSDClearing, Side: account.SideCredit, Amount: decimal.RequireFromString("1.25")}})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	_, body = call("GET", "/v1/user/account-entries?limit=1", token, nil, 200, "OK")
	if err := json.Unmarshal(body, &entries); err != nil || len(entries.Entries) != 1 || entries.Entries[0].Activity != model.HoldConsumed || entries.Entries[0].BalancePart != model.BalanceHeld || entries.Entries[0].BalanceAfter != "0.00" {
		t.Fatal(entries, err)
	}
	func() {
		if _, err := s.DB.Exec("RENAME TABLE account_hold TO account_hold_unavailable"); err != nil {
			t.Fatal(err)
		}
		defer func() {
			if _, err := s.DB.Exec("RENAME TABLE account_hold_unavailable TO account_hold"); err != nil {
				t.Error(err)
			}
		}()
		call("GET", "/v1/user/account-holds", token, nil, 500, "INTERNAL_ERROR")
	}()
	if page := read("/v1/user/account-holds"); len(page.Holds) != 2 {
		t.Fatal("recovery", page)
	}
}
func nextCursorForHold(t *testing.T, codec *idcodec.Codec, id int64) string {
	t.Helper()
	value, err := codec.Encode(id)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
