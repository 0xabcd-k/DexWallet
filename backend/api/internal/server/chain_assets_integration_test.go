//go:build integration

package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/api/internal/types"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
	"github.com/0xabcd-k/DexWallet/backend/logic/deposit"
	shared "github.com/0xabcd-k/DexWallet/backend/svc"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

const (
	baseFeePayer  = "0xa59C992Aa84657df4EbFF6F04E11a1632f6DB1FA"
	baseFeeWallet = "0xecd14F7586f5053CECae148Ae26309C2277D86A8"
	baseFeeHash   = "0xb492cc123ce8af6c3900fdd07ae463dac2f209c2f96220c1ce62551ef59dcc8f"
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
	type splSnapshot struct {
		Mint, SourceATA string
		Addresses       struct{ Recipient string }
		AccountSnapshot json.RawMessage
		RentLamports    uint64
	}
	loadSnapshot := func(token, state string) (f splSnapshot) {
		t.Helper()
		data, err := os.ReadFile("../../../logic/chainassets/testdata/solana-" + token + "-deposit-" + state + ".json")
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(data, &f); err != nil {
			t.Fatal(err)
		}
		return f
	}
	usdcFixture := loadSnapshot("usdc", "new")
	usdtFixture := loadSnapshot("usdt", "existing")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Method string
			Params []json.RawMessage
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Error(err)
		}
		var result any = "0x0"
		switch input.Method {
		case "eth_chainId":
			result = "0x14a34"
		case "eth_getBlockByNumber":
			result = map[string]string{"hash": baseFeeHash, "number": "0x123", "timestamp": fmt.Sprintf("0x%x", time.Now().Unix()), "baseFeePerGas": "0x4c4b40"}
		case "eth_getCode":
			result = "0x"
			if len(input.Params) == 2 && strings.EqualFold(string(input.Params[0]), fmt.Sprintf("%q", baseFeeWallet)) {
				result = "0xef010063c0c19a282a1b52b07dd5a65b58948a07dae32b"
			}
		case "eth_feeHistory":
			result = map[string]any{"oldestBlock": "0x123", "baseFeePerGas": []string{"0x4c4b40", "0x4c4b40"}, "reward": [][]string{{"0xf4240"}}}
		case "eth_call":
			result = fmt.Sprintf("0x%064x", 1234567)
		case "getGenesisHash":
			result = "EtWTRABZaYq6iMfeYKouRu166VU2xqa1wcaWoxPkrZBG"
		case "getLatestBlockhash":
			result = map[string]any{"value": chainassets.SolanaBlock{Blockhash: "F1VvomZpGxaB9taEwSuT1WwAaqP99RYjQXoEkodmvxhA", LastValidBlockHeight: 123}}
		case "getMultipleAccounts":
			var addresses []string
			if len(input.Params) != 2 || json.Unmarshal(input.Params[0], &addresses) != nil || len(addresses) != 2 {
				t.Error("invalid getMultipleAccounts parameters")
				return
			}
			switch addresses[0] {
			case usdcFixture.SourceATA:
				result = usdcFixture.AccountSnapshot
			case usdtFixture.SourceATA:
				result = usdtFixture.AccountSnapshot
			default:
				t.Errorf("unexpected source ATA %q", addresses[0])
				return
			}
		case "getMinimumBalanceForRentExemption":
			result = usdcFixture.RentLamports
		case "getFeeForMessage":
			result = map[string]any{"context": map[string]int{"slot": 123}, "value": 10123}
		case "eth_getBalance":
			if len(input.Params) == 2 && string(input.Params[1]) != `"latest"` {
				result = "0x1000000000000"
			}
		default:
			t.Error(input.Method)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": result})
	}))
	defer upstream.Close()
	s.DepositConfirmations = deposit.NewConfirmations(sqlx.NewSqlConnFromDB(s.DB), s.Redis, s.Namespace)
	const depositPath = "/v1/user/deposit-quotes/preview"
	deposit := types.DepositQuoteRequest{AssetId: chainassets.BaseSepoliaETH, TargetUsd: "10"}
	call("POST", depositPath, "", deposit, 401, "UNAUTHENTICATED")
	call("POST", depositPath, token, deposit, 503, "SERVICE_UNAVAILABLE")
	now := time.Now().Unix()
	rates := &chainassets.DepositQuoteConfig{Version: "http-fixture", AsOf: now - 30, ValidUntil: now + 120, Assets: []chainassets.DepositQuoteRate{{AssetID: deposit.AssetId, USDPerCoin: "2000", EstimatedGas: "0.0001"}}}
	baseUSDC := types.DepositQuoteRequest{AssetId: "base-sepolia:usdc", TargetUsd: "0.01", WalletAddress: baseFeeWallet}
	rates.BaseUSDC = &chainassets.BaseDepositFeeConfig{FeePayer: baseFeePayer, Recipient: baseFeePayer}
	rates.Assets = append(rates.Assets, chainassets.DepositQuoteRate{AssetID: baseUSDC.AssetId, USDPerCoin: "0.98", EstimatedGas: "0.00001"})
	const wallet = "9ucco3Ath4uS71gyfHggdo9jXzUnhkRr5oPHp8qVUVGf"
	sol := types.DepositQuoteRequest{AssetId: chainassets.SolanaDevnet + ":sol", TargetUsd: "1", WalletAddress: wallet}
	rates.Assets = append(rates.Assets, chainassets.DepositQuoteRate{AssetID: sol.AssetId, USDPerCoin: "100", EstimatedGas: "0"})
	rates.SolanaSOL = &chainassets.SolanaDepositFeeConfig{FeePayer: "DcfKLiLsSsoJrrf3t6QXwkjfUarE4yW5sKjzF7xBQkPN", Recipient: "7Njq3pBVPhZWEoRh8w22eC2KBcGUhhCvwFFitsyTg7A5"}
	usdc := types.DepositQuoteRequest{AssetId: chainassets.SolanaDevnet + ":usdc", TargetUsd: "0.01", WalletAddress: wallet}
	rates.Assets = append(rates.Assets, chainassets.DepositQuoteRate{AssetID: usdc.AssetId, USDPerCoin: "1", EstimatedGas: "0"})
	rates.SolanaUSDC = &chainassets.SolanaDepositFeeConfig{FeePayer: rates.SolanaSOL.FeePayer, Recipient: usdcFixture.Addresses.Recipient}
	usdt := types.DepositQuoteRequest{AssetId: chainassets.SolanaDevnet + ":usdt:" + usdtFixture.Mint, TargetUsd: "0.01", WalletAddress: wallet}
	rates.Assets = append(rates.Assets, chainassets.DepositQuoteRate{AssetID: usdt.AssetId, USDPerCoin: "0.98", EstimatedGas: "0"})
	rates.SolanaTestUSDT = &chainassets.SolanaDepositFeeConfig{FeePayer: rates.SolanaSOL.FeePayer, Recipient: usdtFixture.Addresses.Recipient}
	service, err := chainassets.New(chainassets.Config{DepositQuotes: rates, Networks: []chainassets.Endpoint{{Network: chainassets.BaseSepolia, URL: upstream.URL}, {Network: chainassets.SolanaDevnet, URL: upstream.URL, TestUSDTContract: usdtFixture.Mint}}}, chainassets.NewRedisLimiter(s.Redis, s.Namespace, 1))
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
	listed := map[string]bool{}
	for _, n := range catalog.Networks {
		for _, a := range n.Assets {
			if a.DepositPreview {
				if a.Id != deposit.AssetId && a.Id != sol.AssetId && a.Id != usdc.AssetId && a.Id != usdt.AssetId && a.Id != baseUSDC.AssetId {
					t.Fatal(a.Id)
				}
				listed[a.Id] = true
			}
		}
	}
	if !listed[deposit.AssetId] || !listed[sol.AssetId] || !listed[usdc.AssetId] || !listed[usdt.AssetId] || !listed[baseUSDC.AssetId] {
		t.Fatal("missing quoted asset")
	}
	_, raw = call("POST", depositPath, token, deposit, 200, "OK")
	var quote types.DepositQuoteData
	if err := json.Unmarshal(raw, &quote); err != nil || quote.QuoteIdx != "" || quote.MaxGas != "" || quote.AssetId != deposit.AssetId || quote.PayQuantity != "0.0051" || quote.NetUsd != "10.00" || quote.GasUsd != "0.2" || quote.CanSubmit || quote.RateVersion != rates.Version {
		t.Fatal(quote, err)
	}
	deposit.TargetUsd = "0.001"
	call("POST", depositPath, token, deposit, 400, "INVALID_ARGUMENT")
	invalid := request
	invalid.Address = "bad-address"
	call("POST", "/v1/user/chain-assets/balances", token, invalid, 400, "INVALID_ARGUMENT")
	call("POST", depositPath, "", sol, 401, "UNAUTHENTICATED")
	_, raw = call("POST", depositPath, token, sol, 200, "OK")
	var walletQuote types.DepositQuoteData
	if err := json.Unmarshal(raw, &walletQuote); err != nil || !walletQuote.WalletFeePreview || walletQuote.WalletAddress != wallet || len(walletQuote.MessageSha256) != 64 || walletQuote.PayQuantity != "0.010010123" || walletQuote.CanSubmit {
		t.Fatal(walletQuote, err)
	}
	_, raw = call("POST", depositPath, token, usdc, 200, "OK")
	var usdcQuote types.DepositQuoteData
	if err := json.Unmarshal(raw, &usdcQuote); err != nil || usdcQuote.CanSubmit || usdcQuote.PayQuantity != "0.011013" || usdcQuote.AccountFunding != "0.00148844" || usdcQuote.AccountFundingUsd != "0.148844" || usdcQuote.NetUsd != "0.01" {
		t.Fatal(usdcQuote, err)
	}
	_, raw = call("POST", depositPath, token, usdt, 200, "OK")
	var usdtQuote types.DepositQuoteData
	if err := json.Unmarshal(raw, &usdtQuote); err != nil || usdtQuote.AssetId != usdt.AssetId || usdtQuote.CanSubmit || usdtQuote.PayQuantity != "0.011238" || usdtQuote.AccountFunding != "0" || usdtQuote.AccountFundingUsd != "0" || usdtQuote.NetUsd != "0.01" {
		t.Fatal(usdtQuote, err)
	}
	_, raw = call("POST", depositPath, token, baseUSDC, 200, "OK")
	var baseQuote types.DepositQuoteData
	if err := json.Unmarshal(raw, &baseQuote); err != nil || baseQuote.FeeBlockHash != baseFeeHash || baseQuote.MessageSha256 != "" || baseQuote.WalletAddress != baseUSDC.WalletAddress || baseQuote.CanSubmit || baseQuote.NetUsd != "0.01" || baseQuote.PayQuantity != "0.011745" {
		t.Fatal(baseQuote, err)
	}
	_, raw = call("POST", "/v1/user/chain-assets/balances", token, request, 200, "OK")
	var result types.ChainBalancesData
	if err = json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	if result.Items[0].Quantity != "0" || result.Items[1].Quantity != "1.234567" {
		t.Fatal(result)
	}
	call("POST", "/v1/user/chain-assets/balances", token, request, 429, "RATE_LIMITED")
	call("POST", depositPath, token, sol, 429, "RATE_LIMITED")
	checkMarketRatesHTTP(t, s, token, upstream.URL, call)
	checkDepositConfirmationHTTP(t, s, token, walletQuote, call)
}
