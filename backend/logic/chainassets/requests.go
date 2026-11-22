package chainassets

import (
	"fmt"
	"math/big"
	"net/url"
	"strings"

	"github.com/0xabcd-k/DexWallet/backend/apperror"
	tonaddress "github.com/xssnick/tonutils-go/address"
)

const ReceiveScheme = "com.dexwallet.app"
const ReceiveHost = "chain-transfer"
const ReceiveVersion = "1"
const ChannelNotOpen = "CHANNEL_NOT_OPEN"
const maxReceiveLength = 2048

type TargetInput struct{ Network, AssetID, Address, ReceiveCode string }
type Target struct{ Network, AssetID, Address, ReceiveCode string }
type Preview struct {
	Target      Target
	Amount      string
	CanSubmit   bool
	BlockReason string
}

func badTarget(detail string) error {
	return apperror.New(apperror.InvalidArgument, fmt.Errorf("chain target: %s", detail))
}

// A receive code describes a public destination, never ownership or authorization.
// No network call, order, lock, fee reservation or signature is made here.
func (s *Service) ResolveTarget(input TargetInput) (Target, error) {
	if input.ReceiveCode != "" {
		if input.Network != "" || input.AssetID != "" || input.Address != "" {
			return Target{}, badTarget("mixed receive code and fields")
		}
		if len(input.ReceiveCode) > maxReceiveLength || strings.TrimSpace(input.ReceiveCode) != input.ReceiveCode {
			return Target{}, badTarget("invalid code length")
		}
		u, err := url.Parse(input.ReceiveCode)
		if err != nil || u.Scheme != ReceiveScheme || u.Host != ReceiveHost || u.User != nil || u.Path != "" || u.Opaque != "" || u.Fragment != "" || strings.Contains(input.ReceiveCode, "#") {
			return Target{}, badTarget("unsupported receive URI")
		}
		q, err := url.ParseQuery(u.RawQuery)
		if err != nil || len(q) != 4 {
			return Target{}, badTarget("invalid receive parameters")
		}
		for _, key := range []string{"v", "network", "asset", "address"} {
			if len(q[key]) != 1 || q.Get(key) == "" {
				return Target{}, badTarget("missing or repeated parameter")
			}
		}
		if q.Get("v") != ReceiveVersion {
			return Target{}, badTarget("unsupported version")
		}
		input = TargetInput{Network: q.Get("network"), AssetID: q.Get("asset"), Address: q.Get("address")}
	}
	var network Network
	var chosen Asset
	for _, n := range s.networks {
		if n.ID == input.Network {
			network = n
			break
		}
	}
	if network.ID == "" {
		return Target{}, badTarget("unknown network")
	}
	for _, a := range network.Assets {
		if a.ID == input.AssetID {
			chosen = a
			break
		}
	}
	if chosen.ID == "" {
		return Target{}, badTarget("unsupported asset on network")
	}
	address, err := normalizeAddress(network, input.Address)
	if err != nil {
		return Target{}, badTarget(err.Error())
	}
	if network.Family == TON {
		parsed, _ := tonaddress.ParseRawAddr(address)
		address = parsed.Bounce(false).Testnet(network.Testnet).String()
	}
	q := url.Values{"v": {ReceiveVersion}, "network": {network.ID}, "asset": {chosen.ID}, "address": {address}}
	uri := url.URL{Scheme: ReceiveScheme, Host: ReceiveHost, RawQuery: q.Encode()}
	return Target{Network: network.ID, AssetID: chosen.ID, Address: address, ReceiveCode: uri.String()}, nil
}
func (s *Service) PreviewTransfer(input TargetInput, raw string) (Preview, error) {
	target, err := s.ResolveTarget(input)
	if err != nil {
		return Preview{}, err
	}
	var network Network
	var asset Asset
	for _, n := range s.networks {
		if n.ID == target.Network {
			network = n
			for _, a := range n.Assets {
				if a.ID == target.AssetID {
					asset = a
					break
				}
			}
			break
		}
	}
	quantity, err := chainQuantity(network, asset, raw)
	if err != nil {
		return Preview{}, err
	}
	return Preview{Target: target, Amount: quantity, CanSubmit: false, BlockReason: ChannelNotOpen}, nil
}

func chainQuantity(network Network, asset Asset, raw string) (string, error) {
	// Inputs are decimal quantities, never exponents, floats or rounded values.
	if len(raw) == 0 || len(raw) > 100 || strings.TrimSpace(raw) != raw {
		return "", badTarget("invalid amount")
	}
	parts := strings.Split(raw, ".")
	if len(parts) > 2 || !integerPattern.MatchString(parts[0]) {
		return "", badTarget("invalid amount")
	}
	fraction := ""
	if len(parts) == 2 {
		fraction = parts[1]
		if len(fraction) == 0 || len(fraction) > asset.Decimals {
			return "", badTarget("amount precision")
		}
		for _, c := range fraction {
			if c < '0' || c > '9' {
				return "", badTarget("invalid fraction")
			}
		}
	}
	integer, ok := new(big.Int).SetString(parts[0]+fraction+strings.Repeat("0", asset.Decimals-len(fraction)), 10)
	if !ok || integer.Sign() <= 0 {
		return "", badTarget("amount must be positive")
	}
	bits := 256
	switch network.Family {
	case TON:
		bits = 120
	case Solana:
		bits = 64
	case TRON:
		if asset.Origin == Native {
			bits = 63
		}
	}
	if integer.BitLen() > bits {
		return "", badTarget("amount exceeds chain integer range")
	}
	return amount(integer, asset.Decimals), nil
}
