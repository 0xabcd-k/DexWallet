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

func TestSolDepositOrderMigration(t *testing.T) {
	name, dsn, db, err := testRun.NewDatabase(context.Background(), "sol_order_migrate")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	m, err := migration.Open(dsn, name, "../../manifest/sql/diff/business", "business")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	for _, v := range []uint{37, 38, 37, 38} {
		if err = m.Migrate(v); err != nil {
			t.Fatal(v, err)
		}
	}
	const idx = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	mustExec(t, db, "INSERT INTO user(id,nickname) VALUES(1,'owner')")
	mustExec(t, db, "INSERT INTO deposit_quote_confirmation(quote_idx,user_id,snapshot_json,quote_expires_at) VALUES(?,1,'{}',UTC_TIMESTAMP()+INTERVAL 1 MINUTE)", idx)
	mustExec(t, db, "INSERT INTO sol_deposit_order(quote_idx,order_idx,message_base64,message_sha256,last_valid_height,review_after) VALUES(?,?, 'msg',?,10,UTC_TIMESTAMP()+INTERVAL 1 HOUR)", idx, idx, strings.Repeat("b", 64))
	if err = m.Migrate(37); err == nil || !strings.Contains(err.Error(), "sol_deposit_rollback_guard") {
		t.Fatal("unsafe rollback", err)
	}
	var saved string
	if err = db.QueryRow("SELECT order_idx FROM sol_deposit_order WHERE quote_idx=?", idx).Scan(&saved); err != nil || saved != idx {
		t.Fatal(saved, err)
	}
}

func TestSolExecutionMigration(t *testing.T) {
	name, dsn, db, err := testRun.NewDatabase(context.Background(), "sol_exec_migrate")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	m, err := migration.Open(dsn, name, "../../manifest/sql/diff/business", "business")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if err = m.Migrate(38); err != nil {
		t.Fatal(err)
	}
	idx := strings.Repeat("a", 32)
	mustExec(t, db, "INSERT INTO user(id,nickname) VALUES(1,'owner')")
	mustExec(t, db, "INSERT INTO deposit_quote_confirmation(quote_idx,user_id,snapshot_json,quote_expires_at) VALUES(?,1,'{}',UTC_TIMESTAMP()+INTERVAL 1 MINUTE)", idx)
	mustExec(t, db, "INSERT INTO sol_deposit_order(quote_idx,order_idx,message_base64,message_sha256,last_valid_height,review_after,user_signature,accepted_at) VALUES(?,?,'message',?,1,UTC_TIMESTAMP(),'user',UTC_TIMESTAMP())", idx, idx, strings.Repeat("b", 64))
	for _, v := range []uint{39, 38, 39} {
		if err = m.Migrate(v); err != nil {
			t.Fatal(v, err)
		}
	}
	var state string
	if err = db.QueryRow("SELECT execution_state FROM sol_deposit_order WHERE quote_idx=? AND tx_signature IS NULL", idx).Scan(&state); err != nil || state != "waiting" {
		t.Fatal("old accepted order", state, err)
	}
	mustExec(t, db, "UPDATE sol_deposit_order SET execution_state='signed',tx_signature='tx',sponsor_policy='devnet-test' WHERE quote_idx=?", idx)
	if err = m.Migrate(38); err == nil || !strings.Contains(err.Error(), "sol_execution_rollback_guard") {
		t.Fatal("lost execution history on downgrade", err)
	}
}
