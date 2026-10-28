package testenv

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRejectUnsafeTargetsBeforeAnyConnection(t *testing.T) {
	t.Setenv("DEXWALLET_TEST_ENV", "local")
	t.Setenv("DEXWALLET_TEST_MYSQL_ADDRESS", "127.0.0.1:3306")
	good := "dexwallet_test_admin:secret@tcp(127.0.0.1:3306)/"
	if _, e := ValidateDSN(good, "admin"); e != nil {
		t.Fatal(e)
	}
	for _, dsn := range []string{"", "root:secret@tcp(127.0.0.1:3306)/", "dexwallet_test_admin:secret@tcp(192.0.2.1:3306)/", "dexwallet_test_admin:secret@tcp(localhost:3306)/", good + "production", "dexwallet_test_http:secret@tcp(127.0.0.1:3306)/production"} {
		if _, e := ValidateDSN(dsn, "admin"); e == nil {
			t.Fatalf("unsafe test target accepted")
		}
	}
	if _, e := ValidateDSN("dexwallet_test_http:secret@tcp(127.0.0.1:3306)/dexwallet_test_http", "http"); e != nil {
		t.Fatal(e)
	}
	if _, e := ValidateDSN(good, "http"); e == nil {
		t.Fatal("admin accepted for HTTP")
	}
	t.Setenv("DEXWALLET_TEST_ENV", "production")
	if _, e := ValidateDSN(good, "admin"); e == nil {
		t.Fatal("production accepted")
	}
}
func TestManifestRejectsCrossRunCleanup(t *testing.T) {
	t.Setenv("DEXWALLET_TEST_ENV", "local")
	t.Setenv("DEXWALLET_TEST_MYSQL_ADDRESS", "127.0.0.1:3306")
	t.Setenv("DEXWALLET_TEST_MYSQL_DSN", "dexwallet_test_admin:secret@tcp(127.0.0.1:3306)/")
	id := "0123456789abcdef"
	dir := filepath.Join(t.TempDir(), id)
	if e := os.Mkdir(dir, 0700); e != nil {
		t.Fatal(e)
	}
	r := &Run{ID: id, Address: "127.0.0.1:3306", Dir: dir}
	if e := r.Record("database", "dexwallet_dev"); e == nil {
		t.Fatal("external database accepted")
	}
	if e := r.Record("database", r.Name("database", "sample")); e != nil {
		t.Fatal(e)
	}
	loaded, e := LoadRun(dir)
	if e != nil || len(loaded.Resources) != 1 {
		t.Fatal(e)
	}
	r.Resources[0].Name = "dexwallet_contract_ffffffffffffffff_sample"
	if e = r.save(); e != nil {
		t.Fatal(e)
	}
	if _, e = LoadRun(dir); e == nil {
		t.Fatal("cross-run cleanup accepted")
	}
}

func TestCleanupFailureRetainsExactResourcesAndCanResume(t *testing.T) {
	t.Setenv("DEXWALLET_TEST_ENV", "local")
	t.Setenv("DEXWALLET_TEST_MYSQL_ADDRESS", "127.0.0.1:3306")
	t.Setenv("DEXWALLET_TEST_MYSQL_DSN", "dexwallet_test_admin:secret@tcp(127.0.0.1:3306)/")
	id := "0123456789abcdef"
	dir := filepath.Join(t.TempDir(), id)
	if e := os.Mkdir(dir, 0700); e != nil {
		t.Fatal(e)
	}
	r := &Run{ID: id, Address: "127.0.0.1:3306", Dir: dir}
	for _, name := range []string{"one", "two"} {
		if e := r.Record("database", r.Name("database", name)); e != nil {
			t.Fatal(e)
		}
	}
	failure := os.ErrPermission
	if e := r.cleanupWith(func(v Resource) error {
		if v.Name == r.Name("database", "one") {
			return failure
		}
		return nil
	}); e == nil {
		t.Fatal("cleanup failure hidden")
	}
	restored, e := LoadRun(dir)
	if e != nil || len(restored.Resources) != 1 {
		t.Fatalf("lost recovery record: %v", e)
	}
	if e = restored.cleanupWith(func(v Resource) error {
		if v.Name != r.Name("database", "one") {
			t.Fatal("wrong recovery target")
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	restored, e = LoadRun(dir)
	if e != nil || len(restored.Resources) != 0 {
		t.Fatal("recovery not persisted")
	}
	lock, e := Lock(dir)
	if e != nil {
		t.Fatal(e)
	}
	if e = Recover(dir); e == nil {
		t.Fatal("active run cleanup allowed")
	}
	lock.Close()
}
