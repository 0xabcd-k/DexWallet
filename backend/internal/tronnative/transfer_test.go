package tronnative

import (
	"encoding/hex"
	"encoding/json"
	"math/big"
	"os"
	"strings"
	"testing"
)

// Independent TronWeb output from a real, solidified Nile transfer.
func TestTransferMatchesTronWebAuthorization(t *testing.T) {
	for _, name := range []string{"tron-payout-trx", "tron-payout-usdt", "tron-payout-usdt-fixture"} {
		t.Run(name, func(t *testing.T) { checkTronWebAuthorization(t, name) })
	}
}

func checkTronWebAuthorization(t *testing.T, name string) {
	t.Helper()
	raw, err := os.ReadFile("../../logic/chainreceipt/testdata/" + name + ".json")
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Intent struct {
			Sender, Recipient, RefBlockId, RawHex, TxId, Signature string
			Sun, RefHeight, TimestampMs, ExpirationMs, FeeLimitSun int64
			TokenContract, TokenAtoms                              string
		}
	}
	if err = json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	p := f.Intent
	original := Transfer{Sender: p.Sender, Recipient: p.Recipient, Sun: p.Sun, TokenContract: p.TokenContract, TokenAtoms: p.TokenAtoms, FeeLimitSun: p.FeeLimitSun, Reference: Head{p.RefBlockId, p.RefHeight, p.TimestampMs}, Expiration: p.ExpirationMs}
	signed, err := original.Bind(p.RawHex, p.TxId, p.Signature)
	if err != nil || signed.ID() != p.TxId || signed.TokenContract() != p.TokenContract || !strings.HasSuffix(signed.Hex(), strings.ToLower(p.Signature)) {
		t.Fatal(err)
	}
	for _, mutate := range []func(*Transfer){
		func(t *Transfer) { t.Sun++ }, func(t *Transfer) { t.TokenAtoms = "1001" }, func(t *Transfer) { t.FeeLimitSun++ }, func(t *Transfer) { t.TokenContract = t.Recipient }, func(t *Transfer) { t.Sender, t.Recipient = t.Recipient, t.Sender }, func(t *Transfer) { t.Reference.Timestamp-- }, func(t *Transfer) { t.Expiration-- },
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

func TestTransferDataUint256(t *testing.T) {
	const recipient = "TDHZum2JgP2ZgfnveW9f3VM3UjaafdJXio"
	overflow := new(big.Int).Lsh(big.NewInt(1), 256)
	for _, atoms := range []string{"", "0", "-1", "01", "+1", "1.0", overflow.String()} {
		if _, err := TransferData(recipient, atoms); err == nil {
			t.Fatal("accepted amount", atoms)
		}
	}
	max := overflow.Sub(overflow, big.NewInt(1))
	data, err := TransferData(recipient, max.String())
	if err != nil || len(data) != 68 || hex.EncodeToString(data[36:]) != strings.Repeat("ff", 32) {
		t.Fatal("uint256 maximum", err)
	}
}
