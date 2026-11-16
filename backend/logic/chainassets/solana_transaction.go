package chainassets

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mr-tron/base58"
)

type SolanaEvidence struct {
	Transaction json.RawMessage `json:"transaction"`
	Status      json.RawMessage `json:"status"`
}

// Internal read-only observer. No arbitrary endpoint/method or public API is
// exposed. Callers still verify the instruction, asset, recipient and amount.
func (s *Service) ReadSolanaEvidence(ctx context.Context, network, signature string) (SolanaEvidence, error) {
	var result SolanaEvidence
	if len(signature) > 88 {
		return result, fmt.Errorf("invalid Solana signature")
	}
	raw, err := base58.Decode(signature)
	if err != nil || len(raw) != 64 {
		return result, fmt.Errorf("invalid Solana signature")
	}
	var n Network
	for _, item := range s.networks {
		if item.ID == network && item.Family == Solana {
			n = item
			break
		}
	}
	if n.ID == "" || !n.Enabled {
		return result, fmt.Errorf("Solana receipt network %s not enabled", network)
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	e := s.endpoints[network]
	if err = s.checkNetwork(ctx, n, e); err != nil {
		return result, err
	}
	result.Transaction, err = s.rpcRaw(ctx, n, e, "getTransaction", []any{signature, map[string]any{"encoding": "jsonParsed", "commitment": "finalized", "maxSupportedTransactionVersion": 0}})
	if err != nil {
		return result, err
	}
	result.Status, err = s.rpcRaw(ctx, n, e, "getSignatureStatuses", []any{[]string{signature}, map[string]bool{"searchTransactionHistory": true}})
	return result, err
}
