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
	"strings"
	"testing"
	"time"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func TestTONSubmitValidatesNetworkAndAcknowledgment(t *testing.T) {
	a, _ := address.ParseRawAddr("0:" + strings.Repeat("ab", 32))
	message, err := tlb.ToCell(&tlb.ExternalMessage{DstAddr: a, Body: cell.BeginCell().EndCell()})
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"accepted", "wrong network", "rejected", "wrong hash", "http failure"} {
		t.Run(mode, func(t *testing.T) {
			posts := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v2/getMasterchainInfo" {
					root := TONTestnetZeroState
					if mode == "wrong network" {
						root = TONMainnetZeroState
					}
					fmt.Fprintf(w, `{"ok":true,"result":{"init":{"root_hash":%q}}}`, root)
					return
				}
				if r.URL.Path != "/api/v2/sendBocReturnHash" || r.Method != "POST" {
					t.Error(r.Method, r.URL.Path)
					http.Error(w, "unexpected", 400)
					return
				}
				posts++
				var body struct {
					BOC string `json:"boc"`
				}
				_ = json.NewDecoder(r.Body).Decode(&body)
				raw, err := base64.StdEncoding.DecodeString(body.BOC)
				if err != nil || !bytes.Equal(raw, message.ToBOC()) {
					t.Error("message changed")
				}
				if mode == "http failure" {
					http.Error(w, "unavailable", 503)
					return
				}
				if mode == "rejected" {
					fmt.Fprint(w, `{"ok":false,"error":"rejected"}`)
					return
				}
				hash := message.Hash()
				if mode == "wrong hash" {
					hash = make([]byte, 32)
				}
				fmt.Fprintf(w, `{"ok":true,"result":{"hash":%q}}`, base64.StdEncoding.EncodeToString(hash))
			}))
			defer srv.Close()
			s, err := New(Config{Timeout: time.Second, Networks: []Endpoint{{Network: TONTestnet, URL: srv.URL, APIKey: "test"}}}, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			err = s.SubmitTON(context.Background(), TONTestnet, message.ToBOC())
			if (err == nil) != (mode == "accepted") {
				t.Fatal(err)
			}
			want := 1
			if mode == "wrong network" {
				want = 0
			}
			if posts != want {
				t.Fatal("unexpected retry or pre-network submission", posts)
			}
			if err = s.SubmitTON(context.Background(), TONMainnet, message.ToBOC()); err == nil || posts != want {
				t.Fatal("mainnet submission accepted")
			}
		})
	}
}
func TestTONSendLookupUsesBodyAndKeepsUnknown(t *testing.T) {
	raw, err := os.ReadFile("../chainreceipt/testdata/ton-no-state.json")
	if err != nil {
		t.Fatal(err)
	}
	var f struct{ Evidence TONEvidence }
	if err = json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	for _, empty := range []bool{false, true} {
		calls := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			switch r.URL.Path {
			case "/api/v2/getMasterchainInfo":
				w.Write(f.Evidence.Masterchain)
			case "/api/v3/transactionsByMessage":
				if r.URL.Query().Get("direction") != "in" || r.URL.Query().Get("body_hash") != strings.Repeat("ab", 32) {
					t.Error("wrong lookup")
				}
				if empty {
					fmt.Fprint(w, `{"transactions":[]}`)
				} else {
					w.Write(f.Evidence.Parent)
				}
			case "/api/v3/adjacentTransactions":
				if r.URL.Query().Get("direction") != "out" {
					t.Error("wrong adjacency")
				}
				w.Write(f.Evidence.Transaction)
			case "/api/v3/blocks":
				w.Write(f.Evidence.Block)
			default:
				t.Error(r.URL.Path)
			}
		}))
		s, err := New(Config{Timeout: time.Second, Networks: []Endpoint{{Network: TONTestnet, URL: srv.URL, APIKey: "test"}}}, nil)
		if err != nil {
			t.Fatal(err)
		}
		got, err := s.ReadTONSendEvidence(context.Background(), TONTestnet, strings.Repeat("ab", 32))
		if err != nil {
			t.Fatal(err)
		}
		if empty {
			if calls != 2 || len(got.Transaction) != 0 {
				t.Fatal(calls)
			}
		} else if calls != 4 || !bytes.Equal(got.Parent, f.Evidence.Parent) || !bytes.Equal(got.Transaction, f.Evidence.Transaction) {
			t.Fatal("lost chain evidence", calls)
		}
		s.Close()
		srv.Close()
	}
}
