//go:build integration

package integration

import (
	"context"
	"strings"
	"testing"

	"github.com/0xabcd-k/DexWallet/backend/internal/migration"
)

func TestDepositQuoteMigration(t *testing.T) {
	name, dsn, db, err := testRun.NewDatabase(context.Background(), "quote_migrate")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	m, err := migration.Open(dsn, name, "../../manifest/sql/diff/business", "business")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	for _, version := range []uint{36, 37, 36, 37} {
		if err := m.Migrate(version); err != nil {
			t.Fatal(version, err)
		}
	}
	mustExec(t, db, "INSERT INTO user(id,nickname) VALUES(1,'quote-owner')")
	const idx = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	mustExec(t, db, "INSERT INTO deposit_quote_confirmation(quote_idx,user_id,snapshot_json,quote_expires_at) VALUES(?,1,'{}',CURRENT_TIMESTAMP(3)+INTERVAL 1 MINUTE)", idx)
	if err := m.Migrate(36); err == nil || !strings.Contains(err.Error(), "deposit_quote_rollback_guard") {
		t.Fatal("unsafe downgrade", err)
	}
	var owner int64
	if err := db.QueryRow("SELECT user_id FROM deposit_quote_confirmation WHERE quote_idx=?", idx).Scan(&owner); err != nil || owner != 1 {
		t.Fatal(owner, err)
	}
}
