package chainassets

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/apperror"
	"github.com/0xabcd-k/DexWallet/backend/internal/tronnative"
)

const nileResourceOwner = "TPDBmTxJWYucGk5VjDsSi6jBdvVjpCnCsH"

func TestNileBandwidthReceipt(t *testing.T) {
	data, err := os.ReadFile("../chainreceipt/testdata/tron-trx.json")
	if err != nil {
		t.Fatal(err)
	}
	var evidence struct {
		Evidence struct {
			Transaction struct {
				Raw  string `json:"raw_data_hex"`
				TxID string
			}
			Info struct {
				Receipt struct {
					Used int `json:"net_usage"`
				}
			}
		}
	}
	if err := json.Unmarshal(data, &evidence); err != nil {
		t.Fatal(err)
	}
	tx := evidence.Evidence.Transaction
	raw, err := hex.DecodeString(tx.Raw)
	if err != nil || tronnative.TxID(raw) != tx.TxID || tronnative.SingleSignatureBandwidth(raw) != evidence.Evidence.Info.Receipt.Used {
		t.Fatal(tx.TxID, err)
	}
}

type nileCaseChange struct {
	path, key string
	value     any
}
type nileQuoteCase struct {
	name   string
	change nileCaseChange
	want   apperror.Code
}

func TestNileTRXDepositPreview(t *testing.T) {
	type change = nileCaseChange
	const source = "source"
	const grants = "/wallet/getdelegatedresourcev2"
	const resource = "/wallet/getaccountresource"
	cases := []nileQuoteCase{
		{name: "covered"},
		{name: "exact capacity", change: change{resource, "NetLimit", 266}},
		{name: "free bandwidth cannot cover a missing delegated point", change: change{resource, "NetLimit", 265}, want: apperror.ServiceUnavailable},
		{name: "used quota reduces capacity", change: change{resource, "NetUsed", 400}, want: apperror.ServiceUnavailable},
		{name: "insufficient principal", change: change{source, "balance", 999}, want: apperror.InsufficientBalance},
		{name: "recipient not active", change: change{"recipient", "address", ""}, want: apperror.ServiceUnavailable},
		{name: "source signatory differs", change: change{source, "owner_permission", map[string]any{"threshold": 1, "keys": []any{map[string]any{"address": "41913e6da20b97589625ae499b6eb17941e5938405", "weight": 1}}}}, want: apperror.InvalidArgument},
		{name: "own stake", change: change{source, "frozenV2", []any{map[string]any{"amount": 1000000}}}, want: apperror.ServiceUnavailable},
		{name: "old bandwidth grant", change: change{source, "acquired_delegated_frozen_balance_for_bandwidth", 1000000}, want: apperror.ServiceUnavailable},
		{name: "mixed incoming stake", change: change{source, "acquired_delegated_frozenV2_balance_for_bandwidth", 856000001}, want: apperror.ServiceUnavailable},
		{name: "no matching grant", change: change{grants, "delegatedResource", []any{}}, want: apperror.ServiceUnavailable},
		{name: "wrong Nile genesis", change: change{"/wallet/getblockbynum", "blockID", TRONMainnetGenesis}, want: apperror.ServiceUnavailable},
		{name: "expired reference", change: change{"/wallet/getnowblock", "timestamp", int64(1)}, want: apperror.ServiceUnavailable},
	}
	testNileQuotes(t, "testdata/tron-trx-deposit-bandwidth.json", TRONNileTRX, cases)
}

func TestNileUSDTDepositRequiresVerifiedContract(t *testing.T) {
	const officialNileUSDT = "TXYZopYRdj2D9XRtbG411XZZ3kM5VkAeBf"
	id := NileUSDT(officialNileUSDT).ID
	cfg := Config{
		Networks: []Endpoint{{Network: TRONNile, URL: "https://nile.trongrid.io", TestUSDTContract: officialNileUSDT}},
		DepositQuotes: &DepositQuoteConfig{
			Version: "test", AsOf: 1, ValidUntil: 100,
			Assets: []DepositQuoteRate{{id, "1", "0"}, {TRONNileTRX, "0.30", "0"}},
		},
	}
	s, err := New(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	q, err := s.PreviewDeposit(id, "0.01", time.Unix(50, 0))
	if err != nil || q.WalletFeePreview {
		t.Fatalf("ordinary quote for a different contract: %+v, %v", q, err)
	}
	cfg.DepositQuotes.TRONTestUSDT = &TRONDepositFeeConfig{ResourceOwner: nileResourceOwner, Recipient: nileResourceOwner}
	if other, err := New(cfg, nil); err == nil {
		other.Close()
		t.Fatal("platform resource estimate enabled on the official Nile contract")
	}
}

func TestNileUSDTDepositPreview(t *testing.T) {
	const simulation = "transfer"
	const account = "/wallet/getaccountresource"
	const unavailable = apperror.ServiceUnavailable
	type change = nileCaseChange
	cases := []nileQuoteCase{
		{name: "observed payment"},
		{name: "zero TRX still covers token payment", change: change{"source", "balance", 0}},
		{name: "exact Energy", change: change{account, "EnergyLimit", 14650}},
		{name: "one Energy short", change: change{account, "EnergyLimit", 14649}, want: unavailable},
		{name: "consumed Energy", change: change{account, "EnergyUsed", 16738}, want: unavailable},
		{name: "bandwidth short", change: change{account, "NetLimit", 344}, want: unavailable},
		{name: "token units short", change: change{"balance", "constant_result", []string{strings.Repeat("0", 60) + "270f"}}, want: apperror.InsufficientBalance},
		{name: "other Energy supplier", change: change{"source", "account_resource", map[string]any{"acquired_delegated_frozenV2_balance_for_energy": 228000001}}, want: unavailable},
		{name: "own Energy stake", change: change{"source", "frozenV2", []any{map[string]any{"type": "ENERGY", "amount": 1000000}}}, want: unavailable},
		{name: "contract deployer pays Energy", change: change{"contract", "consume_user_resource_percent", 0}, want: unavailable},
		{name: "other contract runtime", change: change{"/wallet/getcontractinfo", "runtimecode", "00"}, want: unavailable},
		{name: "false boolean without transfer", change: change{simulation, "logs", []any{}}, want: unavailable},
		{name: "incorrect transferred amount", change: change{"transferLog", "data", strings.Repeat("0", 64)}, want: unavailable},
		{name: "other payee", change: change{"transferLog", "topics", []string{trc20TransferTopic, strings.Repeat("0", 64), strings.Repeat("0", 64)}}, want: unavailable},
		{name: "failed VM status", change: change{"transaction", "ret", []any{map[string]any{"ret": "FAILED"}}}, want: unavailable},
		{name: "no energy estimate", change: change{simulation, "energy_used", 0}, want: unavailable},
		{name: "fee limit too small", change: change{"/wallet/getchainparameters", "chainParameter", []any{map[string]any{"key": "getEnergyFee", "value": 2000}, map[string]any{"key": "getMaxFeeLimit", "value": 15000000000}}}, want: unavailable},
	}
	testNileQuotes(t, "testdata/tron-usdt-deposit-resources.json", nileDepositUSDTID, cases)
}

func testNileQuotes(t *testing.T, path, assetID string, cases []nileQuoteCase) {
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var fixture struct {
				Calls []struct {
					Path           string
					Params, Result map[string]any
				}
				Preview DepositPreview
			}
			if err := json.Unmarshal(data, &fixture); err != nil {
				t.Fatal(err)
			}
			now := time.Now()
			var head tronnative.Head
			s := service(t, TRONNile, func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
					return
				}
				for _, c := range fixture.Calls {
					if c.Path != r.URL.Path || !reflect.DeepEqual(c.Params, body) {
						continue
					}
					scope := c.Path
					changed := c.Result
					switch scope {
					case "/wallet/getaccount":
						scope = "recipient"
						if body["address"] == "412461517cf642358c8c0650b2cbccee4c31e16d7d" {
							scope = "source"
						}
					case "/wallet/triggerconstantcontract":
						scope = "balance"
						if strings.HasPrefix(body["data"].(string), "a9059cbb") {
							scope = "transfer"
							switch tc.change.path {
							case "transferLog":
								scope = "transferLog"
								changed = c.Result["logs"].([]any)[0].(map[string]any)
							case "transaction":
								scope = "transaction"
								changed = c.Result["transaction"].(map[string]any)
							}
						}
					case "/wallet/getcontractinfo":
						if tc.change.path == "contract" {
							scope = "contract"
							changed = c.Result["smart_contract"].(map[string]any)
						}
					case "/wallet/getnowblock":
						header := c.Result["block_header"].(map[string]any)["raw_data"].(map[string]any)
						head = tronnative.Head{ID: c.Result["blockID"].(string), Height: int64(header["number"].(float64)), Timestamp: now.Add(-time.Second).UnixMilli()}
						header["timestamp"] = head.Timestamp
						if tc.change.path == scope {
							header["timestamp"] = tc.change.value
						}
					}
					if tc.change.path == scope && scope != "/wallet/getnowblock" {
						changed[tc.change.key] = tc.change.value
					}
					_ = json.NewEncoder(w).Encode(c.Result)
					return
				}
				t.Errorf("unexpected RPC %s %v", r.URL.Path, body)
			}, func(c *Config) {
				c.DepositQuotes = &DepositQuoteConfig{Version: "trx-test", AsOf: now.Unix() - 30, ValidUntil: now.Unix() + 120, TRONTRX: &TRONDepositFeeConfig{nileResourceOwner, nileResourceOwner}, Assets: []DepositQuoteRate{{TRONNileTRX, "10", "0.1"}}}
				if assetID == nileDepositUSDTID {
					c.Networks[0].TestUSDTContract = nileDepositTestUSDT
					c.DepositQuotes.TRONTestUSDT = c.DepositQuotes.TRONTRX
					c.DepositQuotes.TRONTRX = nil
					c.DepositQuotes.Assets = append(c.DepositQuotes.Assets, DepositQuoteRate{assetID, "1", "0.1"})
				}
			})
			q, err := s.PreviewWalletDeposit(context.Background(), 7, assetID, "0.01", tronOwner)
			if tc.want != "" {
				var cause *apperror.Error
				if !errors.As(err, &cause) || cause.Code != tc.want || q.PayQuantity != "" {
					t.Fatal(q, err)
				}
				return
			}
			tx := tronnative.Transfer{Sender: tronOwner, Recipient: nileResourceOwner, Sun: 1000, Reference: head, Expiration: head.Timestamp + tronnative.Lifetime.Milliseconds()}
			if assetID == nileDepositUSDTID {
				tx.Sun = 0
				tx.TokenContract = nileDepositTestUSDT
				tx.TokenAtoms = "10000"
				tx.FeeLimitSun = nileUSDTFeeLimitSun
			}
			raw, buildErr := tx.Raw()
			if err != nil || buildErr != nil || q.MessageSHA256 != tronnative.TxID(raw) || q.DelegatedBandwidth != fixture.Preview.DelegatedBandwidth || q.WalletAddress != tronOwner || q.PayQuantity != fixture.Preview.PayQuantity || q.DelegatedEnergy != fixture.Preview.DelegatedEnergy || q.EstimatedGas != "0" || q.GasUSD != "0" || q.NetUSD != "0.01" || q.ExpiresAt != head.Timestamp+depositPreviewTTL.Milliseconds() {
				t.Fatal(q, err, buildErr)
			}
		})
	}
}
