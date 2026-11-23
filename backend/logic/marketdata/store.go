package marketdata

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/redis/go-redis/v9"
)

const redisRatesKey = ":market:kraken:usd"

type Store struct {
	client *redis.Client
	key    string
}

func NewStore(client *redis.Client, namespace string) *Store {
	return &Store{client, namespace + redisRatesKey}
}

func (s *Store) Key() string { return s.key }

// Overlapping refreshes cannot replace a newer trade with a delayed response.
// Epoch microseconds are exact Lua integers for current exchange timestamps.
var publishNewer = redis.NewScript(`
local old = redis.call('HGET', KEYS[1], ARGV[1])
if old and cjson.decode(old).ObservedMicros >= tonumber(ARGV[2]) then return 0 end
return redis.call('HSET', KEYS[1], ARGV[1], ARGV[3])
`)

func (s *Store) publish(ctx context.Context, r Rate) error {
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	return publishNewer.Run(ctx, s.client, []string{s.key}, r.Symbol, r.ObservedMicros, data).Err()
}

// Current reads every currency from one Redis snapshot. Consumers decide
// freshness from the timestamps of the currencies involved in their operation.
func (s *Store) Current(ctx context.Context) (Table, error) {
	values, err := s.client.HGetAll(ctx, s.key).Result()
	if err != nil {
		return nil, err
	}
	table := make(Table, len(values))
	for symbol, raw := range values {
		var r Rate
		if err := json.Unmarshal([]byte(raw), &r); err != nil {
			return nil, fmt.Errorf("decode %s USD rate: %w", symbol, err)
		}
		if r.Symbol != symbol || r.TradeID == "" || r.ObservedMicros <= 0 || !r.USDPerCoin.IsPositive() {
			return nil, fmt.Errorf("invalid stored %s USD rate", symbol)
		}
		table[symbol] = r
	}
	return table, nil
}
