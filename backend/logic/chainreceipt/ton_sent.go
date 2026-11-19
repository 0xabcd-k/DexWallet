package chainreceipt

import (
	"context"
	"encoding/hex"
	"fmt"

	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
)

type TONSentRequest struct{ Sender, Recipient, Atoms, BodyHash string }
type TONSentStored struct {
	Stored
	SenderHash  string `json:"senderHash"`
	MessageHash string `json:"messageHash"`
}
type tonSendSource interface {
	ReadTONSendEvidence(context.Context, string, string) (chainassets.TONEvidence, error)
}

// RecordSent binds the saved signed request to one finalized native TON receipt.
// It persists chain facts only, not an order allocation or a ledger settlement.
func (s *TONService) RecordSent(ctx context.Context, r TONSentRequest) (TONSentStored, error) {
	source, ok := s.verifier.source.(tonSendSource)
	if !ok {
		return TONSentStored{}, ErrUnsupported
	}
	var err error
	r.Sender, err = tonAddress(chainassets.TONTestnet, r.Sender)
	if err != nil {
		return TONSentStored{}, ErrInvalidRequest
	}
	r.Recipient, err = tonAddress(chainassets.TONTestnet, r.Recipient)
	if err != nil {
		return TONSentStored{}, ErrInvalidRequest
	}
	r.BodyHash, err = tonHash(r.BodyHash)
	if err != nil {
		return TONSentStored{}, ErrInvalidRequest
	}
	if _, err = tonNumber(r.Atoms, 120, true); err != nil {
		return TONSentStored{}, ErrInvalidRequest
	}
	evidence, err := source.ReadTONSendEvidence(ctx, chainassets.TONTestnet, r.BodyHash)
	if err != nil {
		return TONSentStored{}, err
	}
	receipt, sender, message, err := verifyTONSent(r, evidence)
	if err != nil {
		return TONSentStored{}, err
	}
	stored, err := s.store.record(ctx, receipt)
	return TONSentStored{Stored: stored, SenderHash: sender, MessageHash: message}, err
}
func verifyTONSent(r TONSentRequest, e chainassets.TONEvidence) (Receipt, string, string, error) {
	parent, err := tonOneTransaction(e.Parent)
	if err != nil {
		return Receipt{}, "", "", err
	}
	if err = normalizeTONTransaction(chainassets.TONTestnet, &parent, true); err != nil {
		return Receipt{}, "", "", err
	}
	if err = tonSuccessful(parent); err != nil {
		return Receipt{}, "", "", err
	}
	body, err := tonBody(parent.In)
	if err != nil {
		return Receipt{}, "", "", err
	}
	if parent.Account != r.Sender || parent.In.Source != "" || parent.In.Init != nil || hex.EncodeToString(body.Hash()) != r.BodyHash || len(*parent.Out) != 1 {
		return Receipt{}, "", "", fmt.Errorf("%w: sending request identity mismatch", ErrConflict)
	}
	outgoing := &(*parent.Out)[0]
	transferBody, err := tonBody(outgoing)
	if err != nil {
		return Receipt{}, "", "", err
	}
	if outgoing.Init != nil || !isFalse(outgoing.Bounce) || !isFalse(outgoing.Bounced) || transferBody.BitsSize() != 0 || transferBody.RefsNum() != 0 {
		return Receipt{}, "", "", fmt.Errorf("%w: output differs from native send policy", ErrConflict)
	}
	if len(e.Transaction) == 0 {
		return Receipt{}, "", "", ErrUnconfirmed
	}
	received, err := tonOneTransaction(e.Transaction)
	if err != nil {
		return Receipt{}, "", "", err
	}
	asset := chainassets.Asset{ID: chainassets.TONTestnet + ":ton", Decimals: 9, Origin: chainassets.Native}
	hash, err := tonHash(received.Hash)
	if err != nil {
		return Receipt{}, "", "", err
	}
	receipt, err := verifyTON(TONRequest{Network: chainassets.TONTestnet, Hash: hash, AssetID: asset.ID, Recipient: r.Recipient, Atoms: r.Atoms}, asset, e)
	if err != nil {
		return Receipt{}, "", "", err
	}
	if receipt.Event.SourceOwner != r.Sender {
		return Receipt{}, "", "", ErrConflict
	}
	return receipt, parent.Hash, (*parent.Out)[0].Hash, nil
}
