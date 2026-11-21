package chainassets

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/internal/tronnative"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/mr-tron/base58"
)

func TestNileFullNodeAndBroadcastAcknowledgement(t *testing.T) {
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	owner := append([]byte{0x41}, crypto.PubkeyToAddress(key.PublicKey).Bytes()...)
	check := sha256.Sum256(owner)
	check = sha256.Sum256(check[:])
	now := time.Now().UnixMilli()
	transfer := tronnative.Transfer{Sender: base58.Encode(append(owner, check[:4]...)), Recipient: "TDHZum2JgP2ZgfnveW9f3VM3UjaafdJXio", Sun: 1000, Reference: tronnative.Head{ID: "0000000000000001d698d4192c56cb6be724a558448e2684802de4d6cd8690dc", Height: 1, Timestamp: now}, Expiration: now + tronnative.Lifetime.Milliseconds()}
	raw, err := transfer.Raw()
	if err != nil {
		t.Fatal(err)
	}
	id := tronnative.TxID(raw)
	digest, _ := hex.DecodeString(id)
	sig, err := crypto.Sign(digest, key)
	if err != nil {
		t.Fatal(err)
	}
	signed, err := transfer.Bind(hex.EncodeToString(raw), id, hex.EncodeToString(sig))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, genesis string
		result        bool
		ack, code     string
		send, ok      bool
	}{
		{"wrong full node", TRONMainnetGenesis, true, id, "SUCCESS", false, false},
		{"different txID", TRONNileGenesis, true, "other", "", true, false},
		{"rejected", TRONNileGenesis, false, id, "SIGERROR", true, false},
		{"conflicting acknowledgement", TRONNileGenesis, true, id, "SIGERROR", true, false},
		{"original", TRONNileGenesis, true, id, "SUCCESS", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var broadcasts int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/wallet/getblockbynum":
					json.NewEncoder(w).Encode(map[string]string{"blockID": tc.genesis})
				case "/wallet/broadcasthex":
					broadcasts++
					var body map[string]string
					if json.NewDecoder(r.Body).Decode(&body) != nil || body["transaction"] != signed.Hex() {
						t.Error("unexpected signed bytes")
					}
					json.NewEncoder(w).Encode(map[string]any{"result": tc.result, "txid": tc.ack, "code": tc.code})
				default:
					t.Error("unexpected endpoint", r.URL.Path)
				}
			}))
			defer server.Close()
			s, err := New(Config{Networks: []Endpoint{{Network: TRONNile, URL: server.URL}}}, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			err = s.SubmitNileTRX(context.Background(), signed)
			if (err == nil) != tc.ok || (broadcasts == 1) != tc.send {
				t.Fatal(broadcasts, err)
			}
		})
	}
}
