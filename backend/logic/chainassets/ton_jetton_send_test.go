package chainassets

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

func TestTONJettonSendQueriesConfiguredMasterAndThreeHops(t *testing.T) {
	raw, err := os.ReadFile("../chainreceipt/testdata/ton-jetton-send.json")
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		JettonRequest struct{ Sender, Recipient, BodyHash, Master, Wallet string }
		Evidence      TONEvidence
	}
	if err = json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	hash := func(raw json.RawMessage) string {
		var v struct{ Transactions []struct{ Hash string } }
		if err := json.Unmarshal(raw, &v); err != nil {
			t.Fatal(err)
		}
		return v.Transactions[0].Hash
	}
	for _, mode := range []string{"confirmed", "missing dispatch", "missing sender", "missing credit", "wrong network", "bad derivation"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				switch r.URL.Path {
				case "/api/v2/getMasterchainInfo":
					if mode == "wrong network" {
						fmt.Fprintf(w, `{"ok":true,"result":{"init":{"root_hash":%q}}}`, TONMainnetZeroState)
					} else {
						w.Write(f.Evidence.Masterchain)
					}
				case "/api/v3/transactionsByMessage":
					if r.URL.Query().Get("body_hash") != f.JettonRequest.BodyHash || r.URL.Query().Get("direction") != "in" {
						t.Error("wrong signed body lookup")
					}
					if mode == "missing dispatch" {
						fmt.Fprint(w, `{"transactions":[]}`)
					} else {
						w.Write(f.Evidence.Dispatch)
					}
				case "/api/v3/adjacentTransactions":
					if r.URL.Query().Get("direction") != "out" {
						t.Error("wrong traversal")
					}
					switch r.URL.Query().Get("hash") {
					case hash(f.Evidence.Dispatch):
						if mode == "missing sender" {
							fmt.Fprint(w, `{"transactions":[]}`)
						} else {
							w.Write(f.Evidence.Parent)
						}
					case hash(f.Evidence.Parent):
						if mode == "missing credit" {
							fmt.Fprint(w, `{"transactions":[]}`)
						} else {
							w.Write(f.Evidence.Transaction)
						}
					default:
						t.Error("unexpected transaction hop")
						http.Error(w, "unexpected", 400)
					}
				case "/api/v3/runGetMethod":
					var q struct {
						Address, Method string
						Stack           []struct{ Type, Value string }
					}
					if json.NewDecoder(r.Body).Decode(&q) != nil || q.Address != f.JettonRequest.Master || q.Method != "get_wallet_address" || len(q.Stack) != 1 || q.Stack[0].Type != "slice" {
						t.Error("wrong derivation request")
						http.Error(w, "invalid", 400)
						return
					}
					b, e := base64.StdEncoding.DecodeString(q.Stack[0].Value)
					if e != nil {
						t.Error(e)
						return
					}
					c, e := cell.FromBOC(b)
					if e != nil {
						t.Error(e)
						return
					}
					a, e := c.BeginParse().LoadAddr()
					if e != nil {
						t.Error(e)
						return
					}
					if mode == "bad derivation" {
						fmt.Fprint(w, `{"exit_code":1,"stack":[]}`)
						return
					}
					switch a.StringRaw() {
					case f.JettonRequest.Sender:
						w.Write(f.Evidence.SenderWallet)
					case f.JettonRequest.Recipient:
						w.Write(f.Evidence.Wallet)
					default:
						t.Error("unexpected owner")
					}
				case "/api/v3/blocks":
					w.Write(f.Evidence.Block)
				default:
					t.Error(r.URL.Path)
					http.Error(w, "unexpected", 400)
				}
			}))
			defer srv.Close()
			s, err := New(Config{Timeout: time.Second, Networks: []Endpoint{{Network: TONTestnet, URL: srv.URL, APIKey: "test", TestUSDTContract: f.JettonRequest.Master}}}, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			derived, err := s.ResolveTONJettonWallet(context.Background(), f.JettonRequest.Master, f.JettonRequest.Sender)
			if mode == "wrong network" || mode == "bad derivation" {
				if err == nil {
					t.Fatal("bad network/get-method accepted")
				}
			} else if err != nil || derived != f.JettonRequest.Wallet {
				t.Fatal(derived, err)
			}
			before := calls
			if _, err = s.ResolveTONJettonWallet(context.Background(), f.JettonRequest.Recipient, f.JettonRequest.Sender); err == nil || calls != before {
				t.Fatal("unapproved master queried")
			}
			got, err := s.ReadTONJettonSendEvidence(context.Background(), TONTestnet, f.JettonRequest.BodyHash, f.JettonRequest.Master, f.JettonRequest.Recipient)
			if mode == "wrong network" {
				if err == nil || calls-before != 1 {
					t.Fatal("wrong network continued", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "confirmed":
				if calls-before != 7 || !bytes.Equal(got.Dispatch, f.Evidence.Dispatch) || !bytes.Equal(got.Parent, f.Evidence.Parent) || !bytes.Equal(got.Transaction, f.Evidence.Transaction) || !bytes.Equal(got.SenderWallet, f.Evidence.SenderWallet) {
					t.Fatal("lost payment path", calls-before)
				}
			case "missing dispatch":
				if calls-before != 2 || len(got.Transaction) != 0 {
					t.Fatal("fabricated dispatch")
				}
			case "missing sender":
				if calls-before != 3 || len(got.Transaction) != 0 {
					t.Fatal("fabricated sender")
				}
			case "missing credit":
				if len(got.Block) != 0 {
					t.Fatal("fabricated credit")
				}
			}
			before = calls
			if _, err = s.ReadTONJettonSendEvidence(context.Background(), TONTestnet, f.JettonRequest.BodyHash, f.JettonRequest.Recipient, f.JettonRequest.Sender); err == nil || calls != before {
				t.Fatal("unapproved master evidence queried")
			}
		})
	}
}
