//go:build integration

package integration

import (
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"errors"
	"math/big"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/internal/baseusdc"
	"github.com/0xabcd-k/DexWallet/backend/internal/testenv"
	"github.com/0xabcd-k/DexWallet/backend/logic/account"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
	"github.com/0xabcd-k/DexWallet/backend/logic/deposit"
	"github.com/0xabcd-k/DexWallet/backend/logic/user"
	accounts "github.com/0xabcd-k/DexWallet/backend/model/account"
	"github.com/0xabcd-k/DexWallet/backend/transaction"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

type lostBaseDepositAck struct {
	*chainassets.Service
	sent int
}

var errLostBaseDepositAck = errors.New("lost Base broadcast response")

func (n *lostBaseDepositAck) BroadcastBaseDeposit(ctx context.Context, tx *types.Transaction, q chainassets.DepositPreview, sig string) error {
	if err := n.Service.BroadcastBaseDeposit(ctx, tx, q, sig); err != nil {
		return err
	}
	n.sent++
	return errLostBaseDepositAck
}

func TestBaseSepoliaUSDCDepositLive(t *testing.T) {
	if os.Getenv("DEXWALLET_TEST_BASE_DEPOSIT_EXECUTE") != "1" {
		t.Skip("explicit dedicated Base Sepolia keys and one test transfer required")
	}
	runBaseDepositLive(t, baseusdc.AssetID)
}

func TestBaseSepoliaETHDepositLive(t *testing.T) {
	if os.Getenv("DEXWALLET_TEST_BASE_ETH_DEPOSIT_EXECUTE") != "1" {
		t.Skip("explicit Base Sepolia ETH deposit required")
	}
	runBaseDepositLive(t, chainassets.BaseSepoliaETH)
}

func runBaseDepositLive(t *testing.T, asset string) {
	raw, err := os.ReadFile(os.Getenv("DEXWALLET_TEST_BASE_DEPOSIT_KEY_FILE"))
	if err != nil {
		t.Fatal(err)
	}
	defer clear(raw)
	var keys map[string]string
	if err = json.Unmarshal(raw, &keys); err != nil {
		t.Fatal(err)
	}
	read := func(role, addr string) *ecdsa.PrivateKey {
		k, e := crypto.HexToECDSA(strings.TrimPrefix(keys[role], "0x"))
		if e != nil || !strings.EqualFold(crypto.PubkeyToAddress(k.PublicKey).Hex(), addr) {
			t.Fatal("wrong dedicated test key", role)
		}
		return k
	}
	const wallet = "0xecd14F7586f5053CECae148Ae26309C2277D86A8"
	const payer = "0xa59C992Aa84657df4EbFF6F04E11a1632f6DB1FA"
	userKey, sponsor := read("user", wallet), read("sponsor", payer)
	db, users := userDatabase(t, "base_deposit_live")
	mustExec(t, db, "INSERT INTO user(id,nickname) VALUES(1,'base-dev')")
	client := redis.NewClient(&redis.Options{Addr: testenv.RedisAddress, Password: os.Getenv("DEXWALLET_REDIS_PASSWORD")})
	defer client.Close()
	ns := testRun.Name("redis", "base_deposit_live")
	rpc := os.Getenv("DEXWALLET_TEST_BASE_DEPOSIT_RPC")
	if rpc == "" {
		rpc = "https://sepolia.base.org"
	}
	now := time.Now().Unix()
	cfg := chainassets.DepositQuoteConfig{Version: "base-test", AsOf: now - 10, ValidUntil: now + 3600, BaseUSDCExecutionWindow: 5 * time.Minute, BaseUSDC: &chainassets.BaseDepositFeeConfig{Recipient: payer, FeePayer: payer}, Assets: []chainassets.DepositQuoteRate{{AssetID: baseusdc.AssetID, USDPerCoin: "0.98", EstimatedGas: "0"}, {AssetID: chainassets.BaseSepoliaETH, USDPerCoin: "2000", EstimatedGas: "0"}}}
	if asset == chainassets.BaseSepoliaETH {
		cfg.BaseETH = cfg.BaseUSDC
		cfg.BaseETHExecutionWindow = 5 * time.Minute
	}
	chain, err := chainassets.New(chainassets.Config{Timeout: 25 * time.Second, Networks: []chainassets.Endpoint{{Network: chainassets.BaseSepolia, URL: rpc}}, DepositQuotes: &cfg}, chainassets.NewRedisLimiter(client, ns, 30))
	if err != nil {
		t.Fatal(err)
	}
	defer chain.Close()
	ctx := context.Background()
	conn := sqlx.NewSqlConnFromDB(db)
	runner := transaction.New(db)
	quotes := deposit.NewConfirmations(conn, client, ns)
	orders := deposit.NewBaseOrders(conn, quotes, users, runner, chain)
	actor := user.Actor{UserID: 1, AuthVersion: 1}
	q, err := chain.PreviewWalletDeposit(ctx, 1, asset, "0.01", wallet)
	if err != nil {
		t.Fatal(err)
	}
	q, err = quotes.Offer(ctx, 1, q)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = quotes.Confirm(ctx, 1, q.QuoteIdx); err != nil {
		t.Fatal(err)
	}
	auth, err := q.BaseAuthorization()
	if err != nil {
		t.Fatal(err)
	}
	maxGross := int64(120000)
	if asset == chainassets.BaseSepoliaETH {
		maxGross = 12000000000000
	}
	cap, err := q.BaseFeeLimitWei()
	if err != nil || cap.Cmp(big.NewInt(50000000000000)) > 0 || auth.Amount.Cmp(big.NewInt(maxGross)) > 0 {
		t.Fatal("outside dedicated test budget")
	}
	sig, err := crypto.Sign(auth.Digest().Bytes(), userKey)
	if err != nil {
		t.Fatal(err)
	}
	sig[64] += 27
	o, err := orders.Authorize(ctx, actor, q.QuoteIdx, hexutil.Encode(sig))
	if err != nil || o.State != deposit.Accepted {
		t.Fatal("admit", err)
	}
	ledger := account.New(accounts.NewUserAccountModel(conn))
	node := &lostBaseDepositAck{Service: chain}
	w := deposit.NewBaseExecution(conn, runner, ledger, node)
	budget := deposit.BaseBudget{Recipient: payer, USDCAtoms: auth.Amount, MaxFeeWei: cap}
	if asset == chainassets.BaseSepoliaETH {
		budget.ETHWei = auth.Amount
		budget.USDCAtoms = nil
	}
	o, err = w.Step(ctx, q.QuoteIdx, budget, sponsor)
	if err != nil || !o.TxHash.Valid || node.sent != 0 {
		t.Fatal("persist before broadcast", err)
	}
	id := o.TxHash.String
	t.Logf("Base deposit order=%s tx=%s gross=%s fee_cap_wei=%s", q.QuoteIdx, id, q.PayQuantity, cap)
	_, err = w.Step(ctx, q.QuoteIdx, deposit.BaseBudget{}, nil)
	if !errors.Is(err, errLostBaseDepositAck) {
		t.Fatal("broadcast", err)
	}
	history := chain
	if asset == chainassets.BaseSepoliaETH {
		traceRPC := os.Getenv("DEXWALLET_TEST_BASE_DEPOSIT_TRACE_RPC")
		if traceRPC != "" {
			history, err = chainassets.New(chainassets.Config{Timeout: 30 * time.Second, Networks: []chainassets.Endpoint{{Network: chainassets.BaseSepolia, URL: traceRPC}}}, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer history.Close()
		}
	}
	w = deposit.NewBaseExecution(conn, runner, ledger, history)
	until := time.Now().Add(35 * time.Minute)
	for time.Now().Before(until) {
		time.Sleep(12 * time.Second)
		o, err = w.Step(ctx, q.QuoteIdx, deposit.BaseBudget{}, nil)
		if err != nil {
			t.Log("observation", err)
			continue
		}
		if o.TxHash.String != id || o.State == deposit.BaseReview {
			t.Fatal("unexpected Base result", o.State, o.ActualFeeWei)
		}
		if o.State != deposit.BaseCredited {
			continue
		}
		replay, err := w.Step(ctx, q.QuoteIdx, deposit.BaseBudget{}, nil)
		if err != nil || replay.CreditVoucherId != o.CreditVoucherId {
			t.Fatal("settlement replay", err)
		}
		page, err := ledger.Entries(ctx, 1, accounts.KindUSD, 0, 2)
		if err != nil || len(page.Entries) != 1 || page.Entries[0].DepositOrderIdx != q.QuoteIdx || !page.Entries[0].BalanceAfter.Equal(o.CreditedUsd.Decimal) {
			t.Fatal("owner statement", err)
		}
		t.Logf("finalized order=%s tx=%s height=%d fee_wei=%s credited_usd=%s entries=1", q.QuoteIdx, id, o.FinalizedHeight, o.ActualFeeWei, o.CreditedUsd.Decimal.StringFixed(2))
		return
	}
	t.Fatal("finality deadline", id)
}
