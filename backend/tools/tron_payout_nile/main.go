// tron_payout_nile drives one native TRX payout in a retained isolated test database.
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
	wireFile := flag.String("signed-file", "", "file with original 65-byte hex signature")
	rpc := flag.String("rpc", "https://nile.trongrid.io", "trusted TRON Nile RPC")
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
		Key, Sender, Recipient, Network string
		Sun                             int64
	}
	b, err := os.ReadFile(*policy)
	if err != nil {
		return err
	}
	if err = json.Unmarshal(b, &p); err != nil {
		return err
	}
	if p.Network != chainassets.TRONNile {
		return errors.New("Nile test policy required")
	}
	db, err := sendprobe.OpenSendDatabase(*state, "tron_payout", *action == "prepare")
	if err != nil {
		return err
	}
	defer db.Close()
	assets, err := chainassets.New(chainassets.Config{Timeout: 30 * time.Second, Networks: []chainassets.Endpoint{{Network: chainassets.TRONNile, URL: *rpc}}}, nil)
	if err != nil {
		return err
	}
	defer assets.Close()
	conn := sqlx.NewSqlConnFromDB(db.DB)
	receipts := chainreceipt.NewTRON(conn, transaction.New(db.DB), assets)
	s, err := chainsend.NewTRON(conn, p.Sender, assets, receipts)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	out := json.NewEncoder(os.Stdout)
	if *action == "prepare" {
		row, err := s.Prepare(ctx, p.Key, p.Recipient, p.Sun)
		if err != nil {
			return err
		}
		return out.Encode(row)
	}
	row, err := s.Find(ctx, p.Key)
	if err != nil {
		return err
	}
	if row.Recipient != p.Recipient || row.Sun != p.Sun {
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
	if row.State != chainsend.TRONConfirmed {
		return errors.New("confirmed payout required")
	}
	receipt, err := receipts.Find(ctx, chainassets.TRONNile, row.TxId, chainreceipt.TRONTransfer, 0)
	if err != nil {
		return err
	}
	if *action == "cleanup" {
		var n int
		if err = db.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM tron_payout").Scan(&n); err != nil {
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
	if err = db.DB.QueryRowContext(ctx, "SELECT evidence_json FROM chain_observed_transaction WHERE network=? AND signature=?", chainassets.TRONNile, row.TxId).Scan(&evidence); err != nil {
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
