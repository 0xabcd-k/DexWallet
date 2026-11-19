package chainassets

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

const maxResponseBytes = 2 << 20
const rpcRequestID = 1

var integerPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)$`)

type upstreamError struct{ code, detail string }

func (e *upstreamError) Error() string { return e.code + ": " + e.detail }
func invalid(detail string) error      { return &upstreamError{ErrorInvalidResponse, detail} }
func (s *Service) request(ctx context.Context, n Network, e Endpoint, path string, query url.Values, body any, out any) error {
	var reader io.Reader
	method := http.MethodGet
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
		method = http.MethodPost
	}
	req, err := http.NewRequestWithContext(ctx, method, endpointURL(e, path, query), reader)
	if err != nil {
		return invalid("request construction")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "DexWallet/chain-rpc")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if e.APIKey != "" {
		header := "X-API-Key"
		if n.Family == TRON {
			header = "TRON-PRO-API-KEY"
		}
		req.Header.Set(header, e.APIKey)
	}
	response, err := s.client.Do(req)
	if err != nil {
		if u, ok := err.(*url.Error); ok {
			err = u.Err
		}
		return fmt.Errorf("upstream request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("upstream HTTP status %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return fmt.Errorf("upstream response read: %w", err)
	}
	if len(data) > maxResponseBytes {
		return invalid("response too large")
	}
	if err = json.Unmarshal(data, out); err != nil {
		return invalid("invalid JSON response")
	}
	return nil
}
func (s *Service) rpc(ctx context.Context, n Network, e Endpoint, method string, params any, out any) error {
	raw, err := s.rpcRaw(ctx, n, e, method, params)
	if err != nil {
		return err
	}
	if string(raw) == "null" {
		return invalid("missing RPC result")
	}
	if err = json.Unmarshal(raw, out); err != nil {
		return invalid("invalid RPC result shape")
	}
	return nil
}

// Some transaction queries legitimately return null; balance queries do not.
func (s *Service) rpcRaw(ctx context.Context, n Network, e Endpoint, method string, params any) (json.RawMessage, error) {
	var response struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      int             `json:"id"`
		Result  json.RawMessage `json:"result"`
		Error   *struct {
			Code int `json:"code"`
		} `json:"error"`
	}
	if err := s.request(ctx, n, e, "", nil, map[string]any{"jsonrpc": "2.0", "id": rpcRequestID, "method": method, "params": params}, &response); err != nil {
		return nil, err
	}
	if response.Error != nil {
		return nil, fmt.Errorf("RPC %s returned code %d", method, response.Error.Code)
	}
	if response.JSONRPC != "2.0" || response.ID != rpcRequestID || len(response.Result) == 0 {
		return nil, invalid("missing RPC result or mismatched id")
	}
	return response.Result, nil
}
func decimalInteger(raw string) (*big.Int, error) {
	if len(raw) > 78 || !integerPattern.MatchString(raw) {
		return nil, invalid("invalid unsigned integer")
	}
	n, ok := new(big.Int).SetString(raw, 10)
	if !ok || n.BitLen() > 256 {
		return nil, invalid("integer exceeds 256 bits")
	}
	return n, nil
}
func hexInteger(raw string, abi bool) (*big.Int, error) {
	if strings.HasPrefix(raw, "0x") {
		raw = raw[2:]
	} else if !abi {
		return nil, invalid("hex quantity without prefix")
	}
	if len(raw) == 0 || len(raw) > 64 || abi && len(raw) != 64 {
		return nil, invalid("invalid hex/ABI quantity size")
	}
	for _, c := range raw {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return nil, invalid("invalid hex quantity")
		}
	}
	n, ok := new(big.Int).SetString(raw, 16)
	if !ok {
		return nil, invalid("invalid hex quantity")
	}
	return n, nil
}
func amount(n *big.Int, decimals int) string {
	text := n.String()
	if decimals == 0 {
		return text
	}
	if len(text) <= decimals {
		text = strings.Repeat("0", decimals+1-len(text)) + text
	}
	split := len(text) - decimals
	return strings.TrimRight(strings.TrimRight(text[:split]+"."+text[split:], "0"), ".")
}
func (s *Service) checkNetwork(ctx context.Context, n Network, e Endpoint) error {
	if n.Family == Base {
		var got string
		if err := s.rpc(ctx, n, e, "eth_chainId", []any{}, &got); err != nil {
			return err
		}
		number, err := hexInteger(got, false)
		if err != nil {
			return err
		}
		expected := int64(8453)
		if n.Testnet {
			expected = 84532
		}
		if number.Cmp(big.NewInt(expected)) != 0 {
			return &upstreamError{ErrorNetwork, "EVM chain id differs"}
		}
	}
	if n.Family == Solana {
		var got string
		if err := s.rpc(ctx, n, e, "getGenesisHash", []any{}, &got); err != nil {
			return err
		}
		expected := "5eykt4UsFv8P8NJdTREpY1vzqKqZKvdpKuc147dw2N9d"
		if n.Testnet {
			expected = "EtWTRABZaYq6iMfeYKouRu166VU2xqa1wcaWoxPkrZBG"
		}
		if got != expected {
			return &upstreamError{ErrorNetwork, "Solana genesis hash differs"}
		}
	}
	return nil
}
