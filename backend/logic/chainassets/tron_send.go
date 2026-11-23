package chainassets

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/internal/tronnative"
)

// FullNode and Solidity routes may be backed by different nodes. Check the
// FullNode genesis before taking a reference, inspecting pending data or sending.
func (s *Service) nileFull(ctx context.Context) (Network, Endpoint, error) {
	for _, n := range s.networks {
		if n.ID != TRONNile || !n.Enabled {
			continue
		}
		e := s.endpoints[n.ID]
		var raw json.RawMessage
		if err := s.tronObject(ctx, n, e, "/wallet/getblockbynum", map[string]any{"num": 0}, &raw); err != nil {
			return Network{}, Endpoint{}, err
		}
		var genesis struct{ BlockID string }
		if json.Unmarshal(raw, &genesis) != nil || genesis.BlockID != TRONNileGenesis {
			return Network{}, Endpoint{}, invalid("TRON FullNode is not Nile")
		}
		return n, e, nil
	}
	return Network{}, Endpoint{}, fmt.Errorf("TRON Nile endpoint required")
}

func (s *Service) NileHead(ctx context.Context) (tronnative.Head, error) {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	n, e, err := s.nileFull(ctx)
	if err != nil {
		return tronnative.Head{}, err
	}
	return s.nileHeadAt(ctx, n, e)
}

func (s *Service) nileHeadAt(ctx context.Context, n Network, e Endpoint) (tronnative.Head, error) {
	var raw json.RawMessage
	if err := s.tronObject(ctx, n, e, "/wallet/getnowblock", map[string]any{}, &raw); err != nil {
		return tronnative.Head{}, err
	}
	var block struct {
		BlockID string
		Header  struct {
			Raw struct{ Number, Timestamp int64 } `json:"raw_data"`
		} `json:"block_header"`
	}
	if json.Unmarshal(raw, &block) != nil {
		return tronnative.Head{}, invalid("malformed TRON head")
	}
	head := tronnative.Head{ID: block.BlockID, Height: block.Header.Raw.Number, Timestamp: block.Header.Raw.Timestamp}
	if !head.Valid() {
		return tronnative.Head{}, invalid("inconsistent TRON head")
	}
	return head, nil
}

func (s *Service) NileTransactionSeen(ctx context.Context, txID string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	n, e, err := s.nileFull(ctx)
	if err != nil {
		return false, err
	}
	for _, method := range []string{"gettransactionbyid", "gettransactionfrompending"} {
		var raw json.RawMessage
		if err = s.tronObject(ctx, n, e, "/wallet/"+method, map[string]any{"value": txID}, &raw); err != nil {
			return false, err
		}
		var obj map[string]json.RawMessage
		if err = json.Unmarshal(raw, &obj); err != nil {
			return false, err
		}
		if len(obj) == 0 {
			continue
		}
		var id string
		if json.Unmarshal(obj["txID"], &id) != nil || id != txID {
			return false, invalid("TRON observation does not identify original transaction")
		}
		return true, nil
	}
	return false, nil
}

func (s *Service) SubmitNileTRX(ctx context.Context, signed tronnative.Signed) error {
	if signed.TokenContract() != "" {
		return fmt.Errorf("native Nile TRX authorization required")
	}
	return s.submitNile(ctx, signed)
}

func (s *Service) NileUSDTContract() string { return s.endpoints[TRONNile].TestUSDTContract }

func (s *Service) SubmitNileUSDT(ctx context.Context, signed tronnative.Signed) error {
	if signed.TokenContract() == "" || signed.TokenContract() != s.NileUSDTContract() {
		return fmt.Errorf("Nile USDT contract differs from configured asset")
	}
	return s.submitNile(ctx, signed)
}

func (s *Service) submitNile(ctx context.Context, signed tronnative.Signed) error {
	if signed.Hex() == "" {
		return fmt.Errorf("verified TRON transfer required")
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	n, e, err := s.nileFull(ctx)
	if err != nil {
		return err
	}
	var ack struct {
		Result                     bool
		TxID, Code, Message, Error string
	}
	// The genesis lookup may have crossed the signed deadline.
	if signed.Expired(time.Now()) {
		return fmt.Errorf("TRON authorization expired before broadcast")
	}
	err = s.request(ctx, n, e, "/wallet/broadcasthex", nil, map[string]string{"transaction": signed.Hex()}, &ack)
	if err != nil {
		return err
	}
	if !ack.Result || ack.TxID != signed.ID() || ack.Error != "" || (ack.Code != "" && ack.Code != "SUCCESS") {
		return fmt.Errorf("TRON broadcast acknowledgement result=%t txid=%q code=%q message=%q error=%q", ack.Result, ack.TxID, ack.Code, ack.Message, ack.Error)
	}
	return nil
}
