package chainassets

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/mr-tron/base58"
	tonaddress "github.com/xssnick/tonutils-go/address"
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
		return NormalizeBaseAddress(value)
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

// NormalizeBaseAddress accepts exact hex addresses and verifies mixed-case EIP-55 checksums.
func NormalizeBaseAddress(value string) (string, error) {
	if !strings.HasPrefix(value, "0x") || !common.IsHexAddress(value) {
		return "", fmt.Errorf("invalid EVM address")
	}
	body := value[2:]
	if body != strings.ToLower(body) && body != strings.ToUpper(body) && value != common.HexToAddress(value).Hex() {
		return "", fmt.Errorf("invalid EVM checksum")
	}
	return strings.ToLower(value), nil
}
