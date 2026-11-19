// ton_send_testnet explicitly operates one pinned test intent in an isolated DB.
// Unknown outcomes retain both DB and run journal for a later invocation.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/internal/migration"
	"github.com/0xabcd-k/DexWallet/backend/internal/testenv"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainreceipt"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainsend"
	"github.com/0xabcd-k/DexWallet/backend/transaction"
	"github.com/golang-migrate/migrate/v4"
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
	var stateData struct{ RunDir string }
	raw, err = os.ReadFile(*state)
	var run *testenv.Run
	if errors.Is(err, os.ErrNotExist) && *action == "prepare" {
		run, err = testenv.NewRun()
		if err != nil {
			return err
		}
		stateData.RunDir = run.Dir
		raw, _ = json.Marshal(stateData)
		file, err := os.OpenFile(*state, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return err
		}
		_, writeErr := file.Write(raw)
		closeErr := file.Close()
		if err = errors.Join(writeErr, closeErr); err != nil {
			return err
		}
		_, _, db, err := run.NewDatabase(context.Background(), "ton_send_live")
		if err != nil {
			return err
		}
		db.Close()
	} else {
		if err != nil {
			return err
		}
		if err = json.Unmarshal(raw, &stateData); err != nil {
			return err
		}
		run, err = testenv.LoadRun(stateData.RunDir)
		if err != nil {
			return err
		}
	}
	lock, err := testenv.Lock(run.Dir)
	if err != nil {
		return err
	}
	defer lock.Close()
	cfg, err := testenv.AdminConfig()
	if err != nil {
		return err
	}
	cfg.DBName = run.Name("database", "ton_send_live")
	owns := false
	for _, r := range run.Resources {
		if r.Kind == "database" && r.Name == cfg.DBName {
			owns = true
		}
	}
	if !owns {
		return errors.New("isolated DB is no longer registered; do not recreate a completed send")
	}
	cfg.ParseTime = true
	cfg.Loc = time.UTC
	cfg.MultiStatements = true
	if cfg.Params == nil {
		cfg.Params = map[string]string{}
	}
	cfg.Params["time_zone"] = "'+00:00'"
	dsn := cfg.FormatDSN()
	if *action == "prepare" {
		root, err := testenv.Root()
		if err != nil {
			return err
		}
		m, err := migration.Open(dsn, cfg.DBName, filepath.Join(root, "backend/manifest/sql/diff/business"), "business")
		if err != nil {
			return err
		}
		err = m.Up()
		a, b := m.Close()
		if err != nil && !errors.Is(err, migrate.ErrNoChange) {
			return err
		}
		if err = errors.Join(a, b); err != nil {
			return err
		}
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return err
	}
	defer db.Close()
	conn, tx := sqlx.NewSqlConnFromDB(db), transaction.New(db)
	assets, err := chainassets.New(chainassets.Config{Timeout: 30 * time.Second, Networks: []chainassets.Endpoint{{Network: chainassets.TONTestnet, URL: *rpc, APIKey: os.Getenv("DEXWALLET_CHAIN_TON_TESTNET_API_KEY")}}}, nil)
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
