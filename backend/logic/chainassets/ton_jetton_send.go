package chainassets

import (
	"context"
	"encoding/base64"
	"encoding/json"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func allowedTONJetton(n Network, master, owner string) (string, string, error) {
	m, err := normalizeAddress(n, master)
	if err != nil {
		return "", "", err
	}
	o, err := normalizeAddress(n, owner)
	if err != nil {
		return "", "", err
	}
	for _, a := range n.Assets {
		if a.Contract != "" && sameAddress(n, a.Contract, m) {
			return m, o, nil
		}
	}
	return "", "", invalid("Jetton master is not configured")
}

// Inputs have already crossed normalizeAddress at the RPC boundary.
func tonWalletQuery(master, owner string) any {
	a, _ := address.ParseRawAddr(owner)
	b := cell.BeginCell().MustStoreAddr(a).EndCell()
	return map[string]any{"address": master, "method": "get_wallet_address", "stack": []any{map[string]string{"type": "slice", "value": base64.StdEncoding.EncodeToString(b.ToBOC())}}}
}

// ResolveTONJettonWallet derives the platform's token account from the configured
// testnet master. A submitted token-wallet address is never trusted on its own.
func (s *Service) ResolveTONJettonWallet(ctx context.Context, master, owner string) (string, error) {
	n, e, err := s.tonTestnet()
	if err != nil {
		return "", err
	}
	master, owner, err = allowedTONJetton(n, master, owner)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	read := s.tonReader(ctx, n, e)
	var raw json.RawMessage
	if err = read("/api/v2/getMasterchainInfo", nil, nil, &raw); err != nil {
		return "", err
	}
	if err = checkTONTestnet(raw); err != nil {
		return "", err
	}
	if err = read("/api/v3/runGetMethod", nil, tonWalletQuery(master, owner), &raw); err != nil {
		return "", err
	}
	var r struct {
		ExitCode *int `json:"exit_code"`
		Stack    []struct {
			Type  string `json:"type"`
			Value string `json:"value"`
		} `json:"stack"`
	}
	if json.Unmarshal(raw, &r) != nil || r.ExitCode == nil || *r.ExitCode != 0 || len(r.Stack) != 1 || (r.Stack[0].Type != "cell" && r.Stack[0].Type != "slice") {
		return "", invalid("invalid Jetton wallet derivation")
	}
	b, err := base64.StdEncoding.DecodeString(r.Stack[0].Value)
	if err != nil {
		return "", invalid("invalid Jetton wallet encoding")
	}
	roots, err := cell.FromBOCMultiRoot(b)
	if err != nil || len(roots) != 1 || roots[0].GetType() != cell.OrdinaryCellType {
		return "", invalid("invalid Jetton wallet cell")
	}
	slice := roots[0].BeginParse()
	a, err := slice.LoadAddr()
	if err != nil || a == nil || a.Type() != address.StdAddress || a.Workchain() != 0 || slice.BitsLeft() != 0 || slice.RefsNum() != 0 {
		return "", invalid("invalid derived Jetton wallet address")
	}
	return a.StringRaw(), nil
}
