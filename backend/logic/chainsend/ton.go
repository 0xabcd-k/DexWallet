// Package chainsend keeps a TON sending attempt immutable across process restarts.
// It does not authorize withdrawals, hold user funds, or declare chain settlement.
package chainsend

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"strings"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
	model "github.com/0xabcd-k/DexWallet/backend/model/chainsend"
	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/ton/wallet"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

const (
	Prepared             = "prepared"
	Signed               = "signed"
	BroadcastUnknown     = "broadcast_unknown"
	Submitted            = "submitted" // Node acceptance only, never recipient delivery.
	tonAsset             = chainassets.TONTestnet + ":ton"
	tonTransferOperation = 0
)

var (
	ErrInvalidRequest   = errors.New("invalid TON send proposal")
	ErrConflict         = errors.New("send proposal differs from the reserved intent")
	ErrSeqnoReserved    = errors.New("TON wallet seqno already reserved by another intent")
	ErrInvalidSignature = errors.New("signer did not sign the exact reserved payload")
	ErrExpired          = errors.New("send validity expired; original execution must still be reconciled")
	ErrOutcomeUnknown   = errors.New("send persistence or broadcast outcome unknown; retain original intent")
)
var requestKey = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,64}$`)

// Wallet is trusted deployment configuration for a dedicated TESTNET V4R2 key.
// The store and executor receive no private key. Mainnet is deliberately absent.
type Wallet struct {
	PublicKey   ed25519.PublicKey
	SubwalletID uint32
}
type Proposal struct {
	Key        string `json:"key"`
	Recipient  string `json:"recipient"`
	Atoms      string `json:"atoms"`
	Seqno      uint32 `json:"seqno"`
	ValidUntil uint32 `json:"validUntil"`
}
type View struct {
	Proposal
	Network     string `json:"network"`
	Sender      string `json:"sender"`
	AssetID     string `json:"assetId"`
	State       string `json:"state"`
	PayloadHash string `json:"payloadHash"`
	MessageHash string `json:"messageHash,omitempty"`
	BodyHash    string `json:"bodyHash,omitempty"`
	Attempts    int64  `json:"attempts"`
	LastError   string `json:"lastError,omitempty"`
}
type SigningRequest struct {
	View
	PublicKey   string
	SubwalletID uint32
	PayloadBOC  []byte `json:"-"`
}

// Signer is an isolated capability: signs only, never submits anything on chain.
// A production adapter must independently enforce its wallet/intent policy.
type Signer interface {
	SignTON(context.Context, SigningRequest) ([]byte, error)
}

// Broadcaster must use its configured testnet endpoint and check network identity.
// nil means node acceptance, NOT execution or receipt confirmation.
type Broadcaster interface {
	SubmitTON(context.Context, string, []byte) error
}
type TON struct {
	store       *Store
	wallet      Wallet
	sender      string
	signer      Signer
	broadcaster Broadcaster
	now         func() time.Time
}

func NewTON(store *Store, w Wallet, signer Signer, broadcaster Broadcaster) (*TON, error) {
	if store == nil || len(w.PublicKey) != ed25519.PublicKeySize || signer == nil || broadcaster == nil {
		return nil, ErrInvalidRequest
	}
	w.PublicKey = append(ed25519.PublicKey(nil), w.PublicKey...)
	a, err := wallet.AddressFromPubKey(w.PublicKey, wallet.V4R2, w.SubwalletID)
	if err != nil {
		return nil, err
	}
	return &TON{store, w, a.StringRaw(), signer, broadcaster, time.Now}, nil
}
func canonicalProposal(p Proposal, sender string) (Proposal, error) {
	if !requestKey.MatchString(p.Key) || p.Seqno == 0 || p.ValidUntil == 0 || len(p.Atoms) > 39 || strings.TrimSpace(p.Recipient) != p.Recipient {
		return p, ErrInvalidRequest
	}
	n, ok := new(big.Int).SetString(p.Atoms, 10)
	if !ok || n.Sign() <= 0 || n.BitLen() > 120 || n.String() != p.Atoms {
		return p, ErrInvalidRequest
	}
	var a *address.Address
	var err error
	if strings.Contains(p.Recipient, ":") {
		a, err = address.ParseRawAddr(p.Recipient)
	} else {
		a, err = address.ParseAddr(p.Recipient)
	}
	if err != nil || a.Type() != address.StdAddress || a.Workchain() != 0 {
		return p, ErrInvalidRequest
	}
	p.Recipient = a.StringRaw()
	if p.Recipient == sender || bytes.Equal(a.Data(), make([]byte, 32)) {
		return p, ErrInvalidRequest
	}
	return p, nil
}

// The SDK's regular BuildMessage reads its own clock/seqno on every call. Here
// the exact V4R2 payload must instead be fixed BEFORE asking the signer. Cell,
// TL-B serialization and address derivation still use the existing TON SDK.
func payload(p Proposal, subwallet uint32) (*cell.Cell, error) {
	a, err := address.ParseRawAddr(p.Recipient)
	if err != nil {
		return nil, err
	}
	n, ok := new(big.Int).SetString(p.Atoms, 10)
	if !ok {
		return nil, ErrInvalidRequest
	}
	msg, err := tlb.ToCell(&tlb.InternalMessage{IHRDisabled: true, Bounce: false, DstAddr: a, Amount: tlb.FromNanoTON(n), Body: cell.BeginCell().EndCell()})
	if err != nil {
		return nil, err
	}
	return cell.BeginCell().MustStoreUInt(uint64(subwallet), 32).MustStoreUInt(uint64(p.ValidUntil), 32).MustStoreUInt(uint64(p.Seqno), 32).MustStoreUInt(tonTransferOperation, 8).MustStoreUInt(wallet.PayGasSeparately|wallet.IgnoreErrors, 8).MustStoreRef(msg).EndCell(), nil
}
func (s *TON) Prepare(ctx context.Context, p Proposal) (View, error) {
	p, err := canonicalProposal(p, s.sender)
	if err != nil {
		return View{}, err
	}
	body, err := payload(p, s.wallet.SubwalletID)
	if err != nil {
		return View{}, err
	}
	row := &model.ChainSendIntent{Network: chainassets.TONTestnet, RequestKey: p.Key, Sender: s.sender, Recipient: p.Recipient, AssetId: tonAsset, AmountAtoms: p.Atoms, PublicKey: hex.EncodeToString(s.wallet.PublicKey), SubwalletId: int64(s.wallet.SubwalletID), Seqno: int64(p.Seqno), ValidUntil: int64(p.ValidUntil), PayloadHash: hex.EncodeToString(body.Hash()), State: Prepared}
	saved, err := s.store.prepare(ctx, row)
	if err != nil {
		return View{}, err
	}
	return view(saved), nil
}
func view(r *model.ChainSendIntent) View {
	return View{Proposal: Proposal{r.RequestKey, r.Recipient, r.AmountAtoms, uint32(r.Seqno), uint32(r.ValidUntil)}, Network: r.Network, Sender: r.Sender, AssetID: r.AssetId, State: r.State, PayloadHash: r.PayloadHash, MessageHash: r.MessageHash, BodyHash: r.BodyHash, Attempts: r.Attempts, LastError: r.LastError}
}
func (s *TON) checkedPayload(r *model.ChainSendIntent) (*cell.Cell, error) {
	if r.Network != chainassets.TONTestnet || r.AssetId != tonAsset || r.Sender != s.sender || r.PublicKey != hex.EncodeToString(s.wallet.PublicKey) || r.SubwalletId != int64(s.wallet.SubwalletID) {
		return nil, ErrConflict
	}
	p, err := canonicalProposal(view(r).Proposal, s.sender)
	if err != nil {
		return nil, err
	}
	c, err := payload(p, s.wallet.SubwalletID)
	if err != nil {
		return nil, err
	}
	if hex.EncodeToString(c.Hash()) != r.PayloadHash {
		return nil, ErrConflict
	}
	return c, nil
}
func envelope(sender string, body *cell.Cell, signature []byte) ([]byte, string, string, error) {
	signed := cell.BeginCell().MustStoreSlice(signature, 512).MustStoreBuilder(body.ToBuilder()).EndCell()
	a, err := address.ParseRawAddr(sender)
	if err != nil {
		return nil, "", "", err
	}
	external, err := tlb.ToCell(&tlb.ExternalMessage{DstAddr: a, Body: signed})
	if err != nil {
		return nil, "", "", err
	}
	return external.ToBOC(), hex.EncodeToString(external.Hash()), hex.EncodeToString(signed.Hash()), nil
}
func (s *TON) checkedArtifact(r *model.ChainSendIntent, body *cell.Cell) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(r.SignedBoc.String)
	if err != nil || !r.SignedBoc.Valid {
		return nil, ErrConflict
	}
	root, err := cell.FromBOC(raw)
	if err != nil {
		return nil, ErrConflict
	}
	var ext tlb.ExternalMessage
	if err = tlb.LoadFromCell(&ext, root.BeginParse()); err != nil || ext.Body == nil {
		return nil, ErrConflict
	}
	sig, err := ext.Body.BeginParse().LoadSlice(512)
	if err != nil || !ed25519.Verify(s.wallet.PublicKey, body.Hash(), sig) {
		return nil, ErrInvalidSignature
	}
	expected, messageHash, bodyHash, err := envelope(s.sender, body, sig)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(raw, expected) || r.MessageHash != messageHash || r.BodyHash != bodyHash {
		return nil, ErrConflict
	}
	return raw, nil
}

// Execute makes one explicit attempt. RPC/signing never runs inside a DB tx.
// A repeated call can submit ONLY the identical persisted BoC. No re-signing with
// a fresh seqno/expiry, refund, final settlement or automatic retry occurs here.
func (s *TON) Execute(ctx context.Context, key string) (View, error) {
	r, err := s.store.load(ctx, key)
	if err != nil {
		return View{}, err
	}
	body, err := s.checkedPayload(r)
	if err != nil {
		return view(r), err
	}
	if r.State == Submitted {
		return view(r), nil
	}
	if s.now().Unix() >= r.ValidUntil {
		return view(r), ErrExpired
	}
	if r.State == Prepared {
		signature, err := s.signer.SignTON(ctx, SigningRequest{View: view(r), PublicKey: r.PublicKey, SubwalletID: s.wallet.SubwalletID, PayloadBOC: body.ToBOC()})
		if err != nil {
			return view(r), fmt.Errorf("sign TON reserved payload: %w", err)
		}
		if !ed25519.Verify(s.wallet.PublicKey, body.Hash(), signature) {
			return view(r), ErrInvalidSignature
		}
		raw, hash, bodyHash, err := envelope(s.sender, body, signature)
		if err != nil {
			return view(r), err
		}
		r, err = s.store.saveSigned(ctx, r, raw, hash, bodyHash)
		if err != nil {
			return View{}, err
		}
	}
	raw, err := s.checkedArtifact(r, body)
	if err != nil {
		return view(r), err
	}
	r, send, err := s.store.beginBroadcast(ctx, r)
	if err != nil {
		return View{}, err
	}
	if !send {
		return view(r), nil
	}
	// Cancellation after the durable marker is still an unknown attempt, never a
	// reason to replace the message. Passing a fresh slice keeps the stored copy safe.
	err = s.broadcaster.SubmitTON(ctx, r.Network, raw)
	if err != nil {
		noteErr := s.store.broadcastResult(ctx, r.Id, false, err.Error())
		return view(r), fmt.Errorf("%w: %w", ErrOutcomeUnknown, errors.Join(err, noteErr))
	}
	if err = s.store.broadcastResult(ctx, r.Id, true, ""); err != nil {
		return view(r), fmt.Errorf("%w: %w", ErrOutcomeUnknown, err)
	}
	return s.store.Find(ctx, key)
}
