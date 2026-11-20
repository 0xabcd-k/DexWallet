package chainassets

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/0xabcd-k/DexWallet/backend/internal/solanaspl"
)

func TestSPLPayoutAccountChecks(t *testing.T) {
	transfer, err := solanaspl.New("9ucco3Ath4uS71gyfHggdo9jXzUnhkRr5oPHp8qVUVGf", "7Njq3pBVPhZWEoRh8w22eC2KBcGUhhCvwFFitsyTg7A5", "4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU", "1000", false)
	if err != nil {
		t.Fatal(err)
	}
	account := func(owner, mint, state, amount string) map[string]any {
		return map[string]any{"owner": solanaspl.Program, "data": map[string]any{"parsed": map[string]any{"type": "account", "info": map[string]any{"owner": owner, "mint": mint, "state": state, "tokenAmount": map[string]any{"amount": amount, "decimals": 6}}}}}
	}
	src := account(transfer.Sender, transfer.Mint, "initialized", "1000")
	dst := account(transfer.Recipient, transfer.Mint, "initialized", "0")
	for _, tc := range []struct {
		name       string
		accounts   []any
		create, ok bool
	}{
		{"existing", []any{src, dst}, false, true}, {"missing_requires_authorization", []any{src, nil}, false, false}, {"new_authorized", []any{src, nil}, true, true},
		{"frozen", []any{src, account(transfer.Recipient, transfer.Mint, "frozen", "0")}, true, false},
		{"wrong_mint", []any{src, account(transfer.Recipient, transfer.Sender, "initialized", "0")}, false, false},
		{"insufficient", []any{account(transfer.Sender, transfer.Mint, "initialized", "999"), dst}, false, false},
		{"wrong_authority", []any{src, account(transfer.Sender, transfer.Mint, "initialized", "1000")}, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				method, _ := rpcInput(t, r)
				switch method {
				case "getGenesisHash":
					rpcResult(w, "EtWTRABZaYq6iMfeYKouRu166VU2xqa1wcaWoxPkrZBG")
				case "getMultipleAccounts":
					rpcResult(w, map[string]any{"value": tc.accounts})
				default:
					t.Error(method)
				}
			}))
			defer server.Close()
			s, err := New(Config{Networks: []Endpoint{{Network: SolanaDevnet, URL: server.URL}}}, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			testTransfer := transfer
			testTransfer.CreateATA = tc.create
			if err = s.CheckSolanaTokenAccounts(context.Background(), testTransfer); (err == nil) != tc.ok {
				t.Fatal(err)
			}
		})
	}
}
