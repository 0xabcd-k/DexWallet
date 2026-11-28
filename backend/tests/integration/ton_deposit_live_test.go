//go:build integration

package integration

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/internal/testenv"
	"github.com/0xabcd-k/DexWallet/backend/logic/account"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainreceipt"
	"github.com/0xabcd-k/DexWallet/backend/logic/deposit"
	"github.com/0xabcd-k/DexWallet/backend/logic/user"
	accounts "github.com/0xabcd-k/DexWallet/backend/model/account"
	"github.com/0xabcd-k/DexWallet/backend/transaction"
	"github.com/redis/go-redis/v9"
	"github.com/xssnick/tonutils-go/ton/wallet"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

const tonPayer = "0:a393a594688a53f6138d88f3447a8991093cc387f0463d0acd955289fadbef7a"
const tonSponsor = "0:100769e6dae579cfbc5ca1637c88d6c83a6c2195b3e571f1e35f20b45b2fa4d0"

type tonDepositProof struct {
	Quote                                            chainassets.DepositPreview
	UserSignature, OrderSignature, PlatformSignature string
	Evidence                                         chainassets.TONEvidence
}
type tonLostAck struct {
	*chainassets.Service
	sent bool
}

func (n *tonLostAck) SubmitTON(ctx context.Context, net string, wire []byte) error {
	if n.sent {
		return errors.New("original dispatch was already submitted")
	}
	err := n.Service.SubmitTON(ctx, net, wire)
	if err != nil {
		return err
	}
	n.sent = true
	return errors.New("test dropped TON broadcast acknowledgement")
}

func TestTONNativeSelfDepositLive(t *testing.T) {
	if os.Getenv("DEXWALLET_TEST_TON_NATIVE_DEPOSIT_EXECUTE") != "1" {
		t.Skip("requires explicit TON Testnet transfer")
	}
	raw, err := os.ReadFile(os.Getenv("DEXWALLET_TEST_TON_USER_KEYS"))
	if err != nil {
		t.Fatal(err)
	}
	defer clear(raw)
	var source struct{ UserMnemonic []string }
	if err = json.Unmarshal(raw, &source); err != nil {
		t.Fatal(err)
	}
	k, err := wallet.SeedToPrivateKey(source.UserMnemonic, "", false)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(k)
	raw2, err := os.ReadFile(os.Getenv("DEXWALLET_TEST_TON_PLATFORM_PHRASE"))
	if err != nil {
		t.Fatal(err)
	}
	defer clear(raw2)
	sk, err := wallet.SeedToPrivateKey(strings.Fields(string(raw2)), "", false)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(sk)
	db, users := userDatabase(t, "ton_native_live")
	mustExec(t, db, "INSERT INTO user(id,nickname) VALUES(1,'ton-test')")
	rdb := redis.NewClient(&redis.Options{Addr: testenv.RedisAddress, Password: os.Getenv("DEXWALLET_REDIS_PASSWORD")})
	defer rdb.Close()
	ns := testRun.Name("redis", "ton_native_live")
	now := time.Now().Unix()
	chain, err := chainassets.New(chainassets.Config{Timeout: 25 * time.Second, Networks: []chainassets.Endpoint{{Network: chainassets.TONTestnet, URL: "https://testnet.toncenter.com"}}, DepositQuotes: &chainassets.DepositQuoteConfig{TONNativeExecution: true, TONNative: &chainassets.TONDepositFeeConfig{PlatformWallet: tonSponsor, TonAPIURL: "https://testnet.tonapi.io"}, Version: "w5-self-test", Assets: []chainassets.DepositQuoteRate{{AssetID: "ton-testnet:ton", USDPerCoin: "1", EstimatedGas: "0.001"}}, AsOf: now - 60, ValidUntil: now + 3600}}, chainassets.NewRedisLimiter(rdb, ns, 30))
	if err != nil {
		t.Fatal(err)
	}
	defer chain.Close()
	conn := sqlx.NewSqlConnFromDB(db)
	runner := transaction.New(db)
	ledger := account.New(accounts.NewUserAccountModel(conn))
	quotes := deposit.NewConfirmations(conn, rdb, ns)
	orders := deposit.NewTonOrders(conn, quotes, users, runner, chain)
	ctx := context.Background()
	q, err := chain.PreviewWalletDeposit(ctx, 1, "ton-testnet:ton", "0.01", tonPayer)
	if err != nil {
		t.Fatal("preview", err, errors.Unwrap(err))
	}
	q, err = quotes.Offer(ctx, 1, q)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = quotes.Confirm(ctx, 1, q.QuoteIdx); err != nil {
		t.Fatal(err)
	}
	b := deposit.TonBudget{Sender: tonPayer, Recipient: tonSponsor, MaxGrossNano: 13_000_000, MaxFeeNano: 3_000_000}
	r, err := q.TONRelay(nil, nil)
	if err != nil || r.Atoms.Int64() > b.MaxGrossNano || r.Recipient != b.Recipient || r.Sender != b.Sender || q.RelayAttached != "0.003" {
		t.Fatal("unexpected test transfer", err)
	}
	sig := ed25519.Sign(k, r.UserDigest)
	digest := q.TONOrderDigest()
	proof := tonDepositProof{Quote: q, UserSignature: hex.EncodeToString(sig), OrderSignature: hex.EncodeToString(ed25519.Sign(k, digest[:]))}
	o, err := orders.Authorize(ctx, user.Actor{UserID: 1, AuthVersion: 1}, q.QuoteIdx, proof.UserSignature, proof.OrderSignature)
	if err != nil || o.State != deposit.Accepted {
		t.Fatal("admit", err)
	}
	lost := &tonLostAck{Service: chain}
	worker := deposit.NewTonExecution(conn, runner, ledger, lost)
	for attempt := 0; attempt < 6; attempt++ {
		o, err = worker.Step(ctx, q.QuoteIdx, b, sk)
		if err == nil || !strings.Contains(err.Error(), "429") {
			break
		}
		time.Sleep(4 * time.Second)
	}
	if err != nil || o.State != deposit.TonSigned {
		t.Fatal("platform signature", err)
	}
	proof.PlatformSignature = o.PlatformSignature.String
	save := func() {
		if path := os.Getenv("DEXWALLET_TEST_TON_PUBLIC_PROOF"); path != "" {
			data, e := json.MarshalIndent(proof, "", "  ")
			if e != nil {
				t.Fatal(e)
			}
			if e = os.WriteFile(path, append(data, '\n'), 0600); e != nil {
				t.Fatal(e)
			}
		}
	}
	save()
	t.Logf("signed quote=%s dispatch=%s gross=%s fee_cap=%s", q.QuoteIdx, o.DispatchHash.String, q.PayQuantity, q.MaxGas)
	for deadline := time.Now().Add(6 * time.Minute); time.Now().Before(deadline); {
		o, err = worker.Step(ctx, q.QuoteIdx, deposit.TonBudget{}, nil)
		if lost.sent {
			worker = deposit.NewTonExecution(conn, runner, ledger, chain)
		}
		if err != nil {
			t.Log("observation", err)
			time.Sleep(4 * time.Second)
			continue
		}
		if o.State == deposit.TonCredited || o.State == deposit.TonReview {
			for tries := 0; tries < 5; tries++ {
				proof.Evidence, err = chain.ReadTONDepositEvidence(ctx, o.DispatchHash.String)
				if err == nil && len(proof.Evidence.Block) > 0 {
					break
				}
				time.Sleep(4 * time.Second)
			}
			if err != nil || len(proof.Evidence.Block) == 0 {
				t.Fatal("final proof", err)
			}
			save()
			if o.State != deposit.TonCredited || !o.ReceiveHash.Valid || !o.CreditVoucherId.Valid {
				t.Fatal("final review", o.ActualFeeNano, o.ReceiveHash)
			}
			if !lost.sent {
				t.Fatal("missing simulated lost acknowledgement")
			}
			again, e := worker.Step(ctx, q.QuoteIdx, deposit.TonBudget{}, nil)
			if e != nil || again.CreditVoucherId != o.CreditVoucherId {
				t.Fatal("replay", e)
			}
			page, e := ledger.Entries(ctx, 1, accounts.KindUSD, 0, 2)
			if e != nil || len(page.Entries) != 1 || page.Entries[0].DepositOrderIdx != q.QuoteIdx || page.Entries[0].BalanceAfter.StringFixed(2) != "0.01" {
				t.Fatal("statement", e)
			}
			storedSig, e := hex.DecodeString(o.PlatformSignature.String)
			if e != nil {
				t.Fatal(e)
			}
			relay, e := q.TONRelay(sig, storedSig)
			if e != nil {
				t.Fatal(e)
			}
			receipt, total, e := chainreceipt.VerifyTONDeposit(proof.Evidence, relay)
			if e != nil || total.Int64() != o.ActualFeeNano.Int64 {
				t.Fatal("receipt replay", e)
			}
			t.Logf("credited receive=%s masterchain=%d fee_nano=%d usd=%s", o.ReceiveHash.String, receipt.Transaction.Slot, o.ActualFeeNano.Int64, o.CreditedUsd.Decimal.StringFixed(2))
			return
		}
		time.Sleep(4 * time.Second)
	}
	t.Fatal("no final TON receipt", q.QuoteIdx)
}
