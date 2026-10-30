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
	"github.com/0xabcd-k/DexWallet/backend/session"
	shared "github.com/0xabcd-k/DexWallet/backend/svc"
	"github.com/golang-jwt/jwt/v4"
	"github.com/shopspring/decimal"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

// Called with the real authenticated users/router from TestProductSessionHTTP.
// Only synthetic fixture writes invoke the internal ledger; no write route exists.
func checkLedgerHTTP(t *testing.T, s *shared.ServiceContext, userID, otherID int64, token string, call func(string, string, string, any, int, string) (http.Header, json.RawMessage)) {
	t.Helper()
	ctx := context.Background()
	call("GET", "/v1/user/account-entries", "", nil, 401, "UNAUTHENTICATED")
	read := func(path string) types.AccountEntriesData {
		t.Helper()
		_, body := call("GET", path, token, nil, 200, "OK")
		var data types.AccountEntriesData
		if err := json.Unmarshal(body, &data); err != nil {
			t.Fatal(err)
		}
		return data
	}
	if data := read("/v1/user/account-entries"); len(data.Entries) != 0 || data.NextCursor != "" {
		t.Fatal("initial statement", data)
	}
	for _, query := range []string{"kind=bad", "before=1", "limit=0", "limit=101", "limit=bad"} {
		call("GET", "/v1/user/account-entries?"+query, token, nil, 400, "INVALID_ARGUMENT")
	}
	posts := []account.Posting{
		{Source: account.Source{Type: account.BusinessDeposit, ID: 1, Action: account.ActionPost}, Entries: []account.Entry{account.Credit(userID, model.KindUSD, decimal.NewFromInt(20)), {System: account.SystemUSDClearing, Side: account.SideDebit, Amount: decimal.NewFromInt(20)}}},
		{Source: account.Source{Type: account.BusinessTransfer, ID: 1, Action: account.ActionPost}, Entries: []account.Entry{account.Debit(userID, model.KindUSD, decimal.NewFromInt(5)), account.Credit(otherID, model.KindUSD, decimal.NewFromInt(5))}},
		{Source: account.Source{Type: account.BusinessReward, ID: 1, Action: account.ActionPost}, Entries: []account.Entry{account.Credit(userID, model.KindRewardUSD, decimal.RequireFromString("2.35")), {System: account.SystemRewardExpense, Side: account.SideDebit, Amount: decimal.RequireFromString("2.35")}}},
	}
	for _, p := range posts {
		if err := s.Tx.TransactCtx(ctx, func(ctx context.Context, tx sqlx.Session) error { _, err := s.Accounts.Post(ctx, tx, p); return err }); err != nil {
			t.Fatal(err)
		}
	}
	page := read(fmt.Sprintf("/v1/user/account-entries?userId=%d&limit=1", otherID))
	if len(page.Entries) != 1 || page.NextCursor == "" || page.Entries[0].Kind != model.KindRewardUSD || page.Entries[0].Amount != "2.35" || page.Entries[0].BalanceAfter != "2.35" {
		t.Fatal("owner and precision", page)
	}
	entryCodec, _ := idcodec.New(s.Config.IDSecret, idcodec.LedgerEntry)
	if id, err := entryCodec.Decode(page.NextCursor); err != nil || id <= 0 {
		t.Fatal("cursor is not entry idx", err)
	}
	second := read("/v1/user/account-entries?limit=1&before=" + page.NextCursor)
	if len(second.Entries) != 1 || second.Entries[0].Direction != "out" || second.Entries[0].Amount != "5.00" || second.Entries[0].BalanceAfter != "15.00" {
		t.Fatal("wrong transfer side or counterpart leaked", second)
	}
	last := read("/v1/user/account-entries?before=" + second.NextCursor)
	if len(last.Entries) != 1 || last.NextCursor != "" || last.Entries[0].Amount != "20.00" {
		t.Fatal("pagination", last)
	}
	usd := read("/v1/user/account-entries?kind=usd_balance")
	if len(usd.Entries) != 2 {
		t.Fatal("filter", usd)
	}
	for _, entry := range usd.Entries {
		if entry.Kind != model.KindUSD || entry.Unit != model.UnitUSD {
			t.Fatal(entry)
		}
	}
	if data := read("/v1/user/account-entries?kind=task_points"); len(data.Entries) != 0 {
		t.Fatal(data)
	}
	for _, entry := range append(page.Entries, usd.Entries...) {
		if _, err := time.Parse(time.RFC3339Nano, entry.CreatedAt); err != nil {
			t.Fatal(err)
		}
		kind, _ := account.BusinessType(entry.BusinessType).IDKind()
		codec, _ := idcodec.New(s.Config.IDSecret, kind)
		if id, err := codec.Decode(entry.BusinessIdx); err != nil || id != 1 {
			t.Fatal("source idx", id, err)
		}
	}
	// A cursor is only a position, never an account selector.
	var otherEntry int64
	if err := s.DB.QueryRow("SELECT e.id FROM ledger_entry e JOIN user_account a ON a.id=e.account_id WHERE a.user_id=?", otherID).Scan(&otherEntry); err != nil {
		t.Fatal(err)
	}
	foreignCursor, _ := entryCodec.Encode(otherEntry)
	filtered := read("/v1/user/account-entries?before=" + foreignCursor)
	if len(filtered.Entries) != 2 || filtered.Entries[0].Direction != "out" {
		t.Fatal("cursor crossed ownership", filtered)
	}
	claims, _, err := s.Tokens.Parse(token)
	if err != nil {
		t.Fatal(err)
	}
	claims.IssuedAt = jwt.NewNumericDate(time.Now().Add(-48 * time.Hour))
	claims.ExpiresAt = jwt.NewNumericDate(claims.IssuedAt.Add(session.Lifetime))
	old, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(sessionTestSecret))
	if err != nil {
		t.Fatal(err)
	}
	h, _ := call("GET", "/v1/user/account-entries", old, nil, 200, "OK")
	if h.Get(session.TokenHeader) == "" {
		t.Fatal("statement did not renew near-expiry token")
	}
	func() {
		if _, err := s.DB.Exec("RENAME TABLE ledger_entry TO ledger_entry_unavailable"); err != nil {
			t.Fatal(err)
		}
		defer func() {
			if _, err := s.DB.Exec("RENAME TABLE ledger_entry_unavailable TO ledger_entry"); err != nil {
				t.Error(err)
			}
		}()
		call("GET", "/v1/user/account-entries", token, nil, 500, "INTERNAL_ERROR")
	}()
	if data := read("/v1/user/account-entries"); len(data.Entries) != 3 {
		t.Fatal("recovery", data)
	}
}
