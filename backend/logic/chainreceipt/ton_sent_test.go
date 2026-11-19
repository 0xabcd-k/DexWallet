package chainreceipt

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"

	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
)

func TestTONSentBindsExternalBodyToFinalizedRecipient(t *testing.T) {
	f, _ := tonSample(t, "ton-no-state")
	parent, err := tonOneTransaction(f.Evidence.Parent)
	if err != nil {
		t.Fatal(err)
	}
	if err = normalizeTONTransaction(chainassets.TONTestnet, &parent, true); err != nil {
		t.Fatal(err)
	}
	body, err := tonBody(parent.In)
	if err != nil {
		t.Fatal(err)
	}
	recipient, err := tonAddress(chainassets.TONTestnet, f.Request.Recipient)
	if err != nil {
		t.Fatal(err)
	}
	request := TONSentRequest{parent.Account, recipient, f.Request.Atoms, hex.EncodeToString(body.Hash())}
	// This historical bootstrap deployed the wallet. The sending channel rejects
	// deployment; derive a no-deployment fake only for the correlation branches.
	if _, _, _, err = verifyTONSent(request, f.Evidence); !errors.Is(err, ErrConflict) {
		t.Fatal("deployment accepted", err)
	}
	editTON(&f.Evidence.Parent, func(tx map[string]any) { tx["in_msg"].(map[string]any)["init_state"] = nil })
	got, sender, message, err := verifyTONSent(request, f.Evidence)
	if err != nil || got.Event != f.Receipt.Event || sender != parent.Hash || message != (*parent.Out)[0].Hash {
		t.Fatal(got, sender, message, err)
	}
	for _, change := range []func(*TONSentRequest){func(r *TONSentRequest) { r.BodyHash = "00" + r.BodyHash[2:] }, func(r *TONSentRequest) { r.Sender = r.Recipient }, func(r *TONSentRequest) { r.Atoms = "1" }, func(r *TONSentRequest) { r.Recipient = r.Sender }} {
		r := request
		change(&r)
		if _, _, _, err = verifyTONSent(r, f.Evidence); err == nil {
			t.Fatal("unrelated transfer accepted")
		}
	}
	for _, test := range []struct {
		name   string
		change func(*chainassets.TONEvidence)
	}{
		{"no sender", func(e *chainassets.TONEvidence) { e.Parent = json.RawMessage(`{"transactions":[]}`) }},
		{"no recipient", func(e *chainassets.TONEvidence) { e.Transaction = json.RawMessage(`{"transactions":[]}`) }},
		{"pending", func(e *chainassets.TONEvidence) {
			editTON(&e.Parent, func(tx map[string]any) { tx["finality"] = "pending" })
		}},
		{"failed", func(e *chainassets.TONEvidence) {
			editTON(&e.Parent, func(tx map[string]any) { tx["description"].(map[string]any)["aborted"] = true })
		}},
		{"extra output", func(e *chainassets.TONEvidence) {
			editTON(&e.Parent, func(tx map[string]any) { tx["out_msgs"] = append(tx["out_msgs"].([]any), tx["out_msgs"].([]any)[0]) })
		}},
		{"unexpected bounce", func(e *chainassets.TONEvidence) { replaceTONIncoming(e, func(m map[string]any) { m["bounce"] = true }) }},
		{"body corruption", func(e *chainassets.TONEvidence) {
			editTON(&e.Parent, func(tx map[string]any) {
				tx["in_msg"].(map[string]any)["message_content"].(map[string]any)["body"] = "bad"
			})
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			e := f.Evidence
			test.change(&e)
			if _, _, _, err := verifyTONSent(request, e); err == nil {
				t.Fatal("invalid evidence accepted")
			} else if test.name == "no recipient" && !errors.Is(err, ErrUnconfirmed) {
				t.Fatal(err)
			}
		})
	}
}

func TestTONRealV4SendingReceipt(t *testing.T) {
	f, _ := tonSample(t, "ton-v4-send")
	parent, err := tonOneTransaction(f.Evidence.Parent)
	if err != nil {
		t.Fatal(err)
	}
	if err = normalizeTONTransaction(chainassets.TONTestnet, &parent, true); err != nil {
		t.Fatal(err)
	}
	body, err := tonBody(parent.In)
	if err != nil {
		t.Fatal(err)
	}
	result, sender, message, err := verifyTONSent(TONSentRequest{Sender: parent.Account, Recipient: f.Request.Recipient, Atoms: f.Request.Atoms, BodyHash: hex.EncodeToString(body.Hash())}, f.Evidence)
	if err != nil || result.Transaction != f.Receipt.Transaction || result.Event != f.Receipt.Event || sender != parent.Hash || message != (*parent.Out)[0].Hash {
		t.Fatal(result, err)
	}
}
