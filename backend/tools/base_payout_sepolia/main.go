// base_payout_sepolia drives one ETH or USDC payout in a retained isolated test database.
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
	state := flag.String("state", "", "local run reference")
	policy := flag.String("policy", "", "public test payout authorization")
	action := flag.String("action", "status", "prepare, bind, step, status, evidence or cleanup")
	wireFile := flag.String("signed-file", "", "file with hex-encoded signed transaction")
	rpc := flag.String("rpc", "https://sepolia.base.org", "trusted Base Sepolia RPC")
	flag.Parse()
	switch *action {
	case "prepare", "bind", "step", "status", "evidence", "cleanup":
	default:
		return errors.New("invalid action")
	}
	if *state == "" || *policy == "" {
		return errors.New("state and policy required")
	}
	var p struct {
		chainsend.BaseTransfer
		Key, Sender string
		ChainID     int64
	}
	b, err := os.ReadFile(*policy)
	if err != nil {
		return err
	}
	if err = json.Unmarshal(b, &p); err != nil {
		return err
	}
	if p.ChainID != chainassets.BaseSepoliaChainID {
		return errors.New("Base Sepolia policy required")
	}
	if p.AssetID == "" {
		p.AssetID = chainassets.BaseSepoliaETH
	}
	db, err := sendprobe.OpenSendDatabase(*state, "base_payout", *action == "prepare")
	if err != nil {
		return err
	}
	defer db.Close()
	assets, err := chainassets.New(chainassets.Config{Timeout: 30 * time.Second, Networks: []chainassets.Endpoint{{Network: chainassets.BaseSepolia, URL: *rpc}}}, nil)
	if err != nil {
		return err
	}
	defer assets.Close()
	conn := sqlx.NewSqlConnFromDB(db.DB)
	receipts := chainreceipt.NewBase(conn, transaction.New(db.DB), assets)
	s, err := chainsend.NewBase(conn, p.Sender, assets, receipts)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	out := json.NewEncoder(os.Stdout)
	if *action == "prepare" {
		row, err := s.Prepare(ctx, p.Key, p.BaseTransfer)
		if err != nil {
			return err
		}
		return out.Encode(row)
	}
	row, err := s.Find(ctx, p.Key)
	if err != nil {
		return err
	}
	if !strings.EqualFold(row.Recipient, p.Recipient) || row.AssetId != p.AssetID || row.TokenAtoms != p.TokenAtoms || row.WeiAmount != p.WeiAmount || row.GasLimit != p.GasLimit || row.FeeCap != p.FeeCap || row.TipCap != p.TipCap {
		return chainsend.ErrConflict
	}
	switch *action {
	case "status":
		return out.Encode(row)
	case "bind":
		wire, err := os.ReadFile(*wireFile)
		if err != nil {
			return err
		}
		row, err = s.Bind(ctx, p.Key, strings.TrimSpace(string(wire)))
		if err != nil {
			return err
		}
		return out.Encode(row)
	case "step":
		row, err = s.Step(ctx, p.Key)
		if err != nil {
			return err
		}
		return out.Encode(row)
	}
	if row.State != chainsend.BaseConfirmed {
		return errors.New("confirmed payout required")
	}
	kind, index := chainreceipt.EVMValue, 0
	if row.TransferLogIndex.Valid {
		kind, index = chainreceipt.EVMLog, int(row.TransferLogIndex.Int64)
	}
	receipt, err := receipts.Find(ctx, chainassets.BaseSepolia, row.TxHash, kind, index)
	if err != nil {
		return err
	}
	if *action == "cleanup" {
		var n int
		if err = db.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM base_payout").Scan(&n); err != nil {
			return err
		}
		if n != 1 {
			return errors.New("cleanup requires a single confirmed payout")
		}
		if err = db.DB.Close(); err != nil {
			return err
		}
		return db.Run.Cleanup()
	}
	var evidence json.RawMessage
	if err = db.DB.QueryRowContext(ctx, "SELECT evidence_json FROM chain_observed_transaction WHERE network=? AND signature=?", chainassets.BaseSepolia, row.TxHash).Scan(&evidence); err != nil {
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
