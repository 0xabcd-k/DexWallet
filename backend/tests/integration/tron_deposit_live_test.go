//go:build integration

package integration

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/internal/testenv"
	"github.com/0xabcd-k/DexWallet/backend/internal/tronnative"
	"github.com/0xabcd-k/DexWallet/backend/logic/account"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
	"github.com/0xabcd-k/DexWallet/backend/logic/deposit"
	"github.com/0xabcd-k/DexWallet/backend/logic/user"
	accounts "github.com/0xabcd-k/DexWallet/backend/model/account"
	"github.com/0xabcd-k/DexWallet/backend/transaction"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

type nileDepositProof struct {
	Quote          chainassets.DepositPreview `json:"quote"`
	UserSignature  string                     `json:"userSignature"`
	OrderSignature string                     `json:"orderSignature"`
	Evidence       chainassets.TRONEvidence   `json:"evidence"`
}

var errNileAckLost = errors.New("simulated loss of Nile broadcast acknowledgement")

type nileLostAck struct{ *chainassets.Service }

func (n nileLostAck) SubmitNileTRX(ctx context.Context, s tronnative.Signed) error {
	if err := n.Service.SubmitNileTRX(ctx, s); err != nil {
		return err
	}
	return errNileAckLost
}

func TestNileTRXSelfDepositLive(t *testing.T) {
	if os.Getenv("DEXWALLET_TEST_NILE_TRX_DEPOSIT_EXECUTE") != "1" {
		t.Skip("requires explicit Nile test TRX transfer")
	}
	const payer = "TDHZum2JgP2ZgfnveW9f3VM3UjaafdJXio"
	const platform = "TPDBmTxJWYucGk5VjDsSi6jBdvVjpCnCsH"
	raw, err := os.ReadFile(os.Getenv("DEXWALLET_TEST_NILE_DEPOSIT_KEY_FILE"))
	if err != nil {
		t.Fatal(err)
	}
	defer clear(raw)
	var wallet struct{ PrivateKey string }
	if err = json.Unmarshal(raw, &wallet); err != nil {
		t.Fatal(err)
	}
	key, err := crypto.HexToECDSA(strings.TrimPrefix(wallet.PrivateKey, "0x"))
	if err != nil {
		t.Fatal("invalid dedicated Nile test key")
	}
	db, users := userDatabase(t, "tron_deposit_live")
	mustExec(t, db, "INSERT INTO user(id,nickname) VALUES(1,'nile-dev')")
	client := redis.NewClient(&redis.Options{Addr: testenv.RedisAddress, Password: os.Getenv("DEXWALLET_REDIS_PASSWORD")})
	defer client.Close()
	ns := testRun.Name("redis", "tron_deposit_live")
	now := time.Now().Unix()
	route := &chainassets.TRONDepositFeeConfig{Recipient: platform, ResourceOwner: platform}
	cfg := chainassets.DepositQuoteConfig{Version: "nile-self-test", AsOf: now - 10, ValidUntil: now + 3600, TRONTRXExecution: true, TRONTRX: route, Assets: []chainassets.DepositQuoteRate{{AssetID: chainassets.TRONNileTRX, USDPerCoin: "10", EstimatedGas: "0"}}}
	chain, err := chainassets.New(chainassets.Config{Timeout: 20 * time.Second, Networks: []chainassets.Endpoint{{Network: chainassets.TRONNile, URL: "https://nile.trongrid.io"}}, DepositQuotes: &cfg}, chainassets.NewRedisLimiter(client, ns, 30))
	if err != nil {
		t.Fatal(err)
	}
	defer chain.Close()
	ctx := context.Background()
	conn := sqlx.NewSqlConnFromDB(db)
	runner := transaction.New(db)
	quotes := deposit.NewConfirmations(conn, client, ns)
	orders := deposit.NewTronOrders(conn, quotes, users, runner, chain)
	ledger := account.New(accounts.NewUserAccountModel(conn))
	q, err := chain.PreviewWalletDeposit(ctx, 1, chainassets.TRONNileTRX, "0.01", payer)
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
	tx, err := q.NileDepositTransfer()
	if err != nil || tx.Sun != 1000 || tx.Sender != payer || tx.Recipient != platform || q.FeePayer != platform || q.MaxGas != "0" {
		t.Fatal("outside dedicated test budget", err)
	}
	hash, _ := hex.DecodeString(q.MessageSHA256)
	sig, err := crypto.Sign(hash, key)
	if err != nil {
		t.Fatal(err)
	}
	digest := chainassets.NileDepositOrderDigest(q.QuoteIdx, q.MessageSHA256)
	bound, err := crypto.Sign(digest[:], key)
	if err != nil {
		t.Fatal(err)
	}
	proof := nileDepositProof{Quote: q, UserSignature: hex.EncodeToString(sig), OrderSignature: hex.EncodeToString(bound)}
	actor := user.Actor{UserID: 1, AuthVersion: 1}
	o, err := orders.Authorize(ctx, actor, q.QuoteIdx, proof.UserSignature, proof.OrderSignature)
	if err != nil || o.State != deposit.TronSigned {
		t.Fatal("admit", err)
	}
	t.Logf("signed order=%s tx=%s reserved_bandwidth=%d", o.QuoteIdx, o.TxId, q.DelegatedBandwidth)
	worker := deposit.NewTronExecution(conn, runner, ledger, nileLostAck{chain})
	if _, err = worker.Step(ctx, q.QuoteIdx); !errors.Is(err, errNileAckLost) {
		t.Fatal("broadcast", err)
	}
	worker = deposit.NewTronExecution(conn, runner, ledger, chain)
	for deadline := time.Now().Add(5 * time.Minute); time.Now().Before(deadline); {
		time.Sleep(4 * time.Second)
		o, err = worker.Step(ctx, q.QuoteIdx)
		if err != nil {
			t.Log("observation", err)
			continue
		}
		if o.State == deposit.TronReview {
			t.Fatal("review", o.ActualBandwidth, o.ActualFeeSun)
		}
		if o.State != deposit.TronCredited {
			continue
		}
		again, err := worker.Step(ctx, q.QuoteIdx)
		if err != nil || again.CreditVoucherId != o.CreditVoucherId || o.ActualFeeSun.Int64 != 0 || o.ActualBandwidth.Int64 != int64(q.DelegatedBandwidth) {
			t.Fatal("resource use or replay", err)
		}
		page, err := ledger.Entries(ctx, 1, accounts.KindUSD, 0, 2)
		if err != nil || len(page.Entries) != 1 || page.Entries[0].DepositOrderIdx != q.QuoteIdx || page.Entries[0].BalanceAfter.StringFixed(2) != "0.01" {
			t.Fatal("statement", err)
		}
		if path := os.Getenv("DEXWALLET_TEST_NILE_PUBLIC_PROOF"); path != "" {
			proof.Evidence, err = chain.ReadTRONEvidence(ctx, chainassets.TRONNile, o.TxId)
			if err != nil {
				t.Fatal(err)
			}
			data, err := json.MarshalIndent(proof, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(path, append(data, '\n'), 0600); err != nil {
				t.Fatal(err)
			}
		}
		t.Logf("solid order=%s tx=%s height=%d bandwidth=%d fee_sun=%d credited_usd=%s entries=1", o.QuoteIdx, o.TxId, o.FinalizedHeight.Int64, o.ActualBandwidth.Int64, o.ActualFeeSun.Int64, o.CreditedUsd.Decimal.StringFixed(2))
		return
	}
	t.Fatal("Nile solidification timeout", q.MessageSHA256)
}
