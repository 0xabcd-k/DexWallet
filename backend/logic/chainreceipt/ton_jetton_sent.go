package chainreceipt

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"strconv"

	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
	"github.com/xssnick/tonutils-go/ton/jetton"
)

type TONJettonSentRequest struct {
	TONSentRequest
	AssetID, Master, Wallet, AttachTON, ForwardTON, QueryID string
}
type tonJettonSendSource interface {
	ReadTONJettonSendEvidence(context.Context, string, string, string, string) (chainassets.TONEvidence, error)
}

// RecordJettonSent requires all three hops: signing wallet, sending token wallet,
// receiving token wallet. Node acceptance and notification alone cannot settle it.
func (s *TONService) RecordJettonSent(ctx context.Context, r TONJettonSentRequest) (TONSentStored, error) {
	source, ok := s.verifier.source.(tonJettonSendSource)
	if !ok {
		return TONSentStored{}, ErrUnsupported
	}
	var err error
	for _, p := range []*string{&r.Sender, &r.Recipient, &r.Master, &r.Wallet} {
		*p, err = tonAddress(chainassets.TONTestnet, *p)
		if err != nil {
			return TONSentStored{}, ErrInvalidRequest
		}
	}
	r.BodyHash, err = tonHash(r.BodyHash)
	if err != nil {
		return TONSentStored{}, ErrInvalidRequest
	}
	for _, v := range []string{r.Atoms, r.AttachTON} {
		if _, err = tonNumber(v, 120, true); err != nil {
			return TONSentStored{}, ErrInvalidRequest
		}
	}
	forward, err := tonNumber(r.ForwardTON, 120, false)
	if err != nil {
		return TONSentStored{}, ErrInvalidRequest
	}
	attach, _ := tonNumber(r.AttachTON, 120, true)
	q, err := strconv.ParseUint(r.QueryID, 10, 64)
	if err != nil || q == 0 || strconv.FormatUint(q, 10) != r.QueryID || forward.Cmp(attach) >= 0 {
		return TONSentStored{}, ErrInvalidRequest
	}
	var asset chainassets.Asset
	for _, n := range s.verifier.source.Catalog() {
		if n.ID == chainassets.TONTestnet {
			for _, a := range n.Assets {
				m, e := tonAddress(n.ID, a.Contract)
				if a.ID == r.AssetID && a.Contract != "" && e == nil && m == r.Master {
					asset = a
					asset.Contract = m
				}
			}
		}
	}
	if asset.ID == "" {
		return TONSentStored{}, ErrInvalidRequest
	}
	evidence, err := source.ReadTONJettonSendEvidence(ctx, chainassets.TONTestnet, r.BodyHash, r.Master, r.Recipient)
	if err != nil {
		return TONSentStored{}, err
	}
	receipt, sender, message, err := verifyTONJettonSent(r, asset, evidence)
	if err != nil {
		return TONSentStored{}, err
	}
	stored, err := s.store.record(ctx, receipt)
	return TONSentStored{Stored: stored, SenderHash: sender, MessageHash: message}, err
}
func verifyTONJettonSent(r TONJettonSentRequest, asset chainassets.Asset, e chainassets.TONEvidence) (Receipt, string, string, error) {
	if len(e.Dispatch) == 0 || len(e.Parent) == 0 || len(e.Transaction) == 0 {
		return Receipt{}, "", "", ErrUnconfirmed
	}
	dispatch, err := tonOneTransaction(e.Dispatch)
	if err != nil {
		return Receipt{}, "", "", err
	}
	if err = normalizeTONTransaction(chainassets.TONTestnet, &dispatch, true); err != nil {
		return Receipt{}, "", "", err
	}
	if err = tonSuccessful(dispatch); err != nil {
		return Receipt{}, "", "", err
	}
	body, err := tonBody(dispatch.In)
	if err != nil {
		return Receipt{}, "", "", err
	}
	if dispatch.Account != r.Sender || dispatch.In.Source != "" || dispatch.In.Init != nil || hex.EncodeToString(body.Hash()) != r.BodyHash || len(*dispatch.Out) != 1 {
		return Receipt{}, "", "", ErrConflict
	}
	parent, err := tonOneTransaction(e.Parent)
	if err != nil {
		return Receipt{}, "", "", err
	}
	if err = normalizeTONTransaction(chainassets.TONTestnet, &parent, false); err != nil {
		return Receipt{}, "", "", err
	}
	derived, err := tonDerivedWallet(e.SenderWallet, chainassets.TONTestnet)
	if err != nil || derived != r.Wallet || parent.Account != r.Wallet || parent.In.Source != r.Sender || dispatch.TraceID != parent.TraceID || dispatch.Hash == parent.Hash || dispatch.MCSeqno > parent.MCSeqno {
		return Receipt{}, "", "", ErrConflict
	}
	sent := &(*dispatch.Out)[0]
	a, _ := json.Marshal(sent)
	b, _ := json.Marshal(parent.In)
	if string(a) != string(b) || sent.Init != nil || !isTrue(sent.Bounce) || !isFalse(sent.Bounced) || sent.Value != r.AttachTON {
		return Receipt{}, "", "", ErrConflict
	}
	dispatchLT, _ := tonNumber(dispatch.LT, 64, true)
	created, _ := tonNumber(sent.CreatedLT, 64, true)
	parentLT, _ := tonNumber(parent.LT, 64, true)
	if dispatchLT.Cmp(created) >= 0 || created.Cmp(parentLT) >= 0 {
		return Receipt{}, "", "", ErrConflict
	}
	transferBody, err := tonBody(sent)
	if err != nil {
		return Receipt{}, "", "", err
	}
	var transfer jetton.TransferPayload
	if err = tonPayload(transferBody, &transfer); err != nil {
		return Receipt{}, "", "", err
	}
	recipient, err := tonParsedAddress(chainassets.TONTestnet, transfer.Destination)
	if err != nil {
		return Receipt{}, "", "", err
	}
	response, err := tonParsedAddress(chainassets.TONTestnet, transfer.ResponseDestination)
	if err != nil {
		return Receipt{}, "", "", err
	}
	if recipient != r.Recipient || response != r.Sender || transfer.Amount.Nano().String() != r.Atoms || strconv.FormatUint(transfer.QueryID, 10) != r.QueryID || transfer.ForwardTONAmount.Nano().String() != r.ForwardTON || transfer.CustomPayload != nil || transfer.ForwardPayload == nil || transfer.ForwardPayload.BitsSize() != 0 || transfer.ForwardPayload.RefsNum() != 0 {
		return Receipt{}, "", "", ErrConflict
	}
	received, err := tonOneTransaction(e.Transaction)
	if err != nil {
		return Receipt{}, "", "", err
	}
	receivedBody, err := tonBody(received.In)
	if err != nil {
		return Receipt{}, "", "", err
	}
	var receivedTransfer tonInternalTransfer
	if err = tonPayload(receivedBody, &receivedTransfer); err != nil {
		return Receipt{}, "", "", err
	}
	receivedResponse, err := tonParsedAddress(chainassets.TONTestnet, receivedTransfer.Response)
	if err != nil || receivedResponse != r.Sender || receivedTransfer.Payload == nil || receivedTransfer.Payload.BitsSize() != 0 || receivedTransfer.Payload.RefsNum() != 0 {
		return Receipt{}, "", "", ErrConflict
	}
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
	// The stored event is the token credit, not its optional downstream notification.
	message, err := tonHash(received.In.Hash)
	return receipt, dispatch.Hash, message, err
}
