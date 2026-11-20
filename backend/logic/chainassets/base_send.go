package chainassets

import (
	"context"
	"fmt"
	"strings"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
)

const BaseSepoliaChainID = 84532

func (s *Service) baseSepoliaRPC(ctx context.Context, method string, params any, out any) error {
	for _, n := range s.networks {
		if n.ID == BaseSepolia && n.Enabled {
			ctx, cancel := context.WithTimeout(ctx, s.timeout)
			defer cancel()
			e := s.endpoints[n.ID]
			if err := s.checkNetwork(ctx, n, e); err != nil {
				return err
			}
			return s.rpc(ctx, n, e, method, params, out)
		}
	}
	return fmt.Errorf("Base Sepolia endpoint required")
}

func (s *Service) PendingBaseNonce(ctx context.Context, sender string) (uint64, error) {
	var result hexutil.Uint64
	err := s.baseSepoliaRPC(ctx, "eth_getTransactionCount", []string{sender, "pending"}, &result)
	return uint64(result), err
}
func (s *Service) SubmitBaseETH(ctx context.Context, tx *types.Transaction) error {
	if tx == nil || tx.Type() != types.DynamicFeeTxType || !tx.ChainId().IsInt64() || tx.ChainId().Int64() != BaseSepoliaChainID || tx.To() == nil || tx.Value().Sign() <= 0 || len(tx.Data()) != 0 || len(tx.AccessList()) != 0 {
		return invalid("invalid Base Sepolia ETH transfer")
	}
	wire, err := tx.MarshalBinary()
	if err != nil {
		return err
	}
	var ack string
	if err = s.baseSepoliaRPC(ctx, "eth_sendRawTransaction", []string{hexutil.Encode(wire)}, &ack); err != nil {
		return err
	}
	if !strings.EqualFold(ack, tx.Hash().Hex()) {
		return invalid("Base transaction hash acknowledgement mismatch")
	}
	return nil
}
