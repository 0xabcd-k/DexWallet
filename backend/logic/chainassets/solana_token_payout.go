package chainassets

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/0xabcd-k/DexWallet/backend/internal/solanaspl"
)

// Both associated accounts are observed in one RPC context. The signed on-chain
// instructions recheck mint, precision and authority if the accounts change later.
func (s *Service) CheckSolanaTokenAccounts(ctx context.Context, t solanaspl.Transfer) error {
	var response struct{ Value []json.RawMessage }
	err := s.devnetRPC(ctx, "getMultipleAccounts", []any{[]string{t.SourceATA, t.DestinationATA}, map[string]string{"encoding": "jsonParsed", "commitment": "confirmed"}}, &response)
	if err != nil {
		return err
	}
	if len(response.Value) != 2 {
		return invalid("missing SPL account observations")
	}
	for index, raw := range response.Value {
		owner := t.Sender
		if index == 1 {
			owner = t.Recipient
		}
		if string(raw) == "null" {
			if index == 1 && t.CreateATA {
				continue
			}
			return fmt.Errorf("required SPL account missing for %s", owner)
		}
		var account struct {
			Owner string
			Data  struct {
				Parsed struct {
					Type string
					Info struct {
						Owner, Mint, State string
						TokenAmount        struct {
							Amount   string
							Decimals *int
						}
					}
				}
			}
		}
		if err = json.Unmarshal(raw, &account); err != nil {
			return invalid("invalid SPL account")
		}
		parsed := account.Data.Parsed
		if account.Owner != solanaspl.Program || parsed.Type != "account" || parsed.Info.Owner != owner || parsed.Info.Mint != t.Mint || parsed.Info.State != "initialized" || parsed.Info.TokenAmount.Decimals == nil || *parsed.Info.TokenAmount.Decimals != solanaspl.Decimals {
			return invalid("unexpected SPL owner, mint, state or decimals")
		}
		amount, err := strconv.ParseUint(parsed.Info.TokenAmount.Amount, 10, 64)
		if err != nil {
			return invalid("invalid SPL balance")
		}
		if index == 0 && amount < t.Amount {
			return fmt.Errorf("insufficient platform token balance")
		}
	}
	return nil
}
