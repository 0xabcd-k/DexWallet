package chainreceipt

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/ton/jetton"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func jettonSendSample(t *testing.T) (TONJettonSentRequest, chainassets.Asset, tonFixture) {
	t.Helper()
	b, err := os.ReadFile("testdata/ton-jetton-send.json")
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		tonFixture
		JettonRequest TONJettonSentRequest
	}
	if err = json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	return f.JettonRequest, chainassets.Asset{ID: f.Request.AssetID, Decimals: 6, Contract: f.JettonRequest.Master, Origin: chainassets.TestFixture}, f.tonFixture
}
func TestTONJettonSentBindsAllThreeHops(t *testing.T) {
	r, asset, f := jettonSendSample(t)
	receipt, sender, message, err := verifyTONJettonSent(r, asset, f.Evidence)
	if err != nil || receipt.Event != f.Receipt.Event || receipt.Transaction != f.Receipt.Transaction || sender == "" || message == "" {
		t.Fatal(receipt, err)
	}
	for _, change := range []func(*TONJettonSentRequest){
		func(r *TONJettonSentRequest) { r.BodyHash = r.BodyHash[2:] + "00" }, func(r *TONJettonSentRequest) { r.Sender = r.Recipient },
		func(r *TONJettonSentRequest) { r.Recipient = r.Sender }, func(r *TONJettonSentRequest) { r.Wallet = r.Sender },
		func(r *TONJettonSentRequest) { r.Atoms = "1" }, func(r *TONJettonSentRequest) { r.QueryID = "1" },
		func(r *TONJettonSentRequest) { r.AttachTON = "49999999" }, func(r *TONJettonSentRequest) { r.ForwardTON = "2" },
	} {
		bad := r
		change(&bad)
		if _, _, _, err = verifyTONJettonSent(bad, asset, f.Evidence); err == nil {
			t.Fatal("different intent reused receipt", bad)
		}
	}
	for _, test := range []struct {
		name    string
		change  func(*chainassets.TONEvidence)
		unknown bool
	}{
		{"missing dispatch", func(e *chainassets.TONEvidence) { e.Dispatch = nil }, true},
		{"missing token sender", func(e *chainassets.TONEvidence) { e.Parent = json.RawMessage(`{"transactions":[]}`) }, true},
		{"missing token credit", func(e *chainassets.TONEvidence) { e.Transaction = json.RawMessage(`{"transactions":[]}`) }, true},
		{"dispatch unfinalized", func(e *chainassets.TONEvidence) {
			editTON(&e.Dispatch, func(tx map[string]any) { tx["finality"] = "pending" })
		}, true},
		{"dispatch failed", func(e *chainassets.TONEvidence) {
			editTON(&e.Dispatch, func(tx map[string]any) { tx["description"].(map[string]any)["aborted"] = true })
		}, false},
		{"sender failed", func(e *chainassets.TONEvidence) {
			editTON(&e.Parent, func(tx map[string]any) { tx["description"].(map[string]any)["aborted"] = true })
		}, false},
		{"credit failed", func(e *chainassets.TONEvidence) {
			editTON(&e.Transaction, func(tx map[string]any) { tx["description"].(map[string]any)["aborted"] = true })
		}, false},
		{"sender mapping swapped", func(e *chainassets.TONEvidence) { e.SenderWallet = e.Wallet }, false},
		{"recipient mapping swapped", func(e *chainassets.TONEvidence) { e.Wallet = e.SenderWallet }, false},
		{"broken first link", func(e *chainassets.TONEvidence) {
			editTON(&e.Dispatch, func(tx map[string]any) { tx["out_msgs"].([]any)[0].(map[string]any)["value"] = "49999999" })
		}, false},
		{"other trace", func(e *chainassets.TONEvidence) {
			editTON(&e.Dispatch, func(tx map[string]any) { tx["trace_id"] = r.BodyHash })
		}, false},
		{"unrelated external authorization", func(e *chainassets.TONEvidence) {
			editTON(&e.Dispatch, func(tx map[string]any) { tx["in_msg"].(map[string]any)["source"] = r.Recipient })
		}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			proof := f.Evidence
			test.change(&proof)
			_, _, _, err := verifyTONJettonSent(r, asset, proof)
			if err == nil || (test.unknown && !errors.Is(err, ErrUnconfirmed)) {
				t.Fatal(err)
			}
		})
	}
}

func TestTONJettonSentRejectsRedirectedExcessAndPayload(t *testing.T) {
	r, asset, f := jettonSendSample(t)
	for _, change := range []func(*jetton.TransferPayload){
		func(p *jetton.TransferPayload) { p.ResponseDestination, _ = address.ParseRawAddr(r.Recipient) },
		func(p *jetton.TransferPayload) { p.CustomPayload = cell.BeginCell().EndCell() },
		func(p *jetton.TransferPayload) { p.ForwardPayload = cell.BeginCell().MustStoreUInt(1, 1).EndCell() },
	} {
		e := f.Evidence
		parent, err := tonOneTransaction(e.Parent)
		if err != nil {
			t.Fatal(err)
		}
		body, err := tonBody(parent.In)
		if err != nil {
			t.Fatal(err)
		}
		var transfer jetton.TransferPayload
		if err = tonPayload(body, &transfer); err != nil {
			t.Fatal(err)
		}
		change(&transfer)
		c, err := tlb.ToCell(transfer)
		if err != nil {
			t.Fatal(err)
		}
		content := map[string]any{"hash": base64.StdEncoding.EncodeToString(c.Hash()), "body": base64.StdEncoding.EncodeToString(c.ToBOC())}
		editTON(&e.Parent, func(tx map[string]any) { tx["in_msg"].(map[string]any)["message_content"] = content })
		editTON(&e.Dispatch, func(tx map[string]any) { tx["out_msgs"].([]any)[0].(map[string]any)["message_content"] = content })
		if _, _, _, err = verifyTONJettonSent(r, asset, e); err == nil {
			t.Fatal("redirected refund/payload accepted")
		}
	}
	e := f.Evidence
	tx, _ := tonOneTransaction(e.Transaction)
	body, _ := tonBody(tx.In)
	var transfer tonInternalTransfer
	if err := tonPayload(body, &transfer); err != nil {
		t.Fatal(err)
	}
	transfer.Response, _ = address.ParseRawAddr(r.Recipient)
	c, err := tlb.ToCell(transfer)
	if err != nil {
		t.Fatal(err)
	}
	setTONIncomingBody(&e, c)
	if _, _, _, err = verifyTONJettonSent(r, asset, e); err == nil {
		t.Fatal("recipient accepted changed excess destination")
	}
}
