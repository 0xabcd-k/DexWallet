package chainassets

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

const BaseGasPriceOracle = "0x420000000000000000000000000000000000000f"

type baseTraceMode uint8

const (
	baseNoTrace baseTraceMode = iota
	baseFullTrace
	baseFinalDepositTrace
)

type BaseEvidence struct {
	Transaction json.RawMessage `json:"transaction"`
	Receipt     json.RawMessage `json:"receipt"`
	Finalized   json.RawMessage `json:"finalized"`
	Block       json.RawMessage `json:"block"`
	OperatorFee json.RawMessage `json:"operatorFee"`
	CallTrace   json.RawMessage `json:"callTrace,omitempty"`
}

// Fixed, configured Base endpoints only. The caller still verifies finality,
// transfer identity and fees; no client can supply these facts.
func (s *Service) ReadBaseEvidence(ctx context.Context, network, hash string) (BaseEvidence, error) {
	return s.readBaseEvidence(ctx, network, hash, baseNoTrace)
}

// ReadBaseCallEvidence additionally requires a complete callTracer tree from the same node.
func (s *Service) ReadBaseCallEvidence(ctx context.Context, network, hash string) (BaseEvidence, error) {
	return s.readBaseEvidence(ctx, network, hash, baseFullTrace)
}

// Native deposits only need the trace for a successful, finalized inclusion.
func (s *Service) ReadBaseDepositETHEvidence(ctx context.Context, network, hash string) (BaseEvidence, error) {
	return s.readBaseEvidence(ctx, network, hash, baseFinalDepositTrace)
}

func (s *Service) readBaseEvidence(ctx context.Context, network, hash string, trace baseTraceMode) (BaseEvidence, error) {
	var out BaseEvidence
	if len(hash) != 66 || !strings.HasPrefix(hash, "0x") {
		return out, fmt.Errorf("invalid Base transaction hash")
	}
	if _, err := hex.DecodeString(hash[2:]); err != nil {
		return out, fmt.Errorf("invalid Base transaction hash")
	}
	var n Network
	for _, candidate := range s.networks {
		if candidate.ID == network && candidate.Family == Base {
			n = candidate
			break
		}
	}
	if n.ID == "" || !n.Enabled {
		return out, fmt.Errorf("Base receipt network %s not enabled", network)
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	e := s.endpoints[network]
	if err := s.checkNetwork(ctx, n, e); err != nil {
		return out, err
	}
	var err error
	out.Transaction, err = s.rpcRaw(ctx, n, e, "eth_getTransactionByHash", []any{hash})
	if err != nil {
		return out, err
	}
	out.Receipt, err = s.rpcRaw(ctx, n, e, "eth_getTransactionReceipt", []any{hash})
	if err != nil {
		return out, err
	}
	if bytes.Equal(bytes.TrimSpace(out.Transaction), []byte("null")) || bytes.Equal(bytes.TrimSpace(out.Receipt), []byte("null")) {
		return out, nil
	}
	var receipt struct{ BlockHash, BlockNumber, GasUsed, Status string }
	if json.Unmarshal(out.Receipt, &receipt) != nil || len(receipt.BlockHash) != 66 || !strings.HasPrefix(receipt.BlockHash, "0x") {
		return out, invalid("missing receipt block")
	}
	if _, err := hex.DecodeString(receipt.BlockHash[2:]); err != nil {
		return out, invalid("invalid receipt block hash")
	}
	height, err := hexInteger(receipt.BlockNumber, false)
	if err != nil {
		return out, err
	}
	gas, err := hexInteger(receipt.GasUsed, false)
	if err != nil {
		return out, err
	}
	out.Finalized, err = s.rpcRaw(ctx, n, e, "eth_getBlockByNumber", []any{"finalized", false})
	if err != nil {
		return out, err
	}
	out.Block, err = s.rpcRaw(ctx, n, e, "eth_getBlockByNumber", []any{receipt.BlockNumber, false})
	if err != nil {
		return out, err
	}
	if trace == baseFinalDepositTrace {
		var head struct{ Number string }
		if bytes.Equal(bytes.TrimSpace(out.Finalized), []byte("null")) || receipt.Status != "0x1" {
			return out, nil
		}
		if err := json.Unmarshal(out.Finalized, &head); err != nil {
			return out, err
		}
		final, err := hexInteger(head.Number, false)
		if err != nil {
			return out, err
		}
		if final.Cmp(height) < 0 {
			return out, nil
		}
	}
	data := baseOracleCall("getOperatorFee(uint256)", gas)
	// Historical rules at the exact canonical block, never today's price.
	out.OperatorFee, err = s.rpcRaw(ctx, n, e, "eth_call", []any{map[string]string{"to": BaseGasPriceOracle, "data": data}, map[string]any{"blockHash": receipt.BlockHash, "requireCanonical": true}})
	if err != nil || trace == baseNoTrace {
		return out, err
	}
	out.CallTrace, err = s.rpcRaw(ctx, n, e, "debug_traceTransaction", []any{hash, map[string]any{"tracer": "callTracer", "timeout": "15s", "tracerConfig": map[string]bool{"onlyTopCall": false}}})
	return out, err
}
