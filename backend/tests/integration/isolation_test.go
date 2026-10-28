//go:build integration

package integration

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/internal/testenv"
	driver "github.com/go-sql-driver/mysql"
)

func TestHTTPAccountIsScoped(t *testing.T) {
	cfg, e := testenv.ValidateDSN(os.Getenv("DEXWALLET_TEST_HTTP_MYSQL_DSN"), "http")
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	name, _, db, e := testRun.NewDatabase(ctx, "scope")
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	if _, e = db.ExecContext(ctx, "CREATE TABLE guard_probe (id INT)"); e != nil {
		t.Fatal(e)
	}
	app, e := sql.Open("mysql", cfg.FormatDSN())
	if e != nil {
		t.Fatal(e)
	}
	defer app.Close()
	if e = app.PingContext(ctx); e != nil {
		t.Fatal(e)
	}
	_, e = app.ExecContext(ctx, "SELECT * FROM `"+name+"`.guard_probe")
	var mysqlError *driver.MySQLError
	if !errors.As(e, &mysqlError) || mysqlError.Number != 1142 {
		t.Fatalf("HTTP account can cross into contract schema, or unexpected error: %v", e)
	}
	t.Log("HTTP test account denied access to this run's contract table")
}

// A separate owner process is killed after recording/creating its resource.
// Recovery must delete only that run and leave our independent control DB intact.
func TestKilledRunRecovery(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	_, _, control, e := testRun.NewDatabase(ctx, "control")
	if e != nil {
		t.Fatal(e)
	}
	defer control.Close()
	childRun, e := testenv.NewRun()
	if e != nil {
		t.Fatal(e)
	}
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestResourceOwnerProcess$")
	cmd.Env = append(os.Environ(), "DEXWALLET_TEST_RUN_DIR="+childRun.Dir, "DEXWALLET_TEST_OWNER_HELPER=1")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if e = cmd.Start(); e != nil {
		t.Fatal(e)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		if e := testenv.Recover(childRun.Dir); e != nil {
			t.Error(e)
		}
	}()
	ready := filepath.Join(childRun.Dir, "ready")
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, e = os.Stat(ready); e == nil {
			break
		}
		select {
		case <-ctx.Done():
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			_ = testenv.Recover(childRun.Dir)
			t.Fatal("owner process did not become ready")
		case <-ticker.C:
		}
	}
	if e = testenv.Recover(childRun.Dir); e == nil {
		t.Fatal("cleanup raced active owner")
	}
	if e = cmd.Process.Kill(); e != nil {
		t.Fatal(e)
	}
	_ = cmd.Wait()
	if e = testenv.Recover(childRun.Dir); e != nil {
		t.Fatal(e)
	}
	cfg, e := testenv.AdminConfig()
	if e != nil {
		t.Fatal(e)
	}
	admin, e := sql.Open("mysql", cfg.FormatDSN())
	if e != nil {
		t.Fatal(e)
	}
	defer admin.Close()
	var count int
	if e = admin.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.schemata WHERE schema_name=?", childRun.Name("database", "owned")).Scan(&count); e != nil || count != 0 {
		t.Fatalf("owned database left behind: %d %v", count, e)
	}
	if _, e = control.ExecContext(ctx, "CREATE TABLE still_owned (id INT)"); e != nil {
		t.Fatalf("recovery changed another run: %v", e)
	}
	t.Log("killed owner recovered; active run and independent schema protected")
}
func TestResourceOwnerProcess(t *testing.T) {
	if os.Getenv("DEXWALLET_TEST_OWNER_HELPER") != "1" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _, db, e := testRun.NewDatabase(ctx, "owned")
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	if e = os.WriteFile(filepath.Join(testRun.Dir, "ready"), []byte("ready"), 0600); e != nil {
		t.Fatal(e)
	}
	time.Sleep(time.Minute) // Parent deliberately terminates this subprocess.
}
