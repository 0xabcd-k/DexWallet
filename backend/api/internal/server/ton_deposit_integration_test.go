//go:build integration

package server

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/api/internal/types"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
	"github.com/0xabcd-k/DexWallet/backend/logic/deposit"
	shared "github.com/0xabcd-k/DexWallet/backend/svc"
)

func checkTONDepositHTTP(t *testing.T, s *shared.ServiceContext, token string, call func(string, string, string, any, int, string) (http.Header, json.RawMessage)) {
	t.Helper()
	for _, proofName := range []string{"credit", "jetton-repeat"} {
		raw, err := os.ReadFile("../../../logic/chainreceipt/testdata/ton-self-deposit-" + proofName + ".json")
		if err != nil {
			t.Fatal(err)
		}
		var proof struct {
			Quote                         chainassets.DepositPreview
			UserSignature, OrderSignature string
		}
		if err = json.Unmarshal(raw, &proof); err != nil {
			t.Fatal(err)
		}
		q := proof.Quote
		_, owner, err := s.Tokens.Parse(token)
		if err != nil {
			t.Fatal(err)
		}
		snap, _ := json.Marshal(q)
		if _, err = s.DB.Exec("INSERT INTO deposit_quote_confirmation(quote_idx,user_id,snapshot_json,quote_expires_at,confirmed_at) VALUES(?,?,?,?,?)", q.QuoteIdx, owner, snap, time.UnixMilli(q.ExpiresAt).UTC(), time.UnixMilli(q.QuotedAt).UTC()); err != nil {
			t.Fatal(err)
		}
		_, saved := call("GET", "/v1/user/deposit-quotes/"+q.QuoteIdx, token, nil, 200, "OK")
		var confirmed types.ConfirmedDepositQuoteData
		if err = json.Unmarshal(saved, &confirmed); err != nil || confirmed.Quote.TonAuthorizationHash != q.TON.AuthorizationHash || confirmed.Quote.TonSimulatedGas != q.TON.SimulatedGas || confirmed.Quote.TonRecipient != q.Recipient || confirmed.Quote.TonJettonWalletCode != q.TON.JettonWalletCode || confirmed.Quote.TonSenderPaidGas != q.TON.SenderPaidGas {
			t.Fatal("TON DTO", err)
		}
		path := "/v1/user/ton-deposit-orders/" + q.QuoteIdx
		call("GET", path, "", nil, 401, "UNAUTHENTICATED")
		_, data := call("GET", path, token, nil, 200, "OK")
		var order types.TonDepositOrderData
		if err = json.Unmarshal(data, &order); err != nil || order.State != deposit.TonExpired {
			t.Fatal("expired TON order", err)
		}
		sig := types.TonDepositAuthorizeRequest{QuoteIdx: q.QuoteIdx, UserSignature: proof.UserSignature, OrderSignature: strings.Repeat("0", 128)}
		call("POST", "/v1/user/ton-deposit-orders/authorize", token, sig, 400, "INVALID_ARGUMENT")
		sig.OrderSignature = proof.OrderSignature
		call("POST", "/v1/user/ton-deposit-orders/authorize", token, sig, 409, "CONFLICT")
	}
}
