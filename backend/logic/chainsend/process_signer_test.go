package chainsend

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
	"github.com/xssnick/tonutils-go/ton/wallet"
)

func TestTestSignerOnlySignsPinnedIntent(t *testing.T) {
	pub, key, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	w := Wallet{PublicKey: pub, SubwalletID: wallet.DefaultSubwallet}
	a, _ := wallet.AddressFromPubKey(pub, wallet.V4R2, w.SubwalletID)
	p := Proposal{Key: "allowed", Recipient: "0:" + strings.Repeat("ab", 32), Atoms: "10000", Seqno: 1, ValidUntil: uint32(time.Now().Add(time.Minute).Unix())}
	body, _ := payload(p, w.SubwalletID, a.StringRaw())
	r := SigningRequest{View: View{Proposal: p, Network: chainassets.TONTestnet, Sender: a.StringRaw(), AssetID: tonAsset, State: Prepared, PayloadHash: hex.EncodeToString(body.Hash())}, PublicKey: hex.EncodeToString(pub), SubwalletID: w.SubwalletID}
	policy := TestPolicy{w, p}
	sig, err := SignTestnet(r, policy, key)
	if err != nil || !ed25519.Verify(pub, body.Hash(), sig) {
		t.Fatal(err)
	}
	for _, edit := range []func(*SigningRequest){func(r *SigningRequest) { r.Atoms = "20000" }, func(r *SigningRequest) { r.Key = "unapproved" }, func(r *SigningRequest) { r.Network = chainassets.TONMainnet }, func(r *SigningRequest) { r.Seqno++ }, func(r *SigningRequest) { r.Recipient = r.Sender }, func(r *SigningRequest) { r.PayloadHash = strings.Repeat("00", 32) }} {
		bad := r
		edit(&bad)
		if _, err = SignTestnet(bad, policy, key); err == nil {
			t.Fatal("unauthorized intent signed")
		}
	}
	// Exercise the actual subprocess protocol, with bounded response and no key in stdin.
	path := filepath.Join(t.TempDir(), "signer")
	response, _ := json.Marshal(SignatureResponse{base64.StdEncoding.EncodeToString(sig)})
	script := "#!/bin/sh\ncat >/dev/null\nprintf '%s' '" + string(response) + "'\n"
	if err = os.WriteFile(path, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	got, err := (ProcessSigner{Executable: path}).SignTON(context.Background(), r)
	if err != nil || !ed25519.Verify(pub, body.Hash(), got) {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, []byte("#!/bin/sh\ncat >/dev/null\necho failed >&2\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err = (ProcessSigner{Executable: path}).SignTON(context.Background(), r); err == nil || !strings.Contains(err.Error(), "failed") {
		t.Fatal(err)
	}
}
