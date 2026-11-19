package chainassets

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
)

func TestTRONEvidenceUsesPinnedNetworkAndSolidBlocks(t *testing.T) {
	b, err := os.ReadFile("../chainreceipt/testdata/tron-usdt.json")
	if err != nil {
		t.Fatal(err)
	}
	var f struct{ Evidence TRONEvidence }
	if err = json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	var info struct {
		ID          string
		BlockNumber int64
	}
	_ = json.Unmarshal(f.Evidence.Info, &info)
	var calls, mode atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var query map[string]any
		if r.Method != http.MethodPost || json.NewDecoder(r.Body).Decode(&query) != nil || query["visible"] != false || r.Header.Get("TRON-PRO-API-KEY") != "test-only" {
			t.Error("wrong request shape")
		}
		var result json.RawMessage
		switch r.URL.Path {
		case "/walletsolidity/getblockbynum":
			switch query["num"] {
			case float64(0):
				result = f.Evidence.Genesis
				if mode.Load() == 1 {
					result = json.RawMessage(`{"blockID":"` + TRONMainnetGenesis + `"}`)
				}
			case float64(info.BlockNumber):
				result = f.Evidence.Block
			default:
				t.Error("wrong block query", query["num"])
			}
		case "/walletsolidity/gettransactionbyid":
			if query["value"] != info.ID {
				t.Error("wrong txID")
			}
			result = f.Evidence.Transaction
		case "/walletsolidity/gettransactioninfobyid":
			result = f.Evidence.Info
			switch mode.Load() {
			case 2:
				result = json.RawMessage(`{}`)
			case 3:
				result = json.RawMessage(`null`)
			case 4:
				result = json.RawMessage(`{"Error":"unavailable"}`)
			case 5:
				result = json.RawMessage(`{"blockNumber":-1}`)
			}
		case "/walletsolidity/getnowblock":
			result = f.Evidence.SolidHead
		default:
			t.Error("non-Solidity or unexpected query", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(result)
	}))
	defer server.Close()
	source, err := New(Config{Networks: []Endpoint{{Network: TRONNile, URL: server.URL, APIKey: "test-only"}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	got, err := source.ReadTRONEvidence(context.Background(), TRONNile, strings.ToUpper(info.ID))
	if err != nil || calls.Load() != 5 || string(got.Block) != string(f.Evidence.Block) {
		t.Fatal(err, calls.Load())
	}
	for _, tt := range []struct {
		mode, calls int32
		wantError   bool
	}{{1, 1, true}, {2, 3, false}, {3, 3, true}, {4, 3, true}, {5, 3, true}} {
		mode.Store(tt.mode)
		calls.Store(0)
		_, err = source.ReadTRONEvidence(context.Background(), TRONNile, info.ID)
		if (err != nil) != tt.wantError || calls.Load() != tt.calls {
			t.Fatal(tt, err, calls.Load())
		}
	}
	calls.Store(0)
	for _, network := range []string{TRONMainnet, SolanaDevnet} {
		if _, err = source.ReadTRONEvidence(context.Background(), network, info.ID); err == nil {
			t.Fatal(network)
		}
	}
	if _, err = source.ReadTRONEvidence(context.Background(), TRONNile, "fake"); err == nil || calls.Load() != 0 {
		t.Fatal(err, calls.Load())
	}
}
