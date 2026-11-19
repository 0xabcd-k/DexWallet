package chainsend

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
	"github.com/xssnick/tonutils-go/ton/wallet"
)

// ProcessSigner invokes a trusted configured executable directly (no shell).
// Its stdin contains public payment facts, stdout one signature, never a key.
// Deployment must enforce OS/service access boundaries; same-UID is test only.
type ProcessSigner struct {
	Executable string
	Args       []string
}
type SignatureResponse struct {
	Signature string `json:"signature"`
}
type signerOutput struct{ bytes.Buffer }

func (b *signerOutput) Write(p []byte) (int, error) {
	const maxSignerOutput = 4096
	if b.Len()+len(p) > maxSignerOutput {
		return 0, errors.New("signer output too large")
	}
	return b.Buffer.Write(p)
}
func (s ProcessSigner) SignTON(ctx context.Context, r SigningRequest) ([]byte, error) {
	raw, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, s.Executable, s.Args...)
	cmd.Stdin = bytes.NewReader(raw)
	var out, diagnostic signerOutput
	cmd.Stdout, cmd.Stderr = &out, &diagnostic
	if err = cmd.Run(); err != nil {
		return nil, fmt.Errorf("signing process failed: %w; %s", err, diagnostic.String())
	}
	var response SignatureResponse
	if json.Unmarshal(out.Bytes(), &response) != nil {
		return nil, ErrInvalidSignature
	}
	sig, err := base64.StdEncoding.DecodeString(response.Signature)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return nil, ErrInvalidSignature
	}
	return sig, nil
}

// TestPolicy authorizes precisely one testnet intent, loaded by the separate
// signing process from operator-owned storage, not from business-process stdin.
type TestPolicy struct {
	Wallet   Wallet   `json:"wallet"`
	Proposal Proposal `json:"proposal"`
}

func SignTestnet(r SigningRequest, p TestPolicy, key ed25519.PrivateKey) ([]byte, error) {
	if len(key) != ed25519.PrivateKeySize || !bytes.Equal(key.Public().(ed25519.PublicKey), p.Wallet.PublicKey) {
		return nil, ErrInvalidSignature
	}
	a, err := wallet.AddressFromPubKey(p.Wallet.PublicKey, wallet.V4R2, p.Wallet.SubwalletID)
	if err != nil {
		return nil, err
	}
	canonical, err := canonicalProposal(p.Proposal, a.StringRaw())
	if err != nil {
		return nil, err
	}
	if r.Proposal != canonical || r.Network != chainassets.TONTestnet || r.AssetID != proposalAsset(canonical) || r.Sender != a.StringRaw() || r.PublicKey != hex.EncodeToString(p.Wallet.PublicKey) || r.SubwalletID != p.Wallet.SubwalletID || r.State != Prepared {
		return nil, ErrConflict
	}
	const testSigningWindow = 15 * time.Minute
	now := time.Now().Unix()
	if int64(r.ValidUntil) <= now || int64(r.ValidUntil) > now+int64(testSigningWindow.Seconds()) {
		return nil, ErrExpired
	}
	if canonical.Jetton.Master != "" {
		allowed, e := rawTONAddress(p.Wallet.JettonMaster)
		if e != nil || allowed != canonical.Jetton.Master {
			return nil, ErrConflict
		}
	}
	body, err := payload(canonical, p.Wallet.SubwalletID, a.StringRaw())
	if err != nil {
		return nil, err
	}
	if hex.EncodeToString(body.Hash()) != r.PayloadHash {
		return nil, ErrConflict
	}
	return ed25519.Sign(key, body.Hash()), nil
}
