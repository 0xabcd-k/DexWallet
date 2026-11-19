package chainassets

import (
	"context"
	"encoding/base64"
	"fmt"

	"github.com/mr-tron/base58"
)

type SolanaBlock struct {
	Blockhash            string `json:"blockhash"`
	LastValidBlockHeight int64  `json:"lastValidBlockHeight"`
}

func (s *Service) devnetRPC(ctx context.Context, method string, params any, result any) error {
	for _, n := range s.networks {
		if n.ID == SolanaDevnet && n.Enabled {
			ctx, cancel := context.WithTimeout(ctx, s.timeout)
			defer cancel()
			endpoint := s.endpoints[n.ID]
			if err := s.checkNetwork(ctx, n, endpoint); err != nil {
				return err
			}
			return s.rpc(ctx, n, endpoint, method, params, result)
		}
	}
	return fmt.Errorf("Solana Devnet endpoint required")
}
func (s *Service) LatestSolanaBlock(ctx context.Context) (SolanaBlock, error) {
	var response struct{ Value SolanaBlock }
	err := s.devnetRPC(ctx, "getLatestBlockhash", []any{map[string]string{"commitment": "confirmed"}}, &response)
	if err != nil {
		return SolanaBlock{}, err
	}
	raw, err := base58.Decode(response.Value.Blockhash)
	if err != nil || len(raw) != 32 || response.Value.LastValidBlockHeight <= 0 {
		return SolanaBlock{}, invalid("invalid latest Solana block")
	}
	return response.Value, nil
}
func (s *Service) SolanaBlockHeight(ctx context.Context) (int64, error) {
	var height int64
	err := s.devnetRPC(ctx, "getBlockHeight", []any{map[string]string{"commitment": "confirmed"}}, &height)
	if err == nil && height <= 0 {
		err = invalid("invalid Solana block height")
	}
	return height, err
}
func (s *Service) SubmitSolana(ctx context.Context, wire []byte) error {
	// The first signature is also the RPC acknowledgement. Only the single-signer
	// legacy transaction emitted by the payout executor is submitted here.
	if len(wire) <= 65 || len(wire) > 1232 || wire[0] != 1 {
		return invalid("invalid Solana transaction envelope")
	}
	var accepted string
	if err := s.devnetRPC(ctx, "sendTransaction", []any{base64.StdEncoding.EncodeToString(wire), map[string]any{"encoding": "base64", "skipPreflight": false, "preflightCommitment": "confirmed", "maxRetries": 0}}, &accepted); err != nil {
		return err
	}
	if accepted != base58.Encode(wire[1:65]) {
		return invalid("Solana transaction signature acknowledgement mismatch")
	}
	return nil
}
