package chainassets

import (
	"context"
	"encoding/base64"
	"github.com/mr-tron/base58"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSubmitSolanaDevnetIdentityAndAcknowledgement(t *testing.T) {
	wire := append([]byte{1}, make([]byte, 130)...)
	sig := base58.Encode(wire[1:65])
	network := "wrong"
	accepted := "wrong"
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, params := rpcInput(t, r)
		switch method {
		case "getGenesisHash":
			rpcResult(w, network)
		case "sendTransaction":
			calls++
			if len(params) != 2 || string(params[0]) != `"`+base64.StdEncoding.EncodeToString(wire)+`"` || string(params[1]) != `{"encoding":"base64","maxRetries":0,"preflightCommitment":"confirmed","skipPreflight":false}` {
				t.Error("unexpected broadcast params", params)
			}
			rpcResult(w, accepted)
		default:
			t.Error(method)
		}
	}))
	defer server.Close()
	assets, err := New(Config{Networks: []Endpoint{{Network: SolanaDevnet, URL: server.URL}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer assets.Close()
	if err = assets.SubmitSolana(context.Background(), wire); err == nil || calls != 0 {
		t.Fatal("wrong genesis", err, calls)
	}
	network = "EtWTRABZaYq6iMfeYKouRu166VU2xqa1wcaWoxPkrZBG"
	if err = assets.SubmitSolana(context.Background(), wire); err == nil || calls != 1 {
		t.Fatal("wrong ack", err, calls)
	}
	accepted = sig
	if err = assets.SubmitSolana(context.Background(), wire); err != nil || calls != 2 {
		t.Fatal(err, calls)
	}
}
