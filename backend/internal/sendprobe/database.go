// Package sendprobe manages retained databases for explicit testnet payouts.
package sendprobe

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/internal/migration"
	"github.com/0xabcd-k/DexWallet/backend/internal/testenv"
	"github.com/golang-migrate/migrate/v4"
)

// SendDatabase retains the run across separate signing, broadcast and observation processes.
type SendDatabase struct {
	DB   *sql.DB
	Run  *testenv.Run
	lock *os.File
}

func (s *SendDatabase) Close() error { return errors.Join(s.DB.Close(), s.lock.Close()) }

func OpenSendDatabase(state, label string, prepare bool) (*SendDatabase, error) {
	var stateData struct{ RunDir string }
	raw, err := os.ReadFile(state)
	var run *testenv.Run
	if errors.Is(err, os.ErrNotExist) && prepare {
		run, err = testenv.NewRun()
		if err != nil {
			return nil, err
		}
		stateData.RunDir = run.Dir
		raw, _ = json.Marshal(stateData)
		file, err := os.OpenFile(state, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return nil, err
		}
		_, writeErr := file.Write(raw)
		closeErr := file.Close()
		if err = errors.Join(writeErr, closeErr); err != nil {
			return nil, err
		}
		_, _, db, err := run.NewDatabase(context.Background(), label)
		if err != nil {
			return nil, err
		}
		db.Close()
	} else {
		if err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &stateData); err != nil {
			return nil, err
		}
		run, err = testenv.LoadRun(stateData.RunDir)
		if err != nil {
			return nil, err
		}
	}
	lock, err := testenv.Lock(run.Dir)
	if err != nil {
		return nil, err
	}

	db, err := openRegisteredSendDB(run, label, prepare)
	if err != nil {
		lock.Close()
		return nil, err
	}
	return &SendDatabase{db, run, lock}, nil
}
func openRegisteredSendDB(run *testenv.Run, label string, prepare bool) (*sql.DB, error) {
	cfg, err := testenv.AdminConfig()
	if err != nil {
		return nil, err
	}
	cfg.DBName = run.Name("database", label)
	owns := false
	for _, r := range run.Resources {
		if r.Kind == "database" && r.Name == cfg.DBName {
			owns = true
		}
	}
	if !owns {
		return nil, errors.New("isolated DB is no longer registered; do not recreate a completed send")
	}
	cfg.ParseTime = true
	cfg.Loc = time.UTC
	cfg.MultiStatements = true
	if cfg.Params == nil {
		cfg.Params = map[string]string{}
	}
	cfg.Params["time_zone"] = "'+00:00'"
	dsn := cfg.FormatDSN()
	if prepare {
		root, err := testenv.Root()
		if err != nil {
			return nil, err
		}
		m, err := migration.Open(dsn, cfg.DBName, filepath.Join(root, "backend/manifest/sql/diff/business"), "business")
		if err != nil {
			return nil, err
		}
		err = m.Up()
		a, b := m.Close()
		if err != nil && !errors.Is(err, migrate.ErrNoChange) {
			return nil, err
		}
		if err = errors.Join(a, b); err != nil {
			return nil, err
		}
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	return db, nil
}
