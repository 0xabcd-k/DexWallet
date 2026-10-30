//go:build integration

package integration

import (
	"fmt"
	"os"
	"testing"

	"github.com/0xabcd-k/DexWallet/backend/internal/testenv"
)

var testRun *testenv.Run

const businessSchemaVersion = 6

func TestMain(m *testing.M) {
	var err error
	if dir := os.Getenv("DEXWALLET_TEST_RUN_DIR"); dir != "" {
		testRun, err = testenv.LoadRun(dir)
	} else {
		testRun, err = testenv.NewRun()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	lock, err := testenv.Lock(testRun.Dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("Resource journal:", testRun.Dir)
	code := m.Run()
	if err = testRun.Cleanup(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		code = 1
	}
	lock.Close()
	os.Exit(code)
}
