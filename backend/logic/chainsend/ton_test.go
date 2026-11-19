package chainsend

import (
	"bytes"
	"crypto/ed25519"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	model "github.com/0xabcd-k/DexWallet/backend/model/chainsend"
	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/ton/wallet"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func TestTONPayloadAndSignatureBindOneNativeTransfer(t *testing.T) {
	pub, key, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	a, err := wallet.AddressFromPubKey(pub, wallet.V4R2, wallet.DefaultSubwallet)
	if err != nil {
		t.Fatal(err)
	}
	p := Proposal{"test-order", "0:" + strings.Repeat("ab", 32), "10000000", 7, 2000000000}
	body, err := payload(p, wallet.DefaultSubwallet)
	if err != nil {
		t.Fatal(err)
	}
	sig := ed25519.Sign(key, body.Hash())
	raw, hash, bodyHash, err := envelope(a.StringRaw(), body, sig)
	if err != nil {
		t.Fatal(err)
	}
	root, err := cell.FromBOC(raw)
	if err != nil {
		t.Fatal(err)
	}
	var external tlb.ExternalMessage
	if err = tlb.LoadFromCell(&external, root.BeginParse()); err != nil {
		t.Fatal(err)
	}
	if external.DstAddr.StringRaw() != a.StringRaw() || external.StateInit != nil {
		t.Fatal("wrong wallet or unexpected deployment")
	}
	s := external.Body.BeginParse()
	gotSig := s.MustLoadSlice(512)
	if !ed25519.Verify(pub, body.Hash(), gotSig) || s.MustLoadUInt(32) != wallet.DefaultSubwallet || s.MustLoadUInt(32) != uint64(p.ValidUntil) || s.MustLoadUInt(32) != uint64(p.Seqno) || s.MustLoadUInt(8) != 0 || s.MustLoadUInt(8) != 3 {
		t.Fatal("wrong wallet authorization")
	}
	var transfer tlb.InternalMessage
	if err = tlb.LoadFromCell(&transfer, s.MustLoadRef()); err != nil {
		t.Fatal(err)
	}
	if transfer.DstAddr.StringRaw() != p.Recipient || transfer.Amount.Nano().String() != p.Atoms || transfer.Bounce || transfer.StateInit != nil || transfer.Body.BitsSize() != 0 || transfer.Body.RefsNum() != 0 || s.BitsLeft() != 0 || s.RefsNum() != 0 {
		t.Fatal("unexpected payment or extra action")
	}
	r := &model.ChainSendIntent{SignedBoc: sql.NullString{String: base64.StdEncoding.EncodeToString(raw), Valid: true}, MessageHash: hash, BodyHash: bodyHash}
	executor := &TON{wallet: Wallet{pub, wallet.DefaultSubwallet}, sender: a.StringRaw()}
	if got, err := executor.checkedArtifact(r, body); err != nil || !bytes.Equal(got, raw) {
		t.Fatal(err)
	}
	// Each business fact changes what is authorized, even when the signer has the right key.
	for _, edit := range []func(*Proposal){
		func(p *Proposal) { p.Atoms = "10000001" }, func(p *Proposal) { p.Recipient = "0:" + strings.Repeat("cd", 32) },
		func(p *Proposal) { p.Seqno++ }, func(p *Proposal) { p.ValidUntil++ },
	} {
		other := p
		edit(&other)
		changed, err := payload(other, wallet.DefaultSubwallet)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = executor.checkedArtifact(r, changed); !errors.Is(err, ErrInvalidSignature) {
			t.Fatal("altered payment accepted", err)
		}
	}
	// Changing the external destination leaves the signed body valid but must still fail.
	wrong, _, _, err := envelope(p.Recipient, body, sig)
	if err != nil {
		t.Fatal(err)
	}
	r.SignedBoc.String = base64.StdEncoding.EncodeToString(wrong)
	if _, err = executor.checkedArtifact(r, body); !errors.Is(err, ErrConflict) {
		t.Fatal("wrong external destination accepted", err)
	}
	r.SignedBoc.String = base64.StdEncoding.EncodeToString(raw)
	r.MessageHash = hex.EncodeToString(make([]byte, 32))
	if _, err = executor.checkedArtifact(r, body); !errors.Is(err, ErrConflict) {
		t.Fatal("wrong hash accepted", err)
	}
}

func TestTONProposalRejectsAmbiguityAndUnsupportedMessages(t *testing.T) {
	p := Proposal{"order-1", "0:" + strings.Repeat("ab", 32), "1", 1, 2000000000}
	sender := "0:" + strings.Repeat("cd", 32)
	for _, edit := range []func(*Proposal){
		func(p *Proposal) { p.Key = "" }, func(p *Proposal) { p.Key = "other order" },
		func(p *Proposal) { p.Atoms = "0" }, func(p *Proposal) { p.Atoms = "-1" }, func(p *Proposal) { p.Atoms = "01" }, func(p *Proposal) { p.Atoms = "1.5" }, func(p *Proposal) { p.Atoms = "+1" }, func(p *Proposal) { p.Atoms = strings.Repeat("9", 39) },
		func(p *Proposal) { p.Recipient = sender }, func(p *Proposal) { p.Recipient = "0:" + strings.Repeat("00", 32) }, func(p *Proposal) { p.Recipient = "-1:" + strings.Repeat("ab", 32) }, func(p *Proposal) { p.Recipient = " " + p.Recipient }, func(p *Proposal) { p.Recipient = "bad" },
		func(p *Proposal) { p.Seqno = 0 }, func(p *Proposal) { p.ValidUntil = 0 },
	} {
		bad := p
		edit(&bad)
		if _, err := canonicalProposal(bad, sender); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("accepted %+v: %v", bad, err)
		}
	}
	a, err := address.ParseRawAddr(p.Recipient)
	if err != nil {
		t.Fatal(err)
	}
	friendly := p
	friendly.Recipient = a.String()
	if got, err := canonicalProposal(friendly, sender); err != nil || got != p {
		t.Fatal(got, err)
	}
}
