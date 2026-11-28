package chainreceipt

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"reflect"
	"strconv"

	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
)

var ErrTONDispatchAbsent = errors.New("TON dispatch not indexed")

// TON Center indexes the signed external body. Follow both internal messages
// to the platform's receive transaction, which is the unique credit event.
func VerifyTONDeposit(e chainassets.TONEvidence, r chainassets.TONDepositRelay) (Receipt, *big.Int, error) {
	var empty Receipt
	dispatch, err := tonOneTransaction(e.Dispatch)
	if errors.Is(err, ErrUnconfirmed) {
		return empty, nil, ErrTONDispatchAbsent
	}
	if err != nil {
		return empty, nil, err
	}
	net := chainassets.TONTestnet
	if err = normalizeTONTransaction(net, &dispatch, true); err != nil {
		return empty, nil, err
	}
	if dispatch.Account != r.Recipient || dispatch.In.Source != "" || dispatch.In.Init != nil || dispatch.In.Content.Hash != r.DispatchHash {
		return empty, nil, bad("TON deposit dispatch differs from original signed request")
	}
	if err = tonSuccessful(dispatch); err != nil {
		return empty, nil, err
	}
	if len(*dispatch.Out) != 1 {
		return empty, nil, ErrExecutionFailed
	}
	if len(e.Parent) == 0 {
		return empty, nil, ErrUnconfirmed
	}
	mid, err := tonOneTransaction(e.Parent)
	if err != nil {
		return empty, nil, err
	}
	if err = normalizeTONTransaction(net, &mid, false); err != nil {
		return empty, nil, err
	}
	relay := &(*dispatch.Out)[0]
	sentLT, _ := tonNumber(dispatch.LT, 64, true)
	createdLT, _ := tonNumber(relay.CreatedLT, 64, true)
	midLT, _ := tonNumber(mid.LT, 64, true)
	if !reflect.DeepEqual(relay, mid.In) || dispatch.TraceID != mid.TraceID || dispatch.MCSeqno > mid.MCSeqno || sentLT.Cmp(createdLT) >= 0 || createdLT.Cmp(midLT) >= 0 || relay.Source != r.Recipient || mid.Account != r.Sender || relay.Content.Hash != r.AuthHash || relay.Value != strconv.FormatInt(chainassets.TONDepositAttachmentNano, 10) || !isTrue(relay.Bounce) || !isFalse(relay.Bounced) || relay.Init != nil {
		return empty, nil, bad("TON deposit internal authorization differs")
	}
	if err = tonSuccessful(mid); err != nil {
		return empty, nil, err
	}
	if len(*mid.Out) != 1 {
		return empty, nil, ErrExecutionFailed
	}
	if len(e.Transaction) == 0 {
		return empty, nil, ErrUnconfirmed
	}
	tx, err := tonOneTransaction(e.Transaction)
	if err != nil {
		return empty, nil, err
	}
	h, err := tonHash(tx.Hash)
	if err != nil {
		return empty, nil, err
	}
	asset := chainassets.Asset{ID: net + ":ton", Origin: chainassets.Native, Decimals: 9}
	receipt, err := verifyTON(TONRequest{Network: net, Hash: h, AssetID: asset.ID, Recipient: r.Recipient, Atoms: r.Atoms.String()}, asset, e)
	if err != nil {
		return empty, nil, err
	}
	payment := &(*mid.Out)[0]
	body, err := tonBody(payment)
	if err != nil || body.BitsSize() != 0 || body.RefsNum() != 0 || !isFalse(payment.Bounce) || payment.Init != nil || receipt.Event.SourceOwner != r.Sender || len(*tx.Out) != 0 {
		return empty, nil, bad("unexpected TON deposit payment action")
	}
	if err = tonSuccessful(tx); err != nil {
		return empty, nil, err
	}
	// Total transaction fees already include the collected part of forwarding fees.
	total := new(big.Int)
	for _, v := range []string{dispatch.TotalFees, relay.ForwardFee, mid.TotalFees, payment.ForwardFee, receipt.Transaction.FeeAtoms} {
		n, err := tonNumber(v, 120, false)
		if err != nil {
			return empty, nil, err
		}
		total.Add(total, n)
	}
	exec, _ := tonNumber(mid.TotalFees, 120, false)
	fwd, _ := tonNumber(payment.ForwardFee, 120, false)
	if exec.Add(exec, fwd).Cmp(big.NewInt(chainassets.TONDepositAttachmentNano)) > 0 {
		return receipt, total, ErrExecutionFailed
	}
	return receipt, total, nil
}

// A relayed payment arriving at a single-use address may also be observed by
// the ordinary listener. Its parent received the signed internal authorization.
func tonDepositParentHash(raw string) (string, error) {
	var record struct{ Parent json.RawMessage }
	if err := json.Unmarshal([]byte(raw), &record); err != nil {
		return "", err
	}
	t, err := tonOneTransaction(record.Parent)
	if err != nil {
		return "", err
	}
	if t.In == nil || t.In.Source == "" {
		return "", nil
	}
	b, err := tonBody(t.In)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(b.Hash()), nil
}
