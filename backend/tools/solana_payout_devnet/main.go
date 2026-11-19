// solana_payout_devnet runs one explicit SOL payout in a retained local test database.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/internal/sendprobe"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainreceipt"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainsend"
	"github.com/0xabcd-k/DexWallet/backend/transaction"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

func run() error {
	state := flag.String("state", "", "retained local run reference")
	policy := flag.String("policy", "", "public JSON with key, sender, recipient and lamports")
	action := flag.String("action", "status", "prepare, bind, step, status, evidence or cleanup")
	sigFile := flag.String("signature-file", "", "file containing the detached base58 signature")
	rpc := flag.String("rpc", "https://api.devnet.solana.com", "trusted Devnet RPC")
	flag.Parse()
	switch *action {
	case "prepare", "bind", "step", "status", "evidence", "cleanup":
	default:
		return errors.New("invalid action")
	}
	if *state == "" || *policy == "" {
		return errors.New("state and policy required")
	}
	var p struct{ Key, Sender, Recipient, Lamports string }
	b, err := os.ReadFile(*policy)
	if err != nil {
		return err
	}
	if err = json.Unmarshal(b, &p); err != nil {
		return err
	}
	isolated, err := sendprobe.OpenSendDatabase(*state, "sol_payout", *action == "prepare")
	if err != nil {
		return err
	}
	defer isolated.Close()
	assets, err := chainassets.New(chainassets.Config{Timeout: 20 * time.Second, Networks: []chainassets.Endpoint{{Network: chainassets.SolanaDevnet, URL: *rpc}}}, nil)
	if err != nil {
		return err
	}
	defer assets.Close()
	conn := sqlx.NewSqlConnFromDB(isolated.DB)
	receipts := chainreceipt.New(conn, transaction.New(isolated.DB), assets)
	service, err := chainsend.NewSolana(conn, p.Sender, assets, receipts)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	out := json.NewEncoder(os.Stdout)
	if *action == "prepare" {
		row, err := service.Prepare(ctx, p.Key, p.Recipient, p.Lamports)
		if err != nil {
			return err
		}
		return out.Encode(row)
	}
	row, err := service.Find(ctx, p.Key)
	if err != nil {
		return err
	}
	if row.Recipient != p.Recipient || row.Lamports != p.Lamports {
		return chainsend.ErrConflict
	}
	switch *action {
	case "status":
		return out.Encode(row)
	case "bind":
		sig, err := os.ReadFile(*sigFile)
		if err != nil {
			return err
		}
		row, err = service.Bind(ctx, p.Key, strings.TrimSpace(string(sig)))
		if err != nil {
			return err
		}
		return out.Encode(row)
	case "step":
		row, err = service.Step(ctx, p.Key)
		if err != nil {
			return err
		}
		return out.Encode(row)
	}
	if row.State != chainsend.SolanaConfirmed {
		return errors.New("confirmed payout required")
	}
	receipt, err := receipts.Find(ctx, chainassets.SolanaDevnet, row.Signature, 0)
	if err != nil {
		return err
	}
	if *action == "cleanup" {
		var count int
		if err = isolated.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM solana_payout").Scan(&count); err != nil {
			return err
		}
		if count != 1 {
			return errors.New("cleanup requires exactly one confirmed payout")
		}
		if err = isolated.DB.Close(); err != nil {
			return err
		}
		return isolated.Run.Cleanup()
	}
	var evidence json.RawMessage
	err = isolated.DB.QueryRowContext(ctx, "SELECT evidence_json FROM chain_observed_transaction WHERE network=? AND signature=?", chainassets.SolanaDevnet, row.Signature).Scan(&evidence)
	if err != nil {
		return err
	}
	return out.Encode(map[string]any{"intent": row, "receipt": receipt, "evidence": evidence})
}
func main() {
	logx.Disable()
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
