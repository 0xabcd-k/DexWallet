//go:build integration

package server

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/api/internal/types"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
	"github.com/0xabcd-k/DexWallet/backend/logic/deposit"
	"github.com/0xabcd-k/DexWallet/backend/logic/user"
	shared "github.com/0xabcd-k/DexWallet/backend/svc"
	"github.com/mr-tron/base58"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

type solTestLimit struct{}

func (solTestLimit) Allow(context.Context, int64) error { return nil }

func checkSolOrderHTTP(t *testing.T, s *shared.ServiceContext, token string, call func(string, string, string, any, int, string) (http.Header, json.RawMessage)) {
	t.Helper()
	pub, sk, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	payer, sponsor, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	var height, fee atomic.Int64
	height.Store(100)
	fee.Store(10000)
	var simFail atomic.Bool
	rpc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string
			Params []json.RawMessage
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			return
		}
		var result any
		switch req.Method {
		case "getGenesisHash":
			result = "EtWTRABZaYq6iMfeYKouRu166VU2xqa1wcaWoxPkrZBG"
		case "getLatestBlockhash":
			result = map[string]any{"value": chainassets.SolanaBlock{Blockhash: "F1VvomZpGxaB9taEwSuT1WwAaqP99RYjQXoEkodmvxhA", LastValidBlockHeight: 120}}
		case "getBlockHeight":
			result = height.Load()
		case "getFeeForMessage":
			result = map[string]any{"context": map[string]int{"slot": 5}, "value": fee.Load()}
		case "simulateTransaction":
			var wire []byte
			var config map[string]any
			if len(req.Params) != 2 || json.Unmarshal(req.Params[0], &wire) != nil || json.Unmarshal(req.Params[1], &config) != nil ||
				len(wire) < 130 || wire[0] != 2 || !bytes.Equal(wire[1:65], make([]byte, 64)) || !bytes.Contains(wire[129:], []byte("dexwallet:deposit:")) || config["replaceRecentBlockhash"] != false || config["sigVerify"] != false {
				t.Error("unexpected simulation")
				return
			}
			if !bytes.Equal(wire[65:129], make([]byte, 64)) && !ed25519.Verify(pub, wire[129:], wire[65:129]) {
				t.Error("wrong signer slot")
				return
			}
			var rejection any
			if simFail.Load() {
				rejection = "InsufficientFundsForFee"
			}
			result = map[string]any{"context": map[string]int{"slot": 5}, "value": map[string]any{"err": rejection}}
		default:
			t.Error("unexpected RPC", req.Method)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": result})
	}))
	defer rpc.Close()
	now := time.Now()
	cfg := chainassets.Config{Networks: []chainassets.Endpoint{{Network: chainassets.SolanaDevnet, URL: rpc.URL}}, DepositQuotes: &chainassets.DepositQuoteConfig{Version: "sign-test", AsOf: now.Unix() - 60, ValidUntil: now.Unix() + 300, SolanaSOLReviewAfter: time.Minute,
		SolanaSOL: &chainassets.SolanaDepositFeeConfig{FeePayer: base58.Encode(payer), Recipient: "7Njq3pBVPhZWEoRh8w22eC2KBcGUhhCvwFFitsyTg7A5"}, Assets: []chainassets.DepositQuoteRate{{AssetID: "solana-devnet:sol", USDPerCoin: "100", EstimatedGas: "0"}}}}
	chain, err := chainassets.New(cfg, solTestLimit{})
	if err != nil {
		t.Fatal(err)
	}
	defer chain.Close()
	origChain, origQ, origO := s.ChainAssets, s.DepositConfirmations, s.SolDepositOrders
	defer func() { s.ChainAssets, s.DepositConfirmations, s.SolDepositOrders = origChain, origQ, origO }()
	conn := sqlx.NewSqlConnFromDB(s.DB)
	s.ChainAssets = chain
	s.DepositConfirmations = deposit.NewConfirmations(conn, s.Redis, s.Namespace)
	s.SolDepositOrders = deposit.NewSolOrders(conn, s.DepositConfirmations, s.Users, s.Tx, chain)
	const path = "/v1/user/sol-deposit-orders/"
	newQuote := func() types.DepositQuoteData {
		_, raw := call("POST", "/v1/user/deposit-quotes/preview", token, types.DepositQuoteRequest{AssetId: "solana-devnet:sol", TargetUsd: "1", WalletAddress: base58.Encode(pub)}, 200, "OK")
		var q types.DepositQuoteData
		if err = json.Unmarshal(raw, &q); err != nil || len(q.SolOrderIdx) != 32 {
			t.Fatal(q, err)
		}
		call("POST", "/v1/user/deposit-quotes/confirm", token, types.DepositQuoteConfirmRequest{QuoteIdx: q.QuoteIdx}, 200, "OK")
		return q
	}
	q, second := newQuote(), newQuote()
	if q.MessageSha256 == second.MessageSha256 || q.SolOrderIdx == second.SolOrderIdx {
		t.Fatal("two attempts share a signable message")
	}
	req := types.DepositQuoteConfirmRequest{QuoteIdx: q.QuoteIdx}
	sigReq := types.SolDepositAuthorizeRequest{QuoteIdx: q.QuoteIdx, UserSignature: strings.Repeat("1", 64)}
	call("POST", path+"authorize", token, sigReq, 404, "NOT_FOUND")
	simFail.Store(true)
	call("POST", path+"prepare", token, req, 422, "UNPROCESSABLE")
	simFail.Store(false)
	call("GET", path+q.QuoteIdx, token, nil, 404, "NOT_FOUND")
	_, raw := call("POST", path+"prepare", token, req, 200, "OK")
	var order types.SolDepositOrderData
	if err = json.Unmarshal(raw, &order); err != nil || order.State != deposit.AwaitingSignature || order.MessageSha256 != q.MessageSha256 || order.OrderIdx != q.SolOrderIdx || order.LastValidBlockHeight != 120 || order.ReviewAfter != q.ExpiresAt+60000 {
		t.Fatal(order, err)
	}
	msg, _ := base64.StdEncoding.DecodeString(order.MessageBase64)
	if !bytes.HasSuffix(msg, []byte("dexwallet:deposit:"+order.OrderIdx)) {
		t.Fatal("missing order memo")
	}
	call("POST", path+"authorize", token, sigReq, 400, "INVALID_ARGUMENT")
	sigReq.UserSignature = base58.Encode(ed25519.Sign(sk, msg))
	height.Store(121)
	call("POST", path+"authorize", token, sigReq, 409, "CONFLICT")
	height.Store(100)
	fee.Add(1)
	call("POST", path+"authorize", token, sigReq, 409, "CONFLICT")
	fee.Add(-1)
	claims, uid, err := s.Tokens.Parse(token)
	if err != nil {
		t.Fatal(err)
	}
	var otherID, otherVersion int64
	if err := s.DB.QueryRow("SELECT id,auth_version FROM user WHERE id<>? AND status='active' LIMIT 1", uid).Scan(&otherID, &otherVersion); err != nil {
		t.Fatal(err)
	}
	other, _, err := s.Tokens.Issue(otherID, otherVersion)
	if err != nil {
		t.Fatal(err)
	}
	call("GET", path+q.QuoteIdx, other, nil, 404, "NOT_FOUND")
	call("POST", path+"authorize", other, sigReq, 404, "NOT_FOUND")
	if _, err := s.DB.Exec("UPDATE user SET payment_restricted_until=UTC_TIMESTAMP()+INTERVAL 1 DAY WHERE id=?", uid); err != nil {
		t.Fatal(err)
	}
	call("POST", path+"authorize", token, sigReq, 423, "PAYMENT_RESTRICTED")
	if _, err := s.DB.Exec("UPDATE user SET payment_restricted_until=NULL WHERE id=?", uid); err != nil {
		t.Fatal(err)
	}
	actor := user.Actor{UserID: uid, AuthVersion: claims.AuthVersion}
	type outcome struct {
		order deposit.SolOrder
		err   error
	}
	results := make(chan outcome, 4)
	for range cap(results) {
		go func() {
			o, e := s.SolDepositOrders.Authorize(context.Background(), actor, q.QuoteIdx, sigReq.UserSignature)
			results <- outcome{o, e}
		}()
	}
	var at time.Time
	for range cap(results) {
		result := <-results
		if result.err != nil || !result.order.AcceptedAt.Valid {
			t.Fatal("concurrent acceptance", result.err)
		}
		if !at.IsZero() && !at.Equal(result.order.AcceptedAt.Time) {
			t.Fatal("acceptance time changed")
		}
		at = result.order.AcceptedAt.Time
	}
	_, accepted := call("POST", path+"authorize", token, sigReq, 200, "OK")
	var got types.SolDepositOrderData
	if err = json.Unmarshal(accepted, &got); err != nil || got.AcceptedAt <= 0 || got.State != deposit.Accepted {
		t.Fatal(got, err)
	}
	// The signature of the first order cannot authorize an equal payment with a different memo.
	call("POST", path+"prepare", token, types.DepositQuoteConfirmRequest{QuoteIdx: second.QuoteIdx}, 200, "OK")
	call("POST", path+"authorize", token, types.SolDepositAuthorizeRequest{QuoteIdx: second.QuoteIdx, UserSignature: sigReq.UserSignature}, 400, "INVALID_ARGUMENT")
	height.Store(121)
	for _, op := range []string{"get", "replay"} {
		var b json.RawMessage
		if op == "get" {
			_, b = call("GET", path+q.QuoteIdx, token, nil, 200, "OK")
		} else {
			_, b = call("POST", path+"authorize", token, sigReq, 200, "OK")
		}
		var replay types.SolDepositOrderData
		if json.Unmarshal(b, &replay) != nil || replay != got {
			t.Fatal(op, replay)
		}
	}
	// Keep an actual prepared attempt past its short offer deadline while the blockhash is still valid.
	height.Store(100)
	ctx := context.Background()
	short, err := chain.PreviewWalletDeposit(ctx, uid, q.AssetId, "1", q.WalletAddress)
	if err != nil {
		t.Fatal(err)
	}
	short.ExpiresAt = time.Now().Add(time.Second).UnixMilli()
	short, err = s.DepositConfirmations.Offer(ctx, uid, short)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.DepositConfirmations.Confirm(ctx, uid, short.QuoteIdx); err != nil {
		t.Fatal(err)
	}
	pending, err := s.SolDepositOrders.Prepare(ctx, actor, short.QuoteIdx)
	if err != nil {
		t.Fatal(err)
	}
	message, _ := base64.StdEncoding.DecodeString(pending.MessageBase64)
	time.Sleep(time.Until(time.UnixMilli(short.ExpiresAt)))
	call("POST", path+"authorize", token, types.SolDepositAuthorizeRequest{QuoteIdx: short.QuoteIdx, UserSignature: base58.Encode(ed25519.Sign(sk, message))}, 409, "CONFLICT")
	pending, err = s.SolDepositOrders.Get(ctx, uid, short.QuoteIdx)
	if err != nil || pending.AcceptedAt.Valid || pending.UserSignature != "" {
		t.Fatal("expired order mutated", err)
	}

	// Drive the accepted HTTP order with a node that loses the first submission
	// and temporarily cannot find it in history. No replacement signature is allowed.
	height.Store(100)
	net := &solExecutionNode{Service: chain}
	terms := chainassets.SolDepositTerms{Recipient: q.SolRecipient, GrossLamports: 10010000, FeeLamports: 10000}
	worker := func(terms chainassets.SolDepositTerms) *deposit.SolExecution {
		e, err := deposit.NewSolExecution(conn, net, q.QuoteIdx, terms, sponsor)
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
	changed := terms
	changed.GrossLamports++
	if row, err := worker(changed).Step(ctx); err == nil || row.TxSignature.Valid {
		t.Fatal("wrong amount reached signer")
	}
	fee.Add(1)
	if row, err := worker(terms).Step(ctx); err == nil || row.TxSignature.Valid {
		t.Fatal("fee increase reached signer")
	}
	fee.Add(-1)
	signed, err := worker(terms).Step(ctx)
	if err != nil || signed.ExecutionState != deposit.SolSigned || signed.SponsorPolicy == "" || len(net.sends) != 0 {
		t.Fatal("persist-before-submit", err)
	}
	if _, err = worker(terms).Step(ctx); !errors.Is(err, errDroppedSolResponse) {
		t.Fatal("expected lost acknowledgement", err)
	}
	resent, err := worker(terms).Step(ctx)
	if err != nil || resent.ExecutionState != deposit.SolSubmitted || len(net.sends) != 2 || !bytes.Equal(net.sends[0], net.sends[1]) ||
		base58.Encode(net.sends[0][1:65]) != signed.TxSignature.String || !ed25519.Verify(payer, msg, net.sends[0][1:65]) {
		t.Fatal("resubmission replaced original wire", err)
	}
	height.Store(121)
	review, err := worker(terms).Step(ctx)
	if err != nil || review.ExecutionState != deposit.SolReview || len(net.sends) != 2 {
		t.Fatal("expired blockhash", err)
	}
	height.Store(100)
	if _, err = worker(terms).Step(ctx); err != nil || len(net.sends) != 2 {
		t.Fatal("review resumed broadcasting", err)
	}
	net.final = true
	failed, err := worker(terms).Step(ctx)
	if err != nil || failed.ExecutionState != deposit.SolFailed || failed.ActualLamportsFee != terms.FeeLamports {
		t.Fatal("missing late failed receipt", err)
	}
	if _, err = worker(changed).Step(ctx); err == nil {
		t.Fatal("terminal recovery accepted another amount")
	}
	_, raw = call("GET", path+q.QuoteIdx, token, nil, 200, "OK")
	var result types.SolDepositOrderData
	if err = json.Unmarshal(raw, &result); err != nil || result.TxSignature != signed.TxSignature.String || result.ExecutionState != deposit.SolFailed || result.ActualFeeLamports != terms.FeeLamports {
		t.Fatal("recovery lookup", result, err)
	}
	call("GET", path+q.QuoteIdx, other, nil, 404, "NOT_FOUND")
}

var errDroppedSolResponse = errors.New("test node dropped response")

type solExecutionNode struct {
	*chainassets.Service
	sends [][]byte
	final bool
}

func (n *solExecutionNode) SubmitSolDeposit(_ context.Context, wire []byte) error {
	n.sends = append(n.sends, bytes.Clone(wire))
	if len(n.sends) == 1 {
		return errDroppedSolResponse
	}
	return nil
}
func (n *solExecutionNode) ObserveSolDeposit(_ context.Context, _ []byte, terms chainassets.SolDepositTerms) (chainassets.SolDepositObservation, error) {
	if n.final {
		return chainassets.SolDepositObservation{Seen: true, Finalized: true, Failed: true, Slot: 99, Fee: terms.FeeLamports}, nil
	}
	return chainassets.SolDepositObservation{}, nil
}
