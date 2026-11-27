//go:build integration

package integration

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
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
	keyFile := os.Getenv("DEXWALLET_TEST_SOL_USER_KEY_FILE")
	if keyFile == "" {
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
}
