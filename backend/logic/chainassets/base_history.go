package chainassets

import (
	"context"
	"encoding/json"
	"fmt"
)

func (s *Service) ReadBaseHistoryHead(ctx context.Context) (json.RawMessage, error) {
	return s.baseHistoryRPC(ctx, "eth_getBlockByNumber", []any{"finalized", false})
}

func (s *Service) ReadBaseHistoryBlock(ctx context.Context, height int64) (json.RawMessage, error) {
	return s.baseHistoryRPC(ctx, "eth_getBlockByNumber", []any{fmt.Sprintf("0x%x", height), false})
}

func (s *Service) ReadBaseBlockReceipts(ctx context.Context, height int64) (json.RawMessage, error) {
	return s.baseHistoryRPC(ctx, "eth_getBlockReceipts", []any{fmt.Sprintf("0x%x", height)})
}

func (s *Service) ReadBaseBlockCalls(ctx context.Context, hash string) (json.RawMessage, error) {
	return s.baseHistoryRPC(ctx, "debug_traceBlockByHash", []any{hash, map[string]any{
		"tracer": "callTracer", "timeout": "15s", "tracerConfig": map[string]bool{"onlyTopCall": false},
	}})
}

func (s *Service) baseHistoryRPC(ctx context.Context, method string, args []any) (json.RawMessage, error) {
	var raw json.RawMessage
	err := s.baseSepoliaRPC(ctx, method, args, &raw)
	return raw, err
}
