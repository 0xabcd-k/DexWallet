package chainassets

import (
	"context"
	"encoding/json"
	"fmt"
)

// NileHistory contains Solidity API responses from one configured endpoint.
// Block is absent when the requested height is above the solid head.
type NileHistory struct {
	Genesis, SolidHead, Block json.RawMessage
	Infos                     []json.RawMessage
}

func (s *Service) ReadNileHistory(ctx context.Context, height int64, includeReceipts bool) (NileHistory, error) {
	var history NileHistory
	if height <= 0 {
		return history, fmt.Errorf("invalid Nile history height")
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	var n Network
	for _, network := range s.networks {
		if network.ID == TRONNile && network.Enabled {
			n = network
			break
		}
	}
	if n.ID == "" {
		return history, fmt.Errorf("TRON Nile endpoint required")
	}
	e := s.endpoints[n.ID]
	read := func(method string, body map[string]any, target *json.RawMessage) error {
		return s.tronObject(ctx, n, e, "/walletsolidity/"+method, body, target)
	}
	if err := read("getblockbynum", map[string]any{"num": 0}, &history.Genesis); err != nil {
		return history, err
	}
	var genesis struct{ BlockID string }
	if json.Unmarshal(history.Genesis, &genesis) != nil || genesis.BlockID != TRONNileGenesis {
		return history, invalid("TRON solidity endpoint is not Nile")
	}
	if err := read("getnowblock", map[string]any{}, &history.SolidHead); err != nil {
		return history, err
	}
	var head struct {
		Header struct {
			Raw struct{ Number int64 } `json:"raw_data"`
		} `json:"block_header"`
	}
	if json.Unmarshal(history.SolidHead, &head) != nil || head.Header.Raw.Number <= 0 {
		return history, invalid("invalid Nile solid head")
	}
	if height > head.Header.Raw.Number {
		return history, nil
	}
	if height == head.Header.Raw.Number {
		history.Block = history.SolidHead
	} else if err := read("getblockbynum", map[string]any{"num": height}, &history.Block); err != nil {
		return history, err
	}
	if includeReceipts {
		err := s.request(ctx, n, e, "/walletsolidity/gettransactioninfobyblocknum", nil, map[string]any{"num": height, "visible": false}, &history.Infos)
		if err != nil {
			return history, err
		}
		if history.Infos == nil {
			return history, invalid("missing Nile block receipts")
		}
	}
	return history, nil
}
