//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/internal/migration"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainreceipt"
)

func TestReceiveAddressInventoryCLI(t *testing.T) {
	ctx := context.Background()
	name, dsn, db, err := testRun.NewDatabase(ctx, "receive_inventory_cli")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	m, err := migration.Open(dsn, name, "../../manifest/sql/diff/business", "business")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if err := m.Up(); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	binary := filepath.Join(dir, "chain_receive")
	build := exec.Command("go", "build", "-o", binary, "./tools/chain_receive")
	build.Dir = "../.."
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	const network = chainassets.TONTestnet
	t.Setenv("DEXWALLET_MYSQL_DSN", dsn)
	t.Setenv("DEXWALLET_CHAIN_TON_TESTNET_RPC_URL", "http://127.0.0.1:1")
	command := func(args ...string) ([]byte, error) {
		return exec.CommandContext(ctx, binary, args...).CombinedOutput()
	}
	run := func(dest any, args ...string) {
		t.Helper()
		out, err := command(args...)
		if err != nil {
			t.Fatalf("%v: %v %s", args, err, out)
		}
		if dest != nil {
			if err := json.Unmarshal(out, dest); err != nil {
				t.Fatalf("%v: %v %s", args, err, out)
			}
		}
	}
	reject := func(want string, args ...string) {
		t.Helper()
		out, err := command(args...)
		if err == nil || !strings.Contains(string(out), want) {
			t.Fatalf("%v: want %q, got %v %s", args, want, err, out)
		}
	}
	stock := func(net string, total, allocated int64) {
		t.Helper()
		var got chainreceipt.ReceiveAddressInventory
		run(&got, "-action", "inventory", "-network", net)
		want := chainreceipt.ReceiveAddressInventory{Network: net, Total: total, Allocated: allocated, Available: total - allocated}
		if got != want {
			t.Fatalf("stock %+v, want %+v", got, want)
		}
	}
	writeList := func(file string, contents ...string) []string {
		t.Helper()
		path := filepath.Join(dir, file)
		if err := os.WriteFile(path, []byte(strings.Join(contents, "\r\n")+"\r\n"), 0600); err != nil {
			t.Fatal(err)
		}
		return []string{"-action", "import", "-network", network, "-file", path}
	}
	owner := func(i int) string { return fmt.Sprintf("0:%064x", i) }
	shared := owner(255)
	run(nil, "-action", "register", "-network", network, "-recipient", shared, "-route", chainreceipt.TONMemo)
	stock(network, 0, 0)
	reject(chainreceipt.ErrInvalidRequest.Error(), writeList("empty", "", " ")...)
	reject("address entry 2", writeList("invalid", owner(1), "bad-address")...)
	reject(chainreceipt.ErrReceiveConflict.Error(), writeList("route-conflict", owner(1), shared)...)
	stock(network, 0, 0)

	batch := writeList("owners", " "+owner(10)+" ", "", owner(11), "0:"+strings.ToUpper(owner(10)[2:]))
	var imported chainreceipt.ReceiveAddressImport
	run(&imported, batch...)
	if imported.Network != network || imported.Added != 2 || imported.Existing != 0 {
		t.Fatalf("import %+v", imported)
	}
	open := []string{"-action", "open", "-network", network, "-purpose", "deposit", "-key", "inventory-test",
		"-beneficiary", "17", "-payer", owner(90), "-asset", network + ":ton", "-atoms", "1000",
		"-expires", strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10)}
	var order chainreceipt.ReceiveRequest
	run(&order, open...)
	if order.Recipient != owner(10) && order.Recipient != owner(11) {
		t.Fatalf("unexpected allocated address %s", order.Recipient)
	}
	stock(network, 2, 1)
	mustExec(t, db, "UPDATE chain_receive_request SET opens_at=1, expires_at=2 WHERE request_key=?", order.Key)
	run(&imported, batch...)
	if imported.Added != 0 || imported.Existing != 2 {
		t.Fatalf("reimport %+v", imported)
	}
	stock(network, 2, 1)
	stock(chainassets.BaseSepolia, 0, 0)
	reject(chainreceipt.ErrInvalidRequest.Error(), "-action", "inventory", "-network", chainassets.TONMainnet)

	// Two operators import overlapping files in opposite order. Each owner
	// must be counted as new by exactly one committed transaction.
	left := writeList("left", owner(20), owner(21), owner(22))
	right := writeList("right", owner(23), owner(22), owner(21))
	start := make(chan struct{})
	type result struct {
		out []byte
		err error
	}
	results := make(chan result, 2)
	for _, args := range [][]string{left, right} {
		go func() {
			<-start
			out, err := command(args...)
			results <- result{out, err}
		}()
	}
	close(start)
	var added, existing int
	for range 2 {
		r := <-results
		if r.err != nil {
			t.Fatalf("concurrent import: %v %s", r.err, r.out)
		}
		if err := json.Unmarshal(r.out, &imported); err != nil {
			t.Fatal(err)
		}
		added += imported.Added
		existing += imported.Existing
	}
	if added != 4 || existing != 2 {
		t.Fatalf("overlap added=%d existing=%d", added, existing)
	}
	stock(network, 6, 1)
	t.Setenv("DEXWALLET_CHAIN_TON_TESTNET_RPC_URL", "")
	stock(network, 6, 1)
	reject(chainreceipt.ErrInvalidRequest.Error(), batch...)
}
