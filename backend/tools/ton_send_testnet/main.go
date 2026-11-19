// ton_send_testnet explicitly operates one pinned test intent in an isolated DB.
// Unknown outcomes retain both DB and run journal for a later invocation.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/internal/sendprobe"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainreceipt"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainsend"
	"github.com/0xabcd-k/DexWallet/backend/transaction"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

type report struct {
	Intent   chainsend.View             `json:"intent"`
	Delivery chainreceipt.TONSentStored `json:"delivery"`
	Evidence json.RawMessage            `json:"evidence"`
}

func run() error {
	action := flag.String("action", "status", "prepare, execute, reconcile, status, cleanup")
	state := flag.String("state", "", "local run reference JSON; retained on error")
	policy := flag.String("policy", "", "pinned public test intent JSON")
	seed := flag.String("seed-file", "", "private test seed passed only to the signing child")
	signer := flag.String("signer", "", "absolute path to ton_test_signer executable")
	rpc := flag.String("rpc", "https://testnet.toncenter.com", "TON Center testnet endpoint")
	evidence := flag.String("evidence", "", "public confirmed evidence output, required for cleanup")
	flag.Parse()
	if *state == "" || *policy == "" {
		return errors.New("state and policy are required")
	}
	switch *action {
	case "prepare", "execute", "reconcile", "status", "cleanup":
	default:
		return errors.New("unsupported action")
	}
	if *action == "cleanup" && *evidence == "" {
		return errors.New("cleanup requires a confirmed evidence output file")
	}
	raw, err := os.ReadFile(*policy)
	if err != nil {
		return err
	}
	var p chainsend.TestPolicy
	if err = json.Unmarshal(raw, &p); err != nil {
		return err
	}
	isolated, err := sendprobe.OpenSendDatabase(*state, "ton_send_live", *action == "prepare")
	if err != nil {
		return err
	}
	defer isolated.Close()
	db, run := isolated.DB, isolated.Run
	conn, tx := sqlx.NewSqlConnFromDB(db), transaction.New(db)
	assets, err := chainassets.New(chainassets.Config{Timeout: 30 * time.Second, Networks: []chainassets.Endpoint{{Network: chainassets.TONTestnet, URL: *rpc, TestUSDTContract: p.Wallet.JettonMaster, APIKey: os.Getenv("DEXWALLET_CHAIN_TON_TESTNET_API_KEY")}}}, nil)
	if err != nil {
		return err
	}
	defer assets.Close()
	executor, err := chainsend.NewTON(chainsend.NewStore(conn, tx), p.Wallet, chainsend.ProcessSigner{Executable: *signer, Args: []string{"--policy", *policy, "--seed-file", *seed}}, assets)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 55*time.Second)
	defer cancel()
	switch *action {
	case "prepare":
		v, err := executor.Prepare(ctx, p.Proposal)
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(v)
	case "execute":
		v, err := executor.Execute(ctx, p.Proposal.Key)
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(v)
	case "status":
		v, err := chainsend.NewStore(conn, tx).Find(ctx, p.Proposal.Key)
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(v)
	}
	delivery, err := executor.Reconcile(ctx, p.Proposal.Key, chainreceipt.NewTON(conn, tx, assets))
	if err != nil {
		return err
	}
	v, err := chainsend.NewStore(conn, tx).Find(ctx, p.Proposal.Key)
	if err != nil {
		return err
	}
	var proof string
	if err = db.QueryRowContext(ctx, "SELECT evidence_json FROM chain_observed_transaction WHERE network=? AND signature=?", v.Network, delivery.Receipt.Transaction.Signature).Scan(&proof); err != nil {
		return err
	}
	result := report{v, delivery, json.RawMessage(proof)}
	if *evidence != "" {
		b, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return err
		}
		if err = os.WriteFile(*evidence, append(b, '\n'), 0600); err != nil {
			return err
		}
	}
	if *action == "cleanup" {
		db.Close()
		if err = run.Cleanup(); err != nil {
			return err
		}
	}
	// stdout is concise; raw public evidence is written only when explicitly requested.
	return json.NewEncoder(os.Stdout).Encode(struct {
		Intent   chainsend.View             `json:"intent"`
		Delivery chainreceipt.TONSentStored `json:"delivery"`
		Cleaned  bool                       `json:"cleaned"`
	}{v, delivery, *action == "cleanup"})
}
func main() {
	// Normal query logs include signed payload arguments. This explicit probe emits
	// only public summaries; execution errors remain on stderr and in the intent.
	logx.Disable()
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
