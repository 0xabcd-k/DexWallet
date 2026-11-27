//go:build integration

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/api/internal/types"
	"github.com/0xabcd-k/DexWallet/backend/logic/deposit"
	model "github.com/0xabcd-k/DexWallet/backend/model/deposit"
	shared "github.com/0xabcd-k/DexWallet/backend/svc"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

func checkDepositConfirmationHTTP(t *testing.T, s *shared.ServiceContext, token string, quote types.DepositQuoteData, call func(string, string, string, any, int, string) (http.Header, json.RawMessage)) {
	t.Helper()
	const confirm = "/v1/user/deposit-quotes/confirm"
	lookup := "/v1/user/deposit-quotes/" + quote.QuoteIdx
	input := types.DepositQuoteConfirmRequest{QuoteIdx: quote.QuoteIdx}
	if len(quote.QuoteIdx) != 32 || quote.MaxGas != quote.EstimatedGas {
		t.Fatal(quote)
	}
	_, user, err := s.Tokens.Parse(token)
	if err != nil {
		t.Fatal(err)
	}
	var otherUser, version int64
	if err := s.DB.QueryRow("SELECT id,auth_version FROM user WHERE id<>? AND status='active' LIMIT 1", user).Scan(&otherUser, &version); err != nil {
		t.Fatal(err)
	}
	other, _, err := s.Tokens.Issue(otherUser, version)
	if err != nil {
		t.Fatal(err)
	}
	call("POST", confirm, "", input, 401, "UNAUTHENTICATED")
	call("GET", lookup, token, nil, 404, "NOT_FOUND")
	call("POST", confirm, other, input, 404, "NOT_FOUND")
	call("POST", confirm, token, types.DepositQuoteConfirmRequest{QuoteIdx: "bad"}, 400, "INVALID_ARGUMENT")

	ctx := context.Background()
	// Concurrent deliveries must all observe the same durable terms and admission time.
	results := make(chan deposit.Confirmation, 6)
	errs := make(chan error, cap(results))
	for range cap(results) {
		go func() {
			record, err := s.DepositConfirmations.Confirm(ctx, user, quote.QuoteIdx)
			results <- record
			errs <- err
		}()
	}
	var first deposit.Confirmation
	for range cap(results) {
		record := <-results
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
		if first.ConfirmedAt.IsZero() {
			first = record
		} else if first != record {
			t.Fatal("concurrent confirmations differ")
		}
	}
	if first.Quote.Recipient != "7Njq3pBVPhZWEoRh8w22eC2KBcGUhhCvwFFitsyTg7A5" || first.Quote.GasUSDPerCoin != "100" || first.Quote.PayDecimals != 9 || first.Quote.RuleVersion != "http-fixture" {
		t.Fatal("lost execution terms", first)
	}
	// Simulate a restarted service with new offers disabled and no original cache or rate table.
	conn := sqlx.NewSqlConnFromDB(s.DB)
	s.DepositConfirmations = deposit.NewConfirmations(conn, nil, s.Namespace)
	for _, op := range []struct {
		method, path string
		input        any
	}{{"POST", confirm, map[string]any{"quoteIdx": quote.QuoteIdx, "maxGas": "99", "targetUsd": "99"}}, {"GET", lookup, nil}} {
		_, raw := call(op.method, op.path, token, op.input, 200, "OK")
		var stored types.ConfirmedDepositQuoteData
		if err := json.Unmarshal(raw, &stored); err != nil || stored.Quote != quote || stored.ConfirmedAt != first.ConfirmedAt.UnixMilli() {
			t.Fatal(stored, err)
		}
	}
	call("GET", lookup, other, nil, 404, "NOT_FOUND")
	call("POST", confirm, other, input, 404, "NOT_FOUND")
	// A stale cache entry must never admit a new confirmation once the database deadline passes.
	const late = "00000000000000000000000000000001"
	rows := model.NewDepositQuoteConfirmationModel(conn)
	if err := rows.ConfirmBeforeExpiry(ctx, late, user, "{}", time.Now().Add(-time.Minute).UTC(), time.Now().Add(-time.Second).UTC()); err != nil {
		t.Fatal(err)
	}
	call("GET", "/v1/user/deposit-quotes/"+late, token, nil, 404, "NOT_FOUND")
	call("POST", confirm, token, types.DepositQuoteConfirmRequest{QuoteIdx: late}, 404, "NOT_FOUND")
}
