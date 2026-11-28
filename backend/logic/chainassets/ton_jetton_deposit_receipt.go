package chainassets

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"strconv"
)

// TON Center can normalize the outer message hash. Locate its original signed
// body first, then fetch the trace and the block of the token credit itself.
func (s *Service) ReadTONJettonDepositEvidence(ctx context.Context, relay TONDepositRelay) (TONEvidence, error) {
	var e TONEvidence
	n, node, err := s.tonTestnet()
	if err != nil {
		return e, err
	}
	h, err := hex.DecodeString(relay.DispatchHash)
	if err != nil || len(h) != 32 || relay.Jetton == nil {
		return e, invalid("invalid Jetton relay lookup")
	}
	master, owner, err := allowedTONJetton(n, TONDepositTestMaster, relay.Recipient)
	if err != nil {
		return e, err
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	read := s.tonReader(ctx, n, node)
	if err = read("/api/v2/getMasterchainInfo", nil, nil, &e.Masterchain); err != nil {
		return e, err
	}
	if err = checkTONTestnet(e.Masterchain); err != nil {
		return e, err
	}
	if err = read("/api/v3/transactionsByMessage", url.Values{"body_hash": {relay.DispatchHash}, "direction": {"in"}, "limit": {"2"}}, nil, &e.Dispatch); err != nil {
		return e, err
	}
	var root struct{ Transactions *[]struct{ Hash string } }
	if json.Unmarshal(e.Dispatch, &root) != nil || root.Transactions == nil || len(*root.Transactions) > 1 {
		return e, invalid("ambiguous Jetton relay dispatch")
	}
	if len(*root.Transactions) == 0 {
		return e, nil
	}
	if err = read("/api/v3/traces", url.Values{"trace_id": {(*root.Transactions)[0].Hash}, "include_actions": {"false"}, "limit": {"2"}}, nil, &e.Trace); err != nil {
		return e, err
	}
	var data struct {
		Traces *[]struct {
			Transactions map[string]struct {
				Account string
				Block   struct {
					Workchain *int
					Shard     string
					Seqno     int64
				} `json:"block_ref"`
			}
		}
	}
	if json.Unmarshal(e.Trace, &data) != nil || data.Traces == nil || len(*data.Traces) > 1 {
		return e, invalid("ambiguous Jetton relay trace")
	}
	if len(*data.Traces) == 0 {
		return e, nil
	}
	for _, t := range (*data.Traces)[0].Transactions {
		addr, err := normalizeAddress(n, t.Account)
		if err != nil {
			return e, err
		}
		if addr != relay.Jetton.ReceiverWallet {
			continue
		}
		b := t.Block
		if e.Block != nil || b.Workchain == nil || *b.Workchain != 0 || len(b.Shard) != 16 || b.Seqno <= 0 {
			return e, invalid("ambiguous receiving Jetton block")
		}
		if _, err = hex.DecodeString(b.Shard); err != nil {
			return e, err
		}
		if err = read("/api/v3/blocks", url.Values{"workchain": {"0"}, "shard": {b.Shard}, "seqno": {strconv.FormatInt(b.Seqno, 10)}, "limit": {"2"}}, nil, &e.Block); err != nil {
			return e, err
		}
		if err = read("/api/v3/runGetMethod", nil, tonWalletQuery(master, owner), &e.Wallet); err != nil {
			return e, err
		}
	}
	return e, nil
}
