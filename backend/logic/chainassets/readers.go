package chainassets

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"net/url"
	"strings"

	"github.com/mr-tron/base58"
)

const splTokenProgram = "TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA"
const balanceOfSelector = "70a08231"

func (s *Service) balance(ctx context.Context, n Network, e Endpoint, owner string, a Asset) (string, error) {
	switch n.Family {
	case Base:
		return s.evm(ctx, n, e, owner, a)
	case Solana:
		return s.solana(ctx, n, e, owner, a)
	case TON:
		return s.ton(ctx, n, e, owner, a)
	case TRON:
		return s.tron(ctx, n, e, owner, a)
	}
	return "", invalid("unsupported reader")
}
func (s *Service) evm(ctx context.Context, n Network, e Endpoint, owner string, a Asset) (string, error) {
	var raw string
	method := "eth_getBalance"
	params := []any{owner, "latest"}
	if a.Origin != Native {
		method = "eth_call"
		params = []any{map[string]string{"to": a.Contract, "data": "0x" + balanceOfSelector + strings.Repeat("0", 24) + owner[2:]}, "latest"}
	}
	if err := s.rpc(ctx, n, e, method, params, &raw); err != nil {
		return "", err
	}
	number, err := hexInteger(raw, a.Origin != Native)
	if err != nil {
		return "", err
	}
	return amount(number, a.Decimals), nil
}
func (s *Service) solana(ctx context.Context, n Network, e Endpoint, owner string, a Asset) (string, error) {
	if a.Origin == Native {
		var data struct {
			Value *json.Number `json:"value"`
		}
		if err := s.rpc(ctx, n, e, "getBalance", []any{owner, map[string]string{"commitment": "confirmed"}}, &data); err != nil {
			return "", err
		}
		if data.Value == nil {
			return "", invalid("missing lamports")
		}
		value, err := decimalInteger(data.Value.String())
		if err != nil {
			return "", err
		}
		return amount(value, a.Decimals), nil
	}
	var response struct {
		Value *[]struct {
			Pubkey  string `json:"pubkey"`
			Account struct {
				Owner string `json:"owner"`
				Data  struct {
					Program string `json:"program"`
					Parsed  struct {
						Type string `json:"type"`
						Info struct {
							Owner       string `json:"owner"`
							Mint        string `json:"mint"`
							State       string `json:"state"`
							TokenAmount struct {
								Amount   string `json:"amount"`
								Decimals *int   `json:"decimals"`
							} `json:"tokenAmount"`
						} `json:"info"`
					} `json:"parsed"`
				} `json:"data"`
			} `json:"account"`
		} `json:"value"`
	}
	if err := s.rpc(ctx, n, e, "getTokenAccountsByOwner", []any{owner, map[string]string{"mint": a.Contract}, map[string]string{"encoding": "jsonParsed", "commitment": "confirmed"}}, &response); err != nil {
		return "", err
	}
	if response.Value == nil {
		return "", invalid("missing token accounts")
	}
	total := new(big.Int)
	seen := map[string]bool{}
	for _, v := range *response.Value {
		info := v.Account.Data.Parsed.Info
		if v.Pubkey == "" || seen[v.Pubkey] || v.Account.Owner != splTokenProgram || v.Account.Data.Program != "spl-token" || v.Account.Data.Parsed.Type != "account" ||
			info.Owner != owner || info.Mint != a.Contract || (info.State != "initialized" && info.State != "frozen") || info.TokenAmount.Decimals == nil || *info.TokenAmount.Decimals != a.Decimals {
			return "", invalid("token account identity or precision mismatch")
		}
		seen[v.Pubkey] = true
		value, err := decimalInteger(info.TokenAmount.Amount)
		if err != nil {
			return "", err
		}
		total.Add(total, value)
	}
	if total.BitLen() > 256 {
		return "", invalid("token balance overflow")
	}
	return amount(total, a.Decimals), nil
}
func (s *Service) ton(ctx context.Context, n Network, e Endpoint, owner string, a Asset) (string, error) {
	if a.Origin == Native {
		var response struct {
			OK     *bool           `json:"ok"`
			Result json.RawMessage `json:"result"`
		}
		if err := s.request(ctx, n, e, "/api/v2/getAddressBalance", url.Values{"address": {owner}}, nil, &response); err != nil {
			return "", err
		}
		if response.OK == nil || !*response.OK {
			return "", invalid("TON balance result not successful")
		}
		var raw string
		if err := json.Unmarshal(response.Result, &raw); err != nil {
			return "", invalid("missing TON balance")
		}
		value, err := decimalInteger(raw)
		if err != nil {
			return "", err
		}
		return amount(value, a.Decimals), nil
	}
	var response struct {
		Wallets *[]struct {
			Address string `json:"address"`
			Balance string `json:"balance"`
			Owner   string `json:"owner"`
			Jetton  string `json:"jetton"`
		} `json:"jetton_wallets"`
	}
	query := url.Values{"owner_address": {owner}, "jetton_address": {a.Contract}, "exclude_zero_balance": {"false"}, "limit": {"2"}}
	if err := s.request(ctx, n, e, "/api/v3/jetton/wallets", query, nil, &response); err != nil {
		return "", err
	}
	// One standard jetton master has one wallet for an owner; do not sum unknown
	// duplicate contracts or silently truncate an unexpected provider result.
	if response.Wallets == nil || len(*response.Wallets) > 1 {
		return "", invalid("missing or ambiguous jetton wallet")
	}
	if len(*response.Wallets) == 0 {
		return "0", nil
	}
	w := (*response.Wallets)[0]
	if _, err := normalizeAddress(n, w.Address); err != nil {
		return "", invalid("invalid jetton wallet address")
	}
	if !sameAddress(n, w.Owner, owner) || !sameAddress(n, w.Jetton, a.Contract) {
		return "", invalid("jetton master or owner mismatch")
	}
	value, err := decimalInteger(w.Balance)
	if err != nil {
		return "", err
	}
	return amount(value, a.Decimals), nil
}
func (s *Service) tron(ctx context.Context, n Network, e Endpoint, owner string, a Asset) (string, error) {
	if a.Origin == Native {
		var raw json.RawMessage
		if err := s.request(ctx, n, e, "/wallet/getaccount", nil, map[string]any{"address": owner, "visible": true}, &raw); err != nil {
			return "", err
		}
		var data struct {
			Address string       `json:"address"`
			Balance *json.Number `json:"balance"`
		}
		if err := json.Unmarshal(raw, &data); err != nil {
			return "", invalid("invalid TRON account")
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
			return "", invalid("missing TRON account")
		}
		if len(fields) == 0 {
			return "0", nil
		} // Full node returns {} for an unactivated account.
		if !sameAddress(n, data.Address, owner) {
			return "", invalid("TRON account address mismatch")
		}
		if data.Balance == nil {
			if _, present := fields["balance"]; present {
				return "", invalid("null TRON balance")
			}
			return "0", nil
		} // protobuf omits an explicit zero balance.
		value, err := decimalInteger(data.Balance.String())
		if err != nil {
			return "", err
		}
		return amount(value, a.Decimals), nil
	}
	decoded, _ := base58.Decode(owner) // already validated at the query boundary
	body := map[string]any{"owner_address": a.Contract, "contract_address": a.Contract, "function_selector": "balanceOf(address)", "parameter": strings.Repeat("0", 24) + hex.EncodeToString(decoded[1:21]), "visible": true}
	var response struct {
		Result struct {
			Result *bool `json:"result"`
		} `json:"result"`
		Constant []string `json:"constant_result"`
	}
	if err := s.request(ctx, n, e, "/wallet/triggerconstantcontract", nil, body, &response); err != nil {
		return "", err
	}
	if response.Result.Result == nil || !*response.Result.Result || len(response.Constant) != 1 {
		return "", invalid("TRC20 constant call failed")
	}
	value, err := hexInteger(response.Constant[0], true)
	if err != nil {
		return "", err
	}
	return amount(value, a.Decimals), nil
}
