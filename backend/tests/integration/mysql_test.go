//go:build integration

package integration

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/internal/migration"
	"github.com/0xabcd-k/DexWallet/backend/internal/testenv"
	"github.com/0xabcd-k/DexWallet/backend/model/example"
	"github.com/0xabcd-k/DexWallet/backend/transaction"
	"github.com/golang-migrate/migrate/v4"
	"github.com/shopspring/decimal"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

func TestMySQLContract(t *testing.T) {
	cfg, err := testenv.AdminConfig()
	if err != nil {
		t.Fatal(err)
	}
	cfg.DBName = ""
	cfg.ParseTime = true
	cfg.MultiStatements = true
	cfg.Timeout = 10 * time.Second
	admin, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { admin.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if err = admin.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
	var serverVersion string
	if err = admin.QueryRowContext(ctx, "SELECT VERSION()").Scan(&serverVersion); err != nil {
		t.Fatal(err)
	}
	t.Log("MySQL server:", serverVersion)
	databaseIndex := 0
	newDatabase := func() (string, string, *sql.DB) {
		databaseIndex++
		name, dsn, db, e := testRun.NewDatabase(ctx, fmt.Sprintf("contract_%d", databaseIndex))
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() {
			if e := db.Close(); e != nil {
				t.Error(e)
			}
		})
		return name, dsn, db
	}
	name, dsn, upgraded := newDatabase()
	_, _, full := newDatabase()
	nameFresh, dsnFresh, fresh := newDatabase()
	root := filepath.Join("..", "..", "manifest", "sql")
	directory := filepath.Join(root, "diff", "examples")
	open := func(dsn, name, dir string) *migrate.Migrate {
		m, err := migration.Open(dsn, name, dir, "examples")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			a, b := m.Close()
			if a != nil || b != nil {
				t.Errorf("close migration: %v %v", a, b)
			}
		})
		return m
	}
	m := open(dsn, name, directory)
	if err = m.Steps(1); err != nil {
		t.Fatal(err)
	}
	precise := "9007199254740993.123456789012345678"
	if _, err = upgraded.ExecContext(ctx, "INSERT INTO contract_sample(sample_key,amount,optional_amount) VALUES(?,?,NULL)", "old", precise); err != nil {
		t.Fatal(err)
	}
	if err = m.Up(); err != nil {
		t.Fatal(err)
	}
	if err = m.Up(); !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("repeat up: %v", err)
	}
	version, dirty, err := m.Version()
	if err != nil || version != 2 || dirty {
		t.Fatalf("migration state: %d %t %v", version, dirty, err)
	}
	fm := open(dsnFresh, nameFresh, directory)
	if err = fm.Up(); err != nil {
		t.Fatal(err)
	}
	ddl, err := os.ReadFile(filepath.Join(root, "ddl", "examples", "contract_sample.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = full.ExecContext(ctx, string(ddl)); err != nil {
		t.Fatal(err)
	}
	structure := func(db *sql.DB) string {
		var table, create string
		if err := db.QueryRowContext(ctx, "SHOW CREATE TABLE contract_sample").Scan(&table, &create); err != nil {
			t.Fatal(err)
		}
		// Auto increment's current counter is data, not schema.
		parts := strings.Fields(create)
		filtered := parts[:0]
		for _, part := range parts {
			if !strings.HasPrefix(part, "AUTO_INCREMENT=") {
				filtered = append(filtered, part)
			}
		}
		return strings.Join(filtered, " ")
	}
	if want, got := structure(full), structure(upgraded); want != got {
		t.Fatalf("DDL / upgrade mismatch:\n%s\n%s", want, got)
	}
	if structure(full) != structure(fresh) {
		t.Fatal("fresh migration differs from full DDL")
	}
	model := example.NewContractSampleModel(sqlx.NewSqlConnFromDB(upgraded))
	old, err := model.FindOneBySampleKey(ctx, "old")
	if err != nil || old.Amount.String() != precise || old.OptionalAmount.Valid || old.Note != "" {
		t.Fatalf("upgrade corrupted existing data: %+v %v", old, err)
	}
	for i, nullable := range []decimal.NullDecimal{
		{}, {Decimal: decimal.Zero, Valid: true}, {Decimal: decimal.RequireFromString(precise), Valid: true},
	} {
		entry := &example.ContractSample{SampleKey: fmt.Sprintf("roundtrip-%d", i), Amount: decimal.RequireFromString(precise), OptionalAmount: nullable, Note: "manual"}
		result, err := model.Insert(ctx, entry)
		if err != nil {
			t.Fatal(err)
		}
		id, err := result.LastInsertId()
		if err != nil {
			t.Fatal(err)
		}
		actual, err := model.FindOne(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if !actual.Amount.Equal(entry.Amount) || actual.OptionalAmount.Valid != nullable.Valid ||
			(nullable.Valid && !actual.OptionalAmount.Decimal.Equal(nullable.Decimal)) {
			t.Fatalf("decimal roundtrip mismatch: %+v", actual)
		}
	}
	rollback := errors.New("expected rollback")
	conn := sqlx.NewSqlConnFromDB(upgraded)
	err = conn.TransactCtx(ctx, func(ctx context.Context, session sqlx.Session) error {
		_, err := model.WithSession(session).Insert(ctx, &example.ContractSample{SampleKey: "rolled-back", Amount: decimal.NewFromInt(1)})
		if err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	if _, err = model.FindOneBySampleKey(ctx, "rolled-back"); !errors.Is(err, example.ErrNotFound) {
		t.Fatalf("transaction extension committed independently: %v", err)
	}

	for _, fail := range []bool{false, true} {
		prefix := fmt.Sprintf("shared-%t-", fail)
		err = transaction.New(upgraded).TransactCtx(ctx, func(ctx context.Context, s sqlx.Session) error {
			for i := 0; i < 2; i++ {
				if _, e := model.WithSession(s).Insert(ctx, &example.ContractSample{SampleKey: fmt.Sprintf("%s%d", prefix, i), Amount: decimal.NewFromInt(1)}); e != nil {
					return e
				}
			}
			if fail {
				return rollback
			}
			return nil
		})
		if fail && !errors.Is(err, rollback) || !fail && err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 2; i++ {
			_, e := model.FindOneBySampleKey(ctx, fmt.Sprintf("%s%d", prefix, i))
			if fail && !errors.Is(e, example.ErrNotFound) || !fail && e != nil {
				t.Fatalf("shared transaction: %v", e)
			}
		}
	}
	// A failed migration must retain dirty state; never automatically force/down.
	failing := t.TempDir()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		contents, err := os.ReadFile(filepath.Join(directory, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(failing, entry.Name()), contents, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err = os.WriteFile(filepath.Join(failing, "000003_fail.up.sql"), []byte("CREATE TABLE partial_marker (id INT PRIMARY KEY); ALTER TABLE missing_contract_table ADD COLUMN note INT;"), 0600); err != nil {
		t.Fatal(err)
	}
	failure := open(dsn, name, failing)
	if err = failure.Up(); err == nil {
		t.Fatal("expected SQL failure")
	}
	v, isDirty, err := failure.Version()
	if err != nil || v != 3 || !isDirty {
		t.Fatalf("failure not preserved: %d %t %v", v, isDirty, err)
	}
	var count int
	if err = upgraded.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = 'partial_marker'").Scan(&count); err != nil || count != 1 {
		t.Fatalf("partial DDL must remain observable: %d %v", count, err)
	}
	var dirtyError migrate.ErrDirty
	if err = failure.Up(); !errors.As(err, &dirtyError) {
		t.Fatalf("dirty migration should block retry: %v", err)
	}
	if structure(upgraded) != structure(full) {
		t.Fatal("failure unexpectedly changed sample table")
	}
	t.Log("PASS: fresh/upgraded/full DDL equality; exact decimal and NULL/zero; transaction rollback; dirty failure retained")
}
