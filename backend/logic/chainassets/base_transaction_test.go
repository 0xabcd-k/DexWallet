package chainassets

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestBaseEvidenceCanonicalHistoricalFeeQueries(t *testing.T) {
	data, err := os.ReadFile("../chainreceipt/testdata/base-usdc.json")
	if err != nil {
		t.Fatal(err)
	}
	var f struct{ Evidence BaseEvidence }
	if err = json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	var rcpt struct{ BlockHash, BlockNumber string }
	_ = json.Unmarshal(f.Evidence.Receipt, &rcpt)
	calls := 0
	badNetwork := false
	missing := false
	traceError := false
	pendingFinality := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string
			Params []json.RawMessage
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		calls++
		var result any
		switch req.Method {
		case "eth_chainId":
			result = "0x14a34"
			if badNetwork {
				result = "0x2105"
			}
		case "eth_getTransactionByHash":
			result = f.Evidence.Transaction
		case "eth_getTransactionReceipt":
			result = f.Evidence.Receipt
			if missing {
				result = nil
			}
		case "eth_getBlockByNumber":
			var tag string
			_ = json.Unmarshal(req.Params[0], &tag)
			if tag == "finalized" {
				result = f.Evidence.Finalized
				if pendingFinality {
					result = map[string]string{"number": "0x1"}
				}
			} else if tag == rcpt.BlockNumber {
				result = f.Evidence.Block
			} else {
				t.Error("unexpected block tag", tag)
			}
		case "debug_traceTransaction":
			var hash string
			var config struct {
				Tracer       string
				Timeout      string
				TracerConfig map[string]bool
			}
			_ = json.Unmarshal(req.Params[0], &hash)
			_ = json.Unmarshal(req.Params[1], &config)
			if hash != "0xdb945055aa01454df203b52e16cecf1548e6c826627da9aaba98f908db3a3d17" || config.Tracer != "callTracer" || config.Timeout != "15s" || config.TracerConfig["onlyTopCall"] || len(config.TracerConfig) != 1 {
				t.Error("incomplete trace request", config)
			}
			if traceError {
				_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "error": map[string]any{"code": -32601, "message": "method unavailable"}, "result": map[string]any{"type": "CALL"}})
				return
			}
			result = map[string]any{"type": "CALL", "calls": []any{}}
		case "eth_call":
			var call map[string]string
			var block map[string]any
			_ = json.Unmarshal(req.Params[0], &call)
			_ = json.Unmarshal(req.Params[1], &block)
			// Query the receipt gas amount and its canonical historical block.
			if call["to"] != BaseGasPriceOracle || len(call["data"]) != 74 || !strings.HasSuffix(call["data"], "000191d4") || block["blockHash"] != rcpt.BlockHash || block["requireCanonical"] != true {
				t.Error(call, block)
			}
			result = f.Evidence.OperatorFee
		default:
			t.Error(req.Method)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": result})
	}))
	defer server.Close()
	source, err := New(Config{Networks: []Endpoint{{Network: BaseSepolia, URL: server.URL}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	hash := "0xdb945055aa01454df203b52e16cecf1548e6c826627da9aaba98f908db3a3d17"
	proof, err := source.ReadBaseEvidence(context.Background(), BaseSepolia, hash)
	if err != nil || calls != 6 || string(proof.OperatorFee) != string(f.Evidence.OperatorFee) {
		t.Fatal(err, calls)
	}
	calls = 0
	proof, err = source.ReadBaseCallEvidence(context.Background(), BaseSepolia, hash)
	if err != nil || calls != 7 || len(proof.CallTrace) == 0 {
		t.Fatal(err, calls)
	}
	pendingFinality = true
	calls = 0
	proof, err = source.ReadBaseDepositETHEvidence(context.Background(), BaseSepolia, hash)
	if err != nil || len(proof.CallTrace) != 0 || len(proof.OperatorFee) != 0 || calls != 5 {
		t.Fatal("premature trace or fee lookup", calls, err)
	}
	pendingFinality = false
	calls = 0
	proof, err = source.ReadBaseDepositETHEvidence(context.Background(), BaseSepolia, hash)
	if err != nil || len(proof.CallTrace) == 0 || len(proof.OperatorFee) == 0 || calls != 7 {
		t.Fatal("final ETH evidence", calls, err)
	}
	traceError = true
	calls = 0
	if _, err = source.ReadBaseCallEvidence(context.Background(), BaseSepolia, hash); err == nil || calls != 7 {
		t.Fatal(err, calls)
	}
	traceError = false
	badNetwork = true
	calls = 0
	if _, err = source.ReadBaseEvidence(context.Background(), BaseSepolia, hash); err == nil || calls != 1 {
		t.Fatal(err, calls)
	}
	badNetwork = false
	missing = true
	calls = 0
	proof, err = source.ReadBaseEvidence(context.Background(), BaseSepolia, hash)
	if err != nil || calls != 3 || string(proof.Receipt) != "null" {
		t.Fatal(err, calls)
	}
	calls = 0
	for _, network := range []string{BaseMainnet, SolanaDevnet} {
		if _, err = source.ReadBaseEvidence(context.Background(), network, hash); err == nil {
			t.Fatal(network)
		}
	}
	if _, err = source.ReadBaseEvidence(context.Background(), BaseSepolia, "fake"); err == nil || calls != 0 {
		t.Fatal(err, calls)
	}
}
