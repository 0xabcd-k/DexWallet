package chainassets

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mr-tron/base58"
)

func TestSolanaEvidenceRequestsHistoryAndFinality(t *testing.T) {
	signature := base58.Encode(make([]byte, 64))
	methods := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Method string
			Params []json.RawMessage
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		methods = append(methods, request.Method)
		var result any
		switch request.Method {
		case "getGenesisHash":
			result = "EtWTRABZaYq6iMfeYKouRu166VU2xqa1wcaWoxPkrZBG"
		case "getTransaction":
			var options map[string]any
			_ = json.Unmarshal(request.Params[1], &options)
			if options["commitment"] != "finalized" || options["encoding"] != "jsonParsed" || options["maxSupportedTransactionVersion"] != float64(0) {
				t.Error(options)
			}
		case "getSignatureStatuses":
			var options map[string]bool
			_ = json.Unmarshal(request.Params[1], &options)
			if !options["searchTransactionHistory"] {
				t.Error(options)
			}
			result = map[string]any{"value": []any{nil}, "context": map[string]any{"slot": 123}}
		default:
			t.Error(request.Method)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": result})
	}))
	defer server.Close()
	source, err := New(Config{Networks: []Endpoint{{Network: SolanaDevnet, URL: server.URL}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	proof, err := source.ReadSolanaEvidence(context.Background(), SolanaDevnet, signature)
	if err != nil || string(proof.Transaction) != "null" || len(methods) != 3 {
		t.Fatal(proof, err, methods)
	}
	for _, network := range []string{SolanaMainnet, BaseMainnet} {
		if _, err := source.ReadSolanaEvidence(context.Background(), network, signature); err == nil {
			t.Fatal(network)
		}
	}
	if _, err := source.ReadSolanaEvidence(context.Background(), SolanaDevnet, "fake-tx"); err == nil {
		t.Fatal("bad signature")
	}
	if len(methods) != 3 {
		t.Fatal("invalid input queried RPC")
	}
}
func TestSolanaEvidenceRejectsWrongNetworkBeforeTransaction(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": "wrong-genesis"})
	}))
	defer server.Close()
	source, _ := New(Config{Networks: []Endpoint{{Network: SolanaDevnet, URL: server.URL}}}, nil)
	defer source.Close()
	if _, err := source.ReadSolanaEvidence(context.Background(), SolanaDevnet, base58.Encode(make([]byte, 64))); err == nil || calls != 1 {
		t.Fatal(err, calls)
	}
}
