package chainsend

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/ton/jetton"
	"github.com/xssnick/tonutils-go/ton/wallet"
)

func TestTONJettonAuthorizationPinsPrincipalAndTONBudget(t *testing.T) {
	pub, key, _ := ed25519.GenerateKey(nil)
	w := Wallet{PublicKey: pub, SubwalletID: wallet.DefaultSubwallet, JettonMaster: "0:" + strings.Repeat("cd", 32)}
	a, _ := wallet.AddressFromPubKey(pub, wallet.V4R2, w.SubwalletID)
	p := Proposal{Key: "jetton", Recipient: "0:" + strings.Repeat("ab", 32), Atoms: "100000", Seqno: 3, ValidUntil: uint32(time.Now().Add(time.Minute).Unix()), Jetton: JettonTransfer{Master: w.JettonMaster, Wallet: "0:" + strings.Repeat("ef", 32), AttachTON: "50000000", ForwardTON: "1", QueryID: "2026111901"}}
	p, err := canonicalProposal(p, a.StringRaw())
	if err != nil {
		t.Fatal(err)
	}
	body, err := payload(p, w.SubwalletID, a.StringRaw())
	if err != nil {
		t.Fatal(err)
	}
	slice := body.BeginParse()
	slice.MustLoadSlice(104)
	if slice.MustLoadUInt(8) != 3 {
		t.Fatal("wrong send mode")
	}
	var message tlb.InternalMessage
	if err = tlb.LoadFromCell(&message, slice.MustLoadRef()); err != nil {
		t.Fatal(err)
	}
	var transfer jetton.TransferPayload
	if err = tlb.LoadFromCell(&transfer, message.Body.BeginParse()); err != nil {
		t.Fatal(err)
	}
	if message.DstAddr.StringRaw() != p.Jetton.Wallet || !message.Bounce || message.Amount.Nano().String() != p.Jetton.AttachTON || message.StateInit != nil || transfer.Destination.StringRaw() != p.Recipient || transfer.ResponseDestination.StringRaw() != a.StringRaw() || transfer.Amount.Nano().String() != p.Atoms || transfer.QueryID != 2026111901 || transfer.ForwardTONAmount.Nano().String() != "1" || transfer.CustomPayload != nil || transfer.ForwardPayload.BitsSize() != 0 || transfer.ForwardPayload.RefsNum() != 0 || slice.BitsLeft() != 0 || slice.RefsNum() != 0 {
		t.Fatal("unexpected token transfer or extra action")
	}
	request := SigningRequest{View: View{Proposal: p, Network: chainassets.TONTestnet, Sender: a.StringRaw(), AssetID: proposalAsset(p), State: Prepared, PayloadHash: hex.EncodeToString(body.Hash())}, PublicKey: hex.EncodeToString(pub), SubwalletID: w.SubwalletID}
	policy := TestPolicy{w, p}
	sig, err := SignTestnet(request, policy, key)
	if err != nil || !ed25519.Verify(pub, body.Hash(), sig) {
		t.Fatal(err)
	}
	for _, change := range []func(*Proposal){
		func(p *Proposal) { p.Atoms = "100001" }, func(p *Proposal) { p.Recipient = p.Jetton.Master },
		func(p *Proposal) { p.Jetton.Master = p.Recipient }, func(p *Proposal) { p.Jetton.Wallet = p.Recipient },
		func(p *Proposal) { p.Jetton.AttachTON = "50000001" }, func(p *Proposal) { p.Jetton.ForwardTON = "2" }, func(p *Proposal) { p.Jetton.QueryID = "2026111902" },
	} {
		bad := request
		change(&bad.Proposal)
		if _, err = SignTestnet(bad, policy, key); err == nil {
			t.Fatal("changed policy signed")
		}
	}
	for _, change := range []func(*Proposal){
		func(p *Proposal) { p.Jetton.Master = "" }, func(p *Proposal) { p.Jetton.Wallet = a.StringRaw() },
		func(p *Proposal) { p.Jetton.AttachTON = "0" }, func(p *Proposal) { p.Jetton.ForwardTON = p.Jetton.AttachTON },
		func(p *Proposal) { p.Jetton.QueryID = "0" }, func(p *Proposal) { p.Jetton.QueryID = "01" }, func(p *Proposal) { p.Jetton.QueryID = "18446744073709551616" },
	} {
		bad := p
		change(&bad)
		if _, err = canonicalProposal(bad, a.StringRaw()); err == nil {
			t.Fatal("ambiguous budget accepted")
		}
	}
	wrong := policy
	wrong.Wallet.JettonMaster = p.Recipient
	if _, err = SignTestnet(request, wrong, key); err == nil {
		t.Fatal("wrong master allowlist accepted")
	}
}

type jettonResolverFake struct {
	derived string
	calls   int
}

func (f *jettonResolverFake) SubmitTON(context.Context, string, []byte) error { return nil }
func (f *jettonResolverFake) ResolveTONJettonWallet(context.Context, string, string) (string, error) {
	f.calls++
	return f.derived, nil
}
func TestTONJettonRequiresTrustedWalletDerivation(t *testing.T) {
	j := JettonTransfer{Master: "master", Wallet: "derived"}
	fake := &jettonResolverFake{derived: "wrong"}
	s := &TON{wallet: Wallet{JettonMaster: j.Master}, broadcaster: fake}
	if err := s.checkJetton(context.Background(), Proposal{Jetton: j}); err == nil {
		t.Fatal("unrelated token wallet accepted")
	}
	fake.derived = j.Wallet
	if err := s.checkJetton(context.Background(), Proposal{Jetton: j}); err != nil {
		t.Fatal(err)
	}
	s.wallet.JettonMaster = "other"
	if err := s.checkJetton(context.Background(), Proposal{Jetton: j}); err == nil || fake.calls != 2 {
		t.Fatal("unapproved master reached RPC")
	}
}
