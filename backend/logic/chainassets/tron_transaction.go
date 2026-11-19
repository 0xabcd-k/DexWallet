package chainassets

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

// Genesis block IDs distinguish networks which share the same address format.
const TRONMainnetGenesis = "00000000000000001ebf88508a03865c71d452e25f4d51194196a1d22b6653dc"
const TRONNileGenesis = "0000000000000000d698d4192c56cb6be724a558448e2684802de4d6cd8690dc"

type TRONEvidence struct {
	Genesis     json.RawMessage `json:"genesis"`
	Transaction json.RawMessage `json:"transaction"`
	Info        json.RawMessage `json:"info"`
	SolidHead   json.RawMessage `json:"solidHead"`
	Block       json.RawMessage `json:"block"`
}

// ReadTRONEvidence uses only the configured Solidity API: a full-node pending
// transaction is never substituted for a solidified receipt.
func (s *Service) ReadTRONEvidence(ctx context.Context, network, hash string) (TRONEvidence, error) {
	var out TRONEvidence
	if len(hash) != 64 {
		return out, fmt.Errorf("invalid TRON transaction ID")
	}
	if _, err := hex.DecodeString(hash); err != nil {
		return out, fmt.Errorf("invalid TRON transaction ID")
	}
	var n Network
	for _, candidate := range s.networks {
		if candidate.ID == network && candidate.Family == TRON {
			n = candidate
			break
		}
	}
	if n.ID == "" || !n.Enabled {
		return out, fmt.Errorf("TRON receipt network %s not enabled", network)
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	e := s.endpoints[network]
	read := func(method string, body map[string]any, target *json.RawMessage) error {
		body["visible"] = false // RPC addresses must be hex, not Base58.
		if err := s.request(ctx, n, e, "/walletsolidity/"+method, nil, body, target); err != nil {
			return err
		}
		var object map[string]json.RawMessage
		if json.Unmarshal(*target, &object) != nil || object == nil {
			return invalid("invalid TRON " + method + " object")
		}
		if object["Error"] != nil || object["error"] != nil {
			return invalid("TRON " + method + " returned an error")
		}
		return nil
	}
	if err := read("getblockbynum", map[string]any{"num": 0}, &out.Genesis); err != nil {
		return out, err
	}
	var genesis struct{ BlockID string }
	_ = json.Unmarshal(out.Genesis, &genesis)
	want := TRONMainnetGenesis
	if network == TRONNile {
		want = TRONNileGenesis
	}
	if genesis.BlockID != want {
		return out, invalid("TRON genesis does not match configured network")
	}
	query := map[string]any{"value": strings.ToLower(hash)}
	if err := read("gettransactionbyid", query, &out.Transaction); err != nil {
		return out, err
	}
	if err := read("gettransactioninfobyid", query, &out.Info); err != nil {
		return out, err
	}
	var info map[string]json.RawMessage
	_ = json.Unmarshal(out.Info, &info)
	if len(info) == 0 {
		return out, nil // {} is the documented not-found response.
	}
	var height int64
	if json.Unmarshal(info["blockNumber"], &height) != nil || height <= 0 {
		return out, invalid("invalid TRON inclusion height")
	}
	if err := read("getnowblock", map[string]any{}, &out.SolidHead); err != nil {
		return out, err
	}
	err := read("getblockbynum", map[string]any{"num": height}, &out.Block)
	return out, err
}
