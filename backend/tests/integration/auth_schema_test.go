//go:build integration

package integration

import (
	"context"
	"os"
	"testing"

	"github.com/0xabcd-k/DexWallet/backend/internal/migration"
)

func TestAuthSchemaUpgrade(t *testing.T) {
	ctx := context.Background()
	name, dsn, db, err := testRun.NewDatabase(ctx, "auth_upgrade")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	m, err := migration.Open(dsn, name, "../../manifest/sql/diff/business", "business")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if err = m.Steps(1); err != nil {
		t.Fatal(err)
	}
	mustExec(t, db, "INSERT INTO `user`(id,nickname) VALUES(1,'existing')")
	mustExec(t, db, "INSERT INTO email_identity(email_address,email_local_part,email_domain,email_original,user_id,binding_version) VALUES('old@example.com','old','example.com','old@example.com',1,3)")
	if err = m.Migrate(4); err != nil {
		t.Fatal(err)
	}
	var nickname string
	var version int
	if err = db.QueryRow("SELECT nickname,binding_version FROM `user` JOIN email_identity ON user_id=`user`.id WHERE `user`.id=1").Scan(&nickname, &version); err != nil || nickname != "existing" || version != 3 {
		t.Fatalf("upgrade changed identity: %s %d %v", nickname, version, err)
	}
	for _, table := range []string{"email_challenge", "auth_operation", "backoffice_challenge"} {
		var count int
		if err = db.QueryRow("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE() AND table_name=?", table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("retired table %s remains: %d %v", table, count, err)
		}
	}
	_, _, full, err := testRun.NewDatabase(ctx, "auth_full")
	if err != nil {
		t.Fatal(err)
	}
	defer full.Close()
	ddl, err := os.ReadFile("../../manifest/sql/ddl/backoffice/tables.sql")
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, full, string(ddl))
	for _, table := range []string{"admin_user", "merchant_user", "backoffice_audit"} {
		var name, a, b string
		if err = db.QueryRow("SHOW CREATE TABLE "+table).Scan(&name, &a); err != nil {
			t.Fatal(err)
		}
		if err = full.QueryRow("SHOW CREATE TABLE "+table).Scan(&name, &b); err != nil {
			t.Fatal(err)
		}
		if a != b {
			t.Fatalf("auth DDL differs from upgrade: %s", table)
		}
	}
	// Rollback restores empty disposable tables without touching existing identities.
	if err = m.Steps(-1); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"email_challenge", "auth_operation", "backoffice_challenge"} {
		var count int
		if err = db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("rollback %s: %d %v", table, count, err)
		}
	}
	if err = m.Migrate(4); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow("SELECT nickname FROM user WHERE id=1").Scan(&nickname); err != nil || nickname != "existing" {
		t.Fatal("migration round trip changed user", err)
	}

}
