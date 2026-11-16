//go:build integration

package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/0xabcd-k/DexWallet/backend/api/internal/types"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
	shared "github.com/0xabcd-k/DexWallet/backend/svc"
)

// The router, login/session and Redis limiter are real; only the public RPC is a fake.
func checkChainAssetsHTTP(t *testing.T, s *shared.ServiceContext, token string, call func(string, string, string, any, int, string) (http.Header, json.RawMessage)) {
	t.Helper()
	defer func() {
		ctx := context.Background()
		iter := s.Redis.Scan(ctx, 0, "rate:"+s.Namespace+":*", 100).Iterator()
		for iter.Next(ctx) {
			if err := testRun.Record("redis", iter.Val()); err != nil {
				t.Error(err)
			}
		}
		if err := iter.Err(); err != nil {
			t.Error(err)
		}
	}()
	call("GET", "/v1/user/chain-assets", "", nil, 401, "UNAUTHENTICATED")
	request := types.ChainBalancesRequest{Network: chainassets.BaseSepolia, Address: "0x9858EfFD232B4033E47d90003D41EC34EcaEda94"}
	call("POST", "/v1/user/chain-assets/balances", "", request, 401, "UNAUTHENTICATED")
	call("POST", "/v1/user/chain-assets/balances", token, request, 503, "SERVICE_UNAVAILABLE")
	// Preparation works with all RPCs disabled, and never authorizes a payment.
	targetInput := types.ChainTargetRequest{Network: request.Network, AssetId: request.Network + ":eth", Address: request.Address}
	call("POST", "/v1/user/chain-transfer-targets/resolve", "", targetInput, 401, "UNAUTHENTICATED")
	_, targetRaw := call("POST", "/v1/user/chain-transfer-targets/resolve", token, targetInput, 200, "OK")
	var target types.ChainTargetData
	if err := json.Unmarshal(targetRaw, &target); err != nil {
		t.Fatal(err)
	}
	call("POST", "/v1/user/chain-transfer-targets/resolve", token, types.ChainTargetRequest{ReceiveCode: target.ReceiveCode}, 200, "OK")
	call("POST", "/v1/user/chain-transfer-targets/resolve", token, types.ChainTargetRequest{ReceiveCode: target.ReceiveCode + "&amount=1"}, 400, "INVALID_ARGUMENT")
	previewInput := types.ChainTransferPreviewRequest{Network: target.Network, AssetId: target.AssetId, Address: target.Address, Amount: "1.000000000000000001"}
	call("POST", "/v1/user/chain-transfers/preview", "", previewInput, 401, "UNAUTHENTICATED")
	_, previewRaw := call("POST", "/v1/user/chain-transfers/preview", token, previewInput, 200, "OK")
	var preview types.ChainTransferPreviewData
	if err := json.Unmarshal(previewRaw, &preview); err != nil {
		t.Fatal(err)
	}
	if preview.Target != target || preview.Amount != previewInput.Amount || preview.CanSubmit || preview.BlockReason != chainassets.ChannelNotOpen {
		t.Fatal(preview)
	}
	previewInput.Amount = "0.0000000000000000001"
	call("POST", "/v1/user/chain-transfers/preview", token, previewInput, 400, "INVALID_ARGUMENT")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input struct{ Method string }
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Error(err)
		}
		result := "0x0"
		switch input.Method {
		case "eth_chainId":
			result = "0x14a34"
		case "eth_call":
			result = fmt.Sprintf("0x%064x", 1234567)
		case "eth_getBalance":
		default:
			t.Error(input.Method)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": result})
	}))
	defer upstream.Close()
	service, err := chainassets.New(chainassets.Config{Networks: []chainassets.Endpoint{{Network: chainassets.BaseSepolia, URL: upstream.URL}}}, chainassets.NewRedisLimiter(s.Redis, s.Namespace, 1))
	if err != nil {
		t.Fatal(err)
	}
	old := s.ChainAssets
	s.ChainAssets = service
	defer func() { s.ChainAssets = old; service.Close() }()
	_, raw := call("GET", "/v1/user/chain-assets", token, nil, 200, "OK")
	var catalog types.ChainAssetCatalogData
	if err = json.Unmarshal(raw, &catalog); err != nil {
		t.Fatal(err)
	}
	if len(catalog.Networks) != 8 {
		t.Fatal(catalog)
	}
	invalid := request
	invalid.Address = "bad-address"
	call("POST", "/v1/user/chain-assets/balances", token, invalid, 400, "INVALID_ARGUMENT")
	for i := 0; i < 5; i++ {
		_, raw = call("POST", "/v1/user/chain-assets/balances", token, request, 200, "OK")
		var result types.ChainBalancesData
		if err = json.Unmarshal(raw, &result); err != nil {
			t.Fatal(err)
		}
		if result.Items[0].Quantity != "0" || result.Items[1].Quantity != "1.234567" {
			t.Fatal(result)
		}
	}
	call("POST", "/v1/user/chain-assets/balances", token, request, 429, "RATE_LIMITED")
}
