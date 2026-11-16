package chainassets

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/mr-tron/base58"
	tonaddress "github.com/xssnick/tonutils-go/address"
	"golang.org/x/crypto/sha3"
)

func normalizeAddress(network Network, value string) (string, error) {
	if len(value) == 0 || len(value) > 128 || strings.TrimSpace(value) != value {
		return "", fmt.Errorf("invalid address length or whitespace")
	}
	switch network.Family {
	case TON:
		var a *tonaddress.Address
		var err error
		if strings.Contains(value, ":") {
			a, err = tonaddress.ParseRawAddr(value)
		} else {
			a, err = tonaddress.ParseAddr(value)
			if err == nil && a.IsTestnetOnly() && !network.Testnet {
				return "", fmt.Errorf("test-only TON address on mainnet")
			}
		}
		if err != nil || a.Type() != tonaddress.StdAddress || (a.Workchain() != 0 && a.Workchain() != -1) {
			return "", fmt.Errorf("invalid TON address")
		}
		return a.StringRaw(), nil
	case Base:
		if len(value) != 42 || !strings.HasPrefix(value, "0x") {
			return "", fmt.Errorf("invalid EVM address")
		}
		b, err := hex.DecodeString(value[2:])
		if err != nil || len(b) != 20 {
			return "", fmt.Errorf("invalid EVM address")
		}
		lower := strings.ToLower(value[2:])
		if value[2:] != lower && value[2:] != strings.ToUpper(lower) {
			h := sha3.NewLegacyKeccak256()
			_, _ = h.Write([]byte(lower))
			hash := hex.EncodeToString(h.Sum(nil))
			for i, c := range lower {
				if c >= 'a' && c <= 'f' && ((value[i+2] >= 'A' && value[i+2] <= 'F') != (hash[i] >= '8')) {
					return "", fmt.Errorf("invalid EVM checksum")
				}
			}
		}
		return "0x" + lower, nil
	case Solana:
		b, err := base58.Decode(value)
		if err != nil || len(b) != 32 {
			return "", fmt.Errorf("invalid Solana address")
		}
		return base58.Encode(b), nil
	case TRON:
		b, err := base58.Decode(value)
		if err != nil || len(b) != 25 || b[0] != 0x41 {
			return "", fmt.Errorf("invalid TRON address")
		}
		one := sha256.Sum256(b[:21])
		two := sha256.Sum256(one[:])
		if !bytes.Equal(b[21:], two[:4]) {
			return "", fmt.Errorf("invalid TRON checksum")
		}
		return value, nil
	}
	return "", fmt.Errorf("unsupported address family")
}
func sameAddress(network Network, a, b string) bool {
	x, e := normalizeAddress(network, a)
	y, f := normalizeAddress(network, b)
	return e == nil && f == nil && x == y
}
