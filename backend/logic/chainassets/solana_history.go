package chainassets

import (
	"context"
	"encoding/json"
	"fmt"
)

const SolanaHistoryBatch = 12

// Newer transaction versions may share a block with supported legacy/v0
// transfers. Only the accounts projection is requested for discovery.
func (s *Service) ReadSolanaHistoryBlock(ctx context.Context, slot int64, headerOnly bool) (json.RawMessage, error) {
	if slot <= 0 {
		return nil, fmt.Errorf("invalid Solana history slot")
	}
	details := "accounts"
	if headerOnly {
		details = "none"
	}
	config := map[string]any{"commitment": "finalized", "encoding": "jsonParsed", "transactionDetails": details, "maxSupportedTransactionVersion": 1, "rewards": false}
	var block json.RawMessage
	err := s.devnetRPC(ctx, "getBlock", []any{slot, config}, &block)
	return block, err
}
func (s *Service) NextSolanaHistorySlots(ctx context.Context, after int64) ([]int64, error) {
	var slots []int64
	err := s.devnetRPC(ctx, "getBlocksWithLimit", []any{after + 1, SolanaHistoryBatch, map[string]string{"commitment": "finalized"}}, &slots)
	return slots, err
}
