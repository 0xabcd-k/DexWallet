package chainassets

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
)

func TestBaseSendChecksNetworkNonceWireAndAcknowledgement(t *testing.T) {
	b, err := os.ReadFile("../chainreceipt/testdata/base-eth.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct{ Evidence BaseEvidence }
	if err = json.Unmarshal(b, &fixture); err != nil {
		t.Fatal(err)
	}
	var tx types.Transaction
	if err = json.Unmarshal(fixture.Evidence.Transaction, &tx); err != nil {
		t.Fatal(err)
	}
	raw, err := tx.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	sender, err := types.Sender(types.NewLondonSigner(tx.ChainId()), &tx)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, network, ack string
		broadcasts         int
	}{
		{"accepted", "0x14a34", tx.Hash().Hex(), 1},
		{"wrong-ack", "0x14a34", "0x01", 1},
		{"wrong-network", "0x2105", tx.Hash().Hex(), 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var q struct {
					Method string
					ID     int
					Params []string
				}
				if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
					t.Error(err)
					return
				}
				result := ""
				switch q.Method {
				case "eth_chainId":
					result = tc.network
				case "eth_getTransactionCount":
					if len(q.Params) != 2 || q.Params[0] != sender.Hex() || q.Params[1] != "pending" {
						t.Error(q.Params)
					}
					result = "0x6"
				case "eth_sendRawTransaction":
					calls++
					if len(q.Params) != 1 || q.Params[0] != hexutil.Encode(raw) {
						t.Error("original wire not sent")
					}
					result = tc.ack
				default:
					t.Error(q.Method)
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": q.ID, "result": result})
			}))
			defer server.Close()
			s, err := New(Config{Networks: []Endpoint{{Network: BaseSepolia, URL: server.URL}}}, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			ctx := context.Background()
			n, err := s.PendingBaseNonce(ctx, sender.Hex())
			if tc.name == "wrong-network" {
				if err == nil {
					t.Fatal("nonce accepted from wrong chain")
				}
			} else if err != nil || n != 6 {
				t.Fatal(n, err)
			}
			err = s.SubmitBaseETH(ctx, &tx)
			if (err == nil) != (tc.name == "accepted") || calls != tc.broadcasts {
				t.Fatal(calls, err)
			}
		})
	}
}
