package migration

import "testing"

func TestRejectMissingOrMismatchedTargetBeforeConnecting(t *testing.T) {
	for _, tc := range []struct{ dsn, database, dir string }{
		{"", "dexwallet_contract_test", "."},
		{"root@tcp(127.0.0.1:1)/wrong", "dexwallet_contract_test", "."},
		{"root@tcp(127.0.0.1:1)/dexwallet_contract_test", "", "."},
	} {
		if m, err := Open(tc.dsn, tc.database, tc.dir, "examples"); err == nil {
			m.Close()
			t.Fatal("expected target validation failure")
		}
	}
}
