package chainassets

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
)

// ReadTONHistory returns consecutive account transactions in increasing logical time.
// Account-chain continuity is validated by the listener before it advances its cursor.
func (s *Service) ReadTONHistory(ctx context.Context, account string, startLT int64, limit int) (json.RawMessage, error) {
	n, e, err := s.tonTestnet()
	if err != nil {
		return nil, err
	}
	account, err = normalizeAddress(n, account)
	if err != nil || startLT <= 0 || limit < 1 || limit > 8 {
		return nil, fmt.Errorf("invalid TON history query")
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	read := s.tonReader(ctx, n, e)
	var raw json.RawMessage
	if err = read("/api/v2/getMasterchainInfo", nil, nil, &raw); err != nil {
		return nil, err
	}
	if err = checkTONTestnet(raw); err != nil {
		return nil, err
	}
	query := url.Values{"account": {account}, "sort": {"asc"}, "start_lt": {strconv.FormatInt(startLT, 10)}, "limit": {strconv.Itoa(limit)}}
	if err = read("/api/v3/transactions", query, nil, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}
