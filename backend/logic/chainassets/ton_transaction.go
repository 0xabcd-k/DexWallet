package chainassets

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

// Pinned zero-state roots from the official TON mainnet/testnet global configs.
const TONMainnetZeroState = "F6OpKZKqvqeFp6CQmFomXNMfMj2EnaUSOXN+Mh+wVWk="
const TONTestnetZeroState = "gj+B8wb/AmlPk1z1AhVI484rhrUpgSr2oSFIh56VoSg="
const tonPublicRequestInterval = 1100 * time.Millisecond

type TONEvidence struct {
	Trace        json.RawMessage `json:"trace,omitempty"`
	Dispatch     json.RawMessage `json:"dispatch,omitempty"`
	SenderWallet json.RawMessage `json:"senderWallet,omitempty"`
	Masterchain  json.RawMessage `json:"masterchain"`
	Transaction  json.RawMessage `json:"transaction"`
	Parent       json.RawMessage `json:"parent"`
	Block        json.RawMessage `json:"block"`
	Wallet       json.RawMessage `json:"wallet,omitempty"`
}

// Hash identifies the recipient transaction, not the sending wallet's external
// message. Wallet derives the owner's address from an allowlisted master.
func (s *Service) ReadTONEvidence(ctx context.Context, network, hash, master, owner string) (TONEvidence, error) {
	var out TONEvidence
	if len(hash) != 64 {
		return out, fmt.Errorf("invalid TON transaction hash")
	}
	if _, err := hex.DecodeString(hash); err != nil {
		return out, fmt.Errorf("invalid TON transaction hash")
	}
	var n Network
	for _, candidate := range s.networks {
		if candidate.ID == network && candidate.Family == TON {
			n = candidate
			break
		}
	}
	if n.ID == "" || !n.Enabled {
		return out, fmt.Errorf("TON receipt network %s not enabled", network)
	}
	var walletOwner *address.Address
	if master != "" {
		canonical, err := normalizeAddress(n, master)
		if err != nil {
			return out, err
		}
		master = canonical
		allowed := false
		for _, a := range n.Assets {
			if a.Contract != "" && sameAddress(n, a.Contract, master) {
				allowed = true
				break
			}
		}
		normalized, err := normalizeAddress(n, owner)
		if !allowed || err != nil {
			return out, fmt.Errorf("invalid TON master/owner")
		}
		walletOwner, err = address.ParseRawAddr(normalized)
		if err != nil {
			return out, err
		}
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	e := s.endpoints[network]
	read := s.tonReader(ctx, n, e)
	if err := read("/api/v2/getMasterchainInfo", nil, nil, &out.Masterchain); err != nil {
		return out, err
	}
	var info struct {
		OK     bool `json:"ok"`
		Result struct {
			Init struct {
				Root string `json:"root_hash"`
			} `json:"init"`
		} `json:"result"`
	}
	want := TONMainnetZeroState
	if network == TONTestnet {
		want = TONTestnetZeroState
	}
	if json.Unmarshal(out.Masterchain, &info) != nil || !info.OK || info.Result.Init.Root != want {
		return out, invalid("TON zero-state does not match network")
	}
	hash = strings.ToLower(hash)
	if err := read("/api/v3/transactions", url.Values{"hash": {hash}, "limit": {"2"}}, nil, &out.Transaction); err != nil {
		return out, err
	}
	var response struct {
		Transactions *[]struct {
			Block struct {
				Workchain *int   `json:"workchain"`
				Shard     string `json:"shard"`
				Seqno     int64  `json:"seqno"`
			} `json:"block_ref"`
		} `json:"transactions"`
	}
	if json.Unmarshal(out.Transaction, &response) != nil || response.Transactions == nil || len(*response.Transactions) > 1 {
		return out, invalid("ambiguous TON transaction")
	}
	if len(*response.Transactions) == 0 {
		return out, nil
	}
	block := (*response.Transactions)[0].Block
	if block.Workchain == nil || (*block.Workchain != 0 && *block.Workchain != -1) || len(block.Shard) != 16 || block.Seqno <= 0 {
		return out, invalid("invalid TON block reference")
	}
	if _, err := hex.DecodeString(block.Shard); err != nil {
		return out, invalid("invalid TON shard")
	}
	if err := read("/api/v3/adjacentTransactions", url.Values{"hash": {hash}, "direction": {"in"}}, nil, &out.Parent); err != nil {
		return out, err
	}
	if err := read("/api/v3/blocks", url.Values{"workchain": {strconv.Itoa(*block.Workchain)}, "shard": {block.Shard}, "seqno": {strconv.FormatInt(block.Seqno, 10)}, "limit": {"2"}}, nil, &out.Block); err != nil {
		return out, err
	}
	if master != "" {
		b := cell.BeginCell()
		if err := b.StoreAddr(walletOwner); err != nil {
			return out, err
		}
		body := map[string]any{"address": master, "method": "get_wallet_address", "stack": []any{map[string]string{"type": "slice", "value": base64.StdEncoding.EncodeToString(b.EndCell().ToBOC())}}}
		if err := read("/api/v3/runGetMethod", nil, body, &out.Wallet); err != nil {
			return out, err
		}
	}
	return out, nil
}

// One observation spaces public requests without retrying side effects.
func (s *Service) tonReader(ctx context.Context, n Network, e Endpoint) func(string, url.Values, any, *json.RawMessage) error {
	var completed time.Time
	return func(path string, query url.Values, body any, target *json.RawMessage) error {
		// Public TON Center is limited to 1 RPS. Do not retry or conceal HTTP errors.
		// This spaces one observation; production concurrent workers need API quotas.
		if e.APIKey == "" && !completed.IsZero() {
			timer := time.NewTimer(time.Until(completed.Add(tonPublicRequestInterval)))
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
			}
		}
		if err := s.request(ctx, n, e, path, query, body, target); err != nil {
			return err
		}
		completed = time.Now()
		var object map[string]json.RawMessage
		if json.Unmarshal(*target, &object) != nil || object == nil || object["error"] != nil {
			return invalid("invalid TON response: " + path)
		}
		return nil
	}
}
