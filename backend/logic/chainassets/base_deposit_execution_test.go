package chainassets

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/internal/baseusdc"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

// A signed order keeps the original wire across retries. Rising L1 fees or a
// reverted simulation must prevent submission even when the approval fit earlier.
func TestBaseUSDCBroadcastPreflight(t *testing.T) {
	wallet, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	sponsor, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now()
	q := DepositPreview{QuoteIdx: strings.Repeat("a", 32), WalletFeePreview: true, WalletAddress: crypto.PubkeyToAddress(wallet.PublicKey).Hex(), FeePayer: crypto.PubkeyToAddress(sponsor.PublicKey).Hex(), Recipient: depositFeePayer, AssetID: baseusdc.AssetID, GasAssetID: BaseSepoliaETH, PayQuantity: "0.01", EstimatedGas: "0.000000001", MaxGas: "0.000000001", ExpiresAt: at.Add(30 * time.Second).UnixMilli(), BaseValidBefore: at.Add(2 * time.Minute).Unix()}
	a, err := q.BaseAuthorization()
	if err != nil {
		t.Fatal(err)
	}
	sig, err := crypto.Sign(a.Digest().Bytes(), wallet)
	if err != nil {
		t.Fatal(err)
	}
	sig[64] += 27
	for _, tc := range []string{"budget fits", "higher L1 fee", "simulation rejected"} {
		t.Run(tc, func(t *testing.T) {
			sent := make(chan string, 1)
			node := service(t, BaseSepolia, func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					Method string
					Params []any
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Error(err)
					return
				}
				switch req.Method {
				case "eth_chainId":
					rpcResult(w, "0x14a34")
				case "eth_getBlockByNumber":
					rpcResult(w, map[string]any{"hash": "0x" + strings.Repeat("1", 64), "number": "0x1", "timestamp": fmt.Sprintf("0x%x", time.Now().Unix()), "baseFeePerGas": "0x64"})
				case "eth_call":
					call := req.Params[0].(map[string]any)
					if call["to"] == BaseGasPriceOracle {
						v := 0
						if tc == "higher L1 fee" && call["data"] == baseOracleCall("getL1FeeUpperBound(uint256)", big.NewInt(baseUSDCUnsignedSize)) {
							v = 1100000000
						}
						rpcResult(w, fmt.Sprintf("0x%064x", v))
						return
					}
					if tc == "simulation rejected" {
						fmt.Fprint(w, `{"jsonrpc":"2.0","id":1,"error":{"code":3,"message":"execution reverted"}}`)
						return
					}
					rpcResult(w, "0x")
				case "eth_sendRawTransaction":
					sent <- req.Params[0].(string)
					rpcResult(w, crypto.Keccak256Hash(hexutil.MustDecode(req.Params[0].(string))).Hex())
				default:
					t.Error("unexpected RPC", req.Method)
				}
			})
			data, err := a.Data(hexutil.Encode(sig))
			if err != nil {
				t.Fatal(err)
			}
			to := common.HexToAddress(baseusdc.SepoliaContract)
			unsigned := types.NewTx(&types.DynamicFeeTx{ChainID: big.NewInt(BaseSepoliaChainID), To: &to, Gas: 100000, GasFeeCap: big.NewInt(200), GasTipCap: big.NewInt(10), Data: data})
			tx, err := types.SignTx(unsigned, types.LatestSignerForChainID(unsigned.ChainId()), sponsor)
			if err != nil {
				t.Fatal(err)
			}
			err = node.BroadcastBaseDeposit(context.Background(), tx, q, hexutil.Encode(sig))
			if tc != "budget fits" {
				if err == nil || len(sent) != 0 {
					t.Fatal("submitted invalid call", err)
				}
				return
			}
			wire, _ := tx.MarshalBinary()
			if err != nil || len(sent) != 1 || <-sent != hexutil.Encode(wire) {
				t.Fatal("original wire", err)
			}
		})
	}
}
