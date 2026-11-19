// ton_test_signer is a separate, single-intent TESTNET signing process.
package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/0xabcd-k/DexWallet/backend/logic/chainsend"
)

func run() error {
	policyPath := flag.String("policy", "", "operator-pinned test intent JSON")
	seedPath := flag.String("seed-file", "", "private 0600 file with a test-only 32-byte hex seed")
	flag.Parse()
	info, err := os.Stat(*seedPath)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 256 {
		return errors.New("test seed must be a small private regular file")
	}
	raw, err := os.ReadFile(*seedPath)
	if err != nil {
		return err
	}
	seed, err := hex.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil || len(seed) != ed25519.SeedSize {
		return errors.New("invalid test seed (value omitted)")
	}
	raw, err = os.ReadFile(*policyPath)
	if err != nil {
		return err
	}
	var p chainsend.TestPolicy
	if err = json.Unmarshal(raw, &p); err != nil {
		return err
	}
	raw, err = io.ReadAll(io.LimitReader(os.Stdin, 16385))
	if err != nil {
		return err
	}
	if len(raw) > 16384 {
		return errors.New("signing request too large")
	}
	var request chainsend.SigningRequest
	if err = json.Unmarshal(raw, &request); err != nil {
		return err
	}
	sig, err := chainsend.SignTestnet(request, p, ed25519.NewKeyFromSeed(seed))
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(chainsend.SignatureResponse{Signature: base64.StdEncoding.EncodeToString(sig)})
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
