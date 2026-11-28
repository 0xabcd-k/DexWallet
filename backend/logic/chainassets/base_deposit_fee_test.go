package chainassets

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/apperror"
	"github.com/0xabcd-k/DexWallet/backend/internal/baseusdc"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
)

const depositFeePayer = "0xa59C992Aa84657df4EbFF6F04E11a1632f6DB1FA"

func TestBaseDepositUnsignedSizeBudget(t *testing.T) {
	// The native redemption is calibrated against the actual platform transaction.
	record, err := os.ReadFile("../chainreceipt/testdata/base-internal-eth.json")
	if err != nil {
		t.Fatal(err)
	}
	var captured struct {
		Evidence struct{ Transaction types.Transaction }
	}
	if err := json.Unmarshal(record, &captured); err != nil {
		t.Fatal(err)
	}
	tx := &captured.Evidence.Transaction
	if tx.Type() != types.DynamicFeeTxType || tx.Gas() != baseETHReferenceGas || tx.Value().Sign() != 0 || len(tx.AccessList()) != 0 {
		t.Fatal("unexpected ETH redemption transaction", tx.Hash())
	}
	maxFee := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	for _, route := range []struct {
		data []byte
		size int
	}{
		{bytes.Repeat([]byte{0xff}, 4+9*32), baseUSDCUnsignedSize},
		{tx.Data(), baseETHUnsignedSize},
	} {
		fields := []any{uint64(BaseSepoliaChainID), uint64(math.MaxUint64), maxFee, maxFee, uint64(math.MaxUint64),
			common.Address{}, uint64(0), route.data, []common.Address{}}
		encoded, err := rlp.EncodeToBytes(fields)
		if err != nil || len(encoded)+1 != route.size {
			t.Fatal(len(encoded), route.size, err)
		}
	}
}

type baseFeeCase struct {
	name, method, prefix, address string
	override                      any
	want                          apperror.Code
}

func TestBaseDepositWalletBudget(t *testing.T) {
	for _, asset := range []struct{ id, name string }{{BaseSepoliaETH, "eth"}, {baseusdc.AssetID, "usdc"}} {
		t.Run(asset.name, func(t *testing.T) {
			checkBaseDepositBudget(t, asset.id, "testdata/base-"+asset.name+"-deposit-budget.json")
		})
	}
}

func checkBaseDepositBudget(t *testing.T, assetID, fixture string) {
	data, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	var evidence struct {
		Calls []struct {
			Method string
			Params []any
			Result any
		}
		Preview     DepositPreview
		RequiredWei string
	}
	if err = json.Unmarshal(data, &evidence); err != nil {
		t.Fatal(err)
	}
	cases := []baseFeeCase{
		{name: "observed budget"},
		{name: "authorization enabled"},
		{name: "other network", method: "eth_chainId", override: "0x2105", want: apperror.ServiceUnavailable},
		{name: "unsupported wallet", method: "eth_getCode", address: evidence.Preview.WalletAddress, override: "0x6000", want: apperror.InvalidArgument},
		{name: "old block", method: "eth_getBlockByNumber", want: apperror.ServiceUnavailable},
		{name: "incomplete history", method: "eth_feeHistory", override: map[string]any{}, want: apperror.ServiceUnavailable},
		{name: "missing L1 oracle", method: "eth_call", prefix: "0xf1c7a58b", override: "0x", want: apperror.ServiceUnavailable},
		{name: "missing operator", method: "eth_call", prefix: "0x275aedd2", want: apperror.ServiceUnavailable},
		{name: "no sponsor ETH", method: "eth_getBalance", address: depositFeePayer, override: "0x0", want: apperror.ServiceUnavailable},
	}
	if assetID == BaseSepoliaETH {
		gross, ok := new(big.Int).SetString(evidence.RequiredWei, 10)
		if !ok {
			t.Fatal("missing independently calculated ETH requirement")
		}
		cases = append(cases,
			baseFeeCase{name: "plain EOA needs account setup", method: "eth_getCode", address: evidence.Preview.WalletAddress, override: "0x", want: apperror.InvalidArgument},
			baseFeeCase{name: "platform payee has code", method: "eth_getCode", address: depositFeePayer, override: "0x6000", want: apperror.ServiceUnavailable},
			baseFeeCase{name: "exact ETH after fee", method: "eth_getBalance", address: evidence.Preview.WalletAddress, override: fmt.Sprintf("0x%x", gross)},
			baseFeeCase{name: "one wei short", method: "eth_getBalance", address: evidence.Preview.WalletAddress, override: fmt.Sprintf("0x%x", new(big.Int).Sub(gross, big.NewInt(1))), want: apperror.InsufficientBalance},
		)
	} else {
		cases = append(cases,

			baseFeeCase{name: "plain EOA", method: "eth_getCode", override: "0x"},
			baseFeeCase{name: "nonzero operator fee", method: "eth_call", prefix: "0x275aedd2", override: fmt.Sprintf("0x%064x", 1000)},
			baseFeeCase{name: "insufficient USDC after fee", method: "eth_call", prefix: "0x70a08231", override: fmt.Sprintf("0x%064x", 11000), want: apperror.InsufficientBalance},
		)
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := service(t, BaseSepolia, func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					Method string
					Params []any
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Error(err)
					return
				}
				for _, observed := range evidence.Calls {
					if observed.Method != req.Method || !reflect.DeepEqual(observed.Params, req.Params) {
						continue
					}
					result := observed.Result
					if req.Method == "eth_getBlockByNumber" {
						h := result.(map[string]any)
						// Rebase only time during offline replay; keep the captured block and fees.
						stamp := time.Now().Add(-5 * time.Second).Unix()
						if tc.method == req.Method {
							stamp -= int64(depositPreviewTTL / time.Second)
						}
						result = map[string]any{"hash": h["hash"], "number": h["number"], "baseFeePerGas": h["baseFeePerGas"], "timestamp": fmt.Sprintf("0x%x", stamp)}
					} else if tc.method == req.Method && (tc.address == "" || strings.EqualFold(req.Params[0].(string), tc.address)) && (tc.prefix == "" || strings.HasPrefix(req.Params[0].(map[string]any)["data"].(string), tc.prefix)) {
						result = tc.override
					}
					rpcResult(w, result)
					return
				}
				t.Errorf("unexpected RPC or parameters: %s %v", req.Method, req.Params)
			}, func(c *Config) {
				now := time.Now().Unix()
				c.DepositQuotes = &DepositQuoteConfig{Version: "base-budget", AsOf: now - 60, ValidUntil: now + 60,
					Assets: []DepositQuoteRate{{BaseSepoliaETH, "2000", "0.00001"}, {baseusdc.AssetID, "0.98", "0.00001"}}}
				route := &BaseDepositFeeConfig{depositFeePayer, depositFeePayer}
				if assetID == BaseSepoliaETH {
					c.DepositQuotes.BaseETH = route
					if tc.name == "authorization enabled" {
						c.DepositQuotes.BaseETHExecutionWindow = 5 * time.Minute
					}
				} else {
					c.DepositQuotes.BaseUSDC = route
					if tc.name == "authorization enabled" {
						c.DepositQuotes.BaseUSDCExecutionWindow = 5 * time.Minute
					}
				}
			})
			q, err := s.PreviewWalletDeposit(context.Background(), 7, assetID, "0.01", evidence.Preview.WalletAddress)
			if tc.want != "" {
				var cause *apperror.Error
				if !errors.As(err, &cause) || cause.Code != tc.want || q.PayQuantity != "" {
					t.Fatal(q, err)
				}
				return
			}
			var wantExpiry int64
			if tc.name == "authorization enabled" {
				wantExpiry = time.UnixMilli(q.ExpiresAt).Add(5 * time.Minute).Unix()
			}
			if q.BaseValidBefore != wantExpiry {
				t.Fatalf("authorization expiry %d, want %d", q.BaseValidBefore, wantExpiry)
			}
			want := evidence.Preview
			if tc.name == "nonzero operator fee" {
				want.EstimatedGas = "0.000000754938001356"
				want.GasUSD = "0.001509876002712"
			}
			if err != nil || q.EstimatedGas != want.EstimatedGas || q.GasUSD != want.GasUSD || q.PayQuantity != want.PayQuantity || q.NetUSD != "0.01" || q.WalletAddress != want.WalletAddress || q.FeeBlockHash != want.FeeBlockHash || q.MessageSHA256 != "" || !q.WalletFeePreview || q.ExpiresAt >= q.QuotedAt+depositPreviewTTL.Milliseconds() {
				t.Fatal(q, err)
			}
		})
	}
}
