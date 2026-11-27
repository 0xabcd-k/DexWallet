//go:build integration

package integration

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/internal/testenv"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
	"github.com/0xabcd-k/DexWallet/backend/logic/deposit"
	"github.com/0xabcd-k/DexWallet/backend/logic/user"
	"github.com/0xabcd-k/DexWallet/backend/transaction"
	"github.com/mr-tron/base58"
	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

// Explicit live test using only the user key of the funded disposable Devnet fixture.
func TestSolanaDevnetDepositAuthorization(t *testing.T) {
	liveSolOrder(t, false)
}

func TestSolanaDevnetDepositExecution(t *testing.T) {
	if os.Getenv("DEXWALLET_TEST_SOL_EXECUTE") != "1" {
		t.Skip("explicit one-order Devnet SOL transfer only")
	}
	liveSolOrder(t, true)
}

func liveSolOrder(t *testing.T, execute bool) {
	t.Helper()
	keyFile := os.Getenv("DEXWALLET_TEST_SOL_USER_KEY_FILE")
	if keyFile == "" {
		if execute {
			t.Fatal("explicit execution requires Devnet user key")
		}
		t.Skip("set the dedicated Devnet user key file to run the live simulation")
	}
	raw, err := os.ReadFile(keyFile)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(raw)
	var sk ed25519.PrivateKey
	if json.Unmarshal(raw, &sk) != nil || len(sk) != ed25519.PrivateKeySize {
		t.Fatal("expected a Devnet CLI user keypair")
	}
	defer clear(sk)
	const wallet = "9ucco3Ath4uS71gyfHggdo9jXzUnhkRr5oPHp8qVUVGf"
	if base58.Encode(sk.Public().(ed25519.PublicKey)) != wallet {
		t.Fatal("wrong Devnet test user")
	}
	endpoint := os.Getenv("DEXWALLET_TEST_SOL_RPC")
	if endpoint == "" {
		endpoint = "https://api.devnet.solana.com"
	}
	db, users := userDatabase(t, "sol_deposit_live")
	mustExec(t, db, "INSERT INTO user(id,nickname) VALUES(1,'devnet')")
	client := redis.NewClient(&redis.Options{Addr: testenv.RedisAddress, Password: os.Getenv("DEXWALLET_REDIS_PASSWORD")})
	defer client.Close()
	ns := testRun.Name("redis", "sol_deposit_live")
	now := time.Now().Unix()
	chain, err := chainassets.New(chainassets.Config{Timeout: 20 * time.Second, Networks: []chainassets.Endpoint{{Network: chainassets.SolanaDevnet, URL: endpoint}}, DepositQuotes: &chainassets.DepositQuoteConfig{SolanaSOLReviewAfter: 10 * time.Minute, Version: "devnet-auth-test", AsOf: now - 60, ValidUntil: now + 300, SolanaSOL: &chainassets.SolanaDepositFeeConfig{FeePayer: "DcfKLiLsSsoJrrf3t6QXwkjfUarE4yW5sKjzF7xBQkPN", Recipient: "7Njq3pBVPhZWEoRh8w22eC2KBcGUhhCvwFFitsyTg7A5"}, Assets: []chainassets.DepositQuoteRate{{AssetID: "solana-devnet:sol", USDPerCoin: "100", EstimatedGas: "0"}}}}, chainassets.NewRedisLimiter(client, ns, 30))
	if err != nil {
		t.Fatal(err)
	}
	defer chain.Close()
	conn := sqlx.NewSqlConnFromDB(db)
	quotes := deposit.NewConfirmations(conn, client, ns)
	orders := deposit.NewSolOrders(conn, quotes, users, transaction.New(db), chain)
	actor := user.Actor{UserID: 1, AuthVersion: 1}
	ctx := context.Background()
	q, err := chain.PreviewWalletDeposit(ctx, actor.UserID, "solana-devnet:sol", "0.01", wallet)
	if err != nil {
		t.Fatal(err)
	}
	q, err = quotes.Offer(ctx, actor.UserID, q)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = quotes.Confirm(ctx, actor.UserID, q.QuoteIdx); err != nil {
		t.Fatal(err)
	}
	o, err := orders.Prepare(ctx, actor, q.QuoteIdx)
	if err != nil {
		t.Fatal(err)
	}
	if o.AcceptedAt.Valid || o.UserSignature != "" {
		t.Fatal("unauthorized result on prepare")
	}
	msg, err := base64.StdEncoding.DecodeString(o.MessageBase64)
	if err != nil {
		t.Fatal(err)
	}
	signature := base58.Encode(ed25519.Sign(sk, msg))
	accepted, err := orders.Authorize(ctx, actor, q.QuoteIdx, signature)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := orders.Authorize(ctx, actor, q.QuoteIdx, signature)
	if err != nil || !accepted.AcceptedAt.Valid || accepted.AcceptedAt != replay.AcceptedAt {
		t.Fatal("acceptance recovery failed", err)
	}
	t.Logf("Devnet order %s message %s: gross %s SOL, fee cap %s SOL, accepted %s, quote expiry %s, last valid height %d", o.OrderIdx, o.MessageSha256, q.PayQuantity, q.MaxGas, accepted.AcceptedAt.Time.UTC().Format(time.RFC3339Nano), time.UnixMilli(q.ExpiresAt).UTC().Format(time.RFC3339Nano), o.LastValidHeight)

	if !execute {
		return
	}
	sponsorRaw, err := os.ReadFile(os.Getenv("DEXWALLET_TEST_SOL_SPONSOR_KEY_FILE"))
	if err != nil {
		t.Fatal(err)
	}
	defer clear(sponsorRaw)
	var sponsor ed25519.PrivateKey
	if err = json.Unmarshal(sponsorRaw, &sponsor); err != nil || len(sponsor) != ed25519.PrivateKeySize || base58.Encode(sponsor.Public().(ed25519.PublicKey)) != q.FeePayer {
		t.Fatal("wrong dedicated Devnet sponsor")
	}
	defer clear(sponsor)
	terms := chainassets.SolDepositTerms{Recipient: "7Njq3pBVPhZWEoRh8w22eC2KBcGUhhCvwFFitsyTg7A5", GrossLamports: 110000, FeeLamports: 10000}
	lost := &lostDevnetAck{Service: chain}
	worker := func() *deposit.SolExecution {
		w, err := deposit.NewSolExecution(conn, lost, q.QuoteIdx, terms, sponsor)
		if err != nil {
			t.Fatal(err)
		}
		return w
	}
	exec := worker()
	signed, err := exec.Step(ctx)
	if err != nil || signed.ExecutionState != deposit.SolSigned || !signed.TxSignature.Valid || lost.wire != nil {
		t.Fatal("persist before broadcast", err)
	}
	id := signed.TxSignature.String
	t.Logf("order=%s quote=%s Devnet tx=%s", o.OrderIdx, q.QuoteIdx, id)
	_, err = exec.Step(ctx)
	if !errors.Is(err, errLostDevnetAck) {
		t.Fatal("expected simulated loss after successful RPC submission", err)
	}
	exec = worker() // new executor owns no previous in-memory signing/submission state
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		time.Sleep(3 * time.Second)
		result, err := exec.Step(ctx)
		if err != nil {
			t.Log("observing original tx", err)
			continue
		}
		if result.TxSignature.String != id || result.ExecutionState == deposit.SolFailed {
			t.Fatal("unexpected execution result", result.ExecutionState)
		}
		if result.ExecutionState != deposit.SolFinalized {
			continue
		}
		again, err := worker().Step(ctx)
		if err != nil || again.TxSignature.String != id || again.FinalizedSlot != result.FinalizedSlot || again.ActualLamportsFee != terms.FeeLamports {
			t.Fatal("finalized replay", err)
		}
		seen, err := orders.Get(ctx, actor.UserID, q.QuoteIdx)
		if err != nil || seen.ExecutionState != deposit.SolFinalized || seen.TxSignature.String != id {
			t.Fatal("owner cannot recover chain result", err)
		}
		t.Logf("finalized slot=%d fee_lamports=%d gross_lamports=%d submissions_of_same_wire=%d", again.FinalizedSlot, again.ActualLamportsFee, terms.GrossLamports, lost.sent)
		return
	}
	t.Fatalf("Devnet observation deadline for original tx %s", id)
}

var errLostDevnetAck = errors.New("injected lost Devnet send acknowledgement")

type lostDevnetAck struct {
	*chainassets.Service
	wire []byte
	sent int
}

func (n *lostDevnetAck) SubmitSolDeposit(ctx context.Context, wire []byte) error {
	if n.wire != nil && !bytes.Equal(wire, n.wire) {
		return errors.New("attempted replacement transaction")
	}
	if err := n.Service.SubmitSolDeposit(ctx, wire); err != nil {
		return err
	}
	n.sent++
	if n.wire == nil {
		n.wire = bytes.Clone(wire)
		return errLostDevnetAck
	}
	return nil
}
