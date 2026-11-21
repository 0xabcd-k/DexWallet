package tronnative

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// Independent TronWeb output from a real, solidified Nile transfer.
func TestNativeTransferMatchesTronWebAuthorization(t *testing.T) {
	raw, err := os.ReadFile("../../logic/chainreceipt/testdata/tron-payout-trx.json")
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Intent struct {
			Sender, Recipient, RefBlockId, RawHex, TxId, Signature string
			Sun, RefHeight, TimestampMs, ExpirationMs              int64
		}
	}
	if err = json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	p := f.Intent
	original := Transfer{p.Sender, p.Recipient, p.Sun, Head{p.RefBlockId, p.RefHeight, p.TimestampMs}, p.ExpirationMs}
	signed, err := original.Bind(p.RawHex, p.TxId, p.Signature)
	if err != nil || signed.ID() != p.TxId || !strings.HasSuffix(signed.Hex(), strings.ToLower(p.Signature)) {
		t.Fatal(err)
	}
	for _, mutate := range []func(*Transfer){
		func(t *Transfer) { t.Sun++ }, func(t *Transfer) { t.Sender, t.Recipient = t.Recipient, t.Sender }, func(t *Transfer) { t.Reference.Timestamp-- }, func(t *Transfer) { t.Expiration-- },
		func(t *Transfer) {
			id, _ := hex.DecodeString(t.Reference.ID)
			id[8] ^= 1
			t.Reference.ID = hex.EncodeToString(id)
		},
	} {
		v := original
		mutate(&v)
		if _, err := v.Bind(p.RawHex, p.TxId, p.Signature); err == nil {
			t.Fatal("modified authorization accepted")
		}
	}
	rawData, _ := hex.DecodeString(p.RawHex)
	withMemo := appendBytes(rawData, 10, []byte("additional payload"))
	if _, err := original.Bind(hex.EncodeToString(withMemo), TxID(withMemo), p.Signature); err == nil {
		t.Fatal("extra memo accepted")
	}
	sig, _ := hex.DecodeString(p.Signature)
	sig[10] ^= 1
	if _, err := original.Bind(p.RawHex, p.TxId, hex.EncodeToString(sig)); err == nil {
		t.Fatal("incorrect signer accepted")
	}
	for _, invalid := range []string{"", strings.Repeat("0", 130), p.Signature + "00", p.Signature[:128] + "ff"} {
		if _, err := original.Bind(p.RawHex, p.TxId, invalid); err == nil {
			t.Fatal("invalid signature accepted")
		}
	}
}
