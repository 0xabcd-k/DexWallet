package chainreceipt

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"reflect"
	"strconv"
	"strings"

	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
)

type TONJettonCosts struct{ Excess, Reserve, Surplus, SenderPaid int64 }

const (
	relayDispatch = iota
	payerAuthorization
	senderTransfer
	tokenReceipt
	ownerNotification
	platformRefund
)

type tonCenterTrace struct {
	ID         string `json:"trace_id"`
	Incomplete *bool  `json:"is_incomplete"`
	Info       struct {
		State   string `json:"trace_state"`
		Pending *int   `json:"pending_messages"`
		Count   int    `json:"transactions"`
	} `json:"trace_info"`
	Transactions map[string]json.RawMessage
}

func tonRelayChild(parent, child tonTransaction, bounce bool) bool {
	if child.In.Source != parent.Account || child.TraceID != parent.TraceID || child.MCSeqno < parent.MCSeqno || *child.In.Bounce != bounce || *child.In.Bounced {
		return false
	}
	a, _ := tonNumber(parent.LT, 64, true)
	b, _ := tonNumber(child.In.CreatedLT, 64, true)
	c, _ := tonNumber(child.LT, 64, true)
	if a.Cmp(b) >= 0 || b.Cmp(c) >= 0 {
		return false
	}
	for _, out := range *parent.Out {
		if reflect.DeepEqual(&out, child.In) {
			return true
		}
	}
	return false
}

func tonStepCost(t tonTransaction) *big.Int {
	n, _ := tonNumber(t.TotalFees, 120, false)
	for _, m := range *t.Out {
		fw, _ := tonNumber(m.ForwardFee, 120, false)
		n.Add(n, fw)
	}
	return n
}

// The signed W5 dispatch, credited Jetton wallet and both platform messages
// belong to one finalized trace. No order can settle from a notification alone.
func VerifyTONJettonDeposit(e chainassets.TONEvidence, r chainassets.TONDepositRelay) (Receipt, *big.Int, *TONJettonCosts, error) {
	var empty Receipt
	root, err := tonOneTransaction(e.Dispatch)
	if errors.Is(err, ErrUnconfirmed) {
		return empty, nil, nil, ErrTONDispatchAbsent
	}
	if err != nil {
		return empty, nil, nil, err
	}
	if err = normalizeTONTransaction(chainassets.TONTestnet, &root, true); err != nil {
		return empty, nil, nil, err
	}
	j := r.Jetton
	if j == nil || root.Account != r.Recipient || root.In.Source != "" || root.In.Init != nil || root.In.Content.Hash != r.DispatchHash {
		return empty, nil, nil, bad("incorrect Jetton deposit dispatch")
	}
	if err = tonSuccessful(root); err != nil {
		return empty, nil, nil, err
	}
	if len(*root.Out) != 1 {
		return empty, nil, nil, ErrExecutionFailed
	}
	var list struct{ Traces *[]tonCenterTrace }
	if len(e.Trace) == 0 {
		return empty, nil, nil, ErrUnconfirmed
	}
	if json.Unmarshal(e.Trace, &list) != nil || list.Traces == nil || len(*list.Traces) > 1 {
		return empty, nil, nil, bad("invalid Jetton trace response")
	}
	if len(*list.Traces) == 0 {
		return empty, nil, nil, ErrUnconfirmed
	}
	tr := (*list.Traces)[0]
	id, err := tonHash(tr.ID)
	if err != nil || id != root.Hash {
		return empty, nil, nil, bad("unrelated Jetton trace")
	}
	if !isFalse(tr.Incomplete) || tr.Info.State != "complete" || tr.Info.Pending == nil || *tr.Info.Pending != 0 {
		return empty, nil, nil, ErrUnconfirmed
	}
	if tr.Info.Count != len(tr.Transactions) {
		return empty, nil, nil, bad("inconsistent Jetton trace length")
	}
	// Signed payloads identify the two platform leaves even when their order changes.
	steps := [...]struct {
		body, owner     string
		parent, outputs int
		bounce          bool
	}{
		relayDispatch:      {r.DispatchHash, r.Recipient, -1, 1, false},
		payerAuthorization: {r.AuthHash, r.Sender, relayDispatch, 1, true},
		senderTransfer:     {j.PaymentHash, j.SenderWallet, payerAuthorization, 1, true},
		tokenReceipt:       {j.InternalHash, j.ReceiverWallet, senderTransfer, 2, true},
		ownerNotification:  {j.NotificationHash, r.Recipient, tokenReceipt, 0, false},
		platformRefund:     {j.ExcessHash, r.Recipient, tokenReceipt, 0, false},
	}
	txs := make([]tonTransaction, len(steps))
	raws := make([]json.RawMessage, len(steps))
	var head struct {
		Result struct{ Last struct{ Seqno int64 } }
	}
	if json.Unmarshal(e.Masterchain, &head) != nil || head.Result.Last.Seqno <= 0 {
		return empty, nil, nil, bad("missing Jetton confirmation head")
	}
	for k, raw := range tr.Transactions {
		var t tonTransaction
		if json.Unmarshal(raw, &t) != nil {
			return empty, nil, nil, bad("malformed Jetton transaction")
		}
		h, err := tonHash(k)
		if err != nil {
			return empty, nil, nil, err
		}
		if err = normalizeTONTransaction(chainassets.TONTestnet, &t, h == root.Hash); err != nil {
			return empty, nil, nil, err
		}
		if t.Hash != h || t.TraceID != id {
			return empty, nil, nil, bad("Jetton trace identity mismatch")
		}
		if t.MCSeqno > head.Result.Last.Seqno {
			return empty, nil, nil, ErrUnconfirmed
		}
		for i, step := range steps {
			if t.In.Content.Hash == step.body && t.Account == step.owner {
				if raws[i] != nil {
					return empty, nil, nil, bad("duplicate Jetton trace step")
				}
				txs[i], raws[i] = t, raw
				break
			}
		}
	}
	if raws[relayDispatch] == nil || !reflect.DeepEqual(txs[relayDispatch], root) {
		return empty, nil, nil, bad("Jetton trace root differs")
	}
	for i, t := range txs {
		if raws[i] == nil {
			return empty, nil, nil, bad("missing signed Jetton trace step")
		}
		if i != relayDispatch && !tonRelayChild(txs[steps[i].parent], t, steps[i].bounce) {
			return empty, nil, nil, bad("disconnected Jetton message")
		}
		if t.EndStatus != "active" {
			return empty, nil, nil, ErrExecutionFailed
		}
		if i == ownerNotification {
			err = tonNativeCredit(t)
		} else {
			err = tonSuccessful(t)
		}
		if err != nil {
			return empty, nil, nil, err
		}
		if len(*t.Out) != steps[i].outputs {
			return empty, nil, nil, ErrExecutionFailed
		}
		if i != tokenReceipt && t.In.Init != nil {
			return empty, nil, nil, bad("unexpected relay state initialization")
		}
	}
	if len(tr.Transactions) != len(txs) || txs[payerAuthorization].In.Value != strconv.FormatInt(chainassets.TONDepositAttachmentNano+chainassets.TONJettonTransferNano, 10) || txs[senderTransfer].In.Value != strconv.FormatInt(chainassets.TONJettonTransferNano, 10) || txs[ownerNotification].In.Value != "1" {
		return empty, nil, nil, bad("unexpected Jetton relay amounts/actions")
	}
	recv := txs[tokenReceipt]
	if recv.In.Init == nil {
		return empty, nil, nil, bad("missing Jetton wallet initialization")
	}
	init, err := tonCell(recv.In.Init.Body)
	initHash, hashErr := tonHash(recv.In.Init.Hash)
	if err != nil || hashErr != nil || hex.EncodeToString(init.Hash()) != initHash || initHash != strings.TrimPrefix(j.ReceiverWallet, "0:") {
		return empty, nil, nil, bad("wrong Jetton wallet initialization")
	}
	// Feed the canonical token-receipt verifier the actual send/receive transactions.
	one := func(raw json.RawMessage) json.RawMessage {
		v, _ := json.Marshal(struct {
			Transactions []json.RawMessage `json:"transactions"`
		}{[]json.RawMessage{raw}})
		return v
	}
	e.Parent, e.Transaction = one(raws[senderTransfer]), one(raws[tokenReceipt])
	if len(e.Block) == 0 || len(e.Wallet) == 0 {
		return empty, nil, nil, ErrUnconfirmed
	}
	asset := chainassets.Asset{ID: chainassets.TONDepositTestUSDT, Contract: chainassets.TONDepositTestMaster, Origin: chainassets.TestFixture, Decimals: 6}
	receipt, err := verifyTON(TONRequest{Network: chainassets.TONTestnet, Hash: recv.Hash, AssetID: asset.ID, Recipient: r.Recipient, Atoms: r.Atoms.String()}, asset, e)
	if err != nil {
		return empty, nil, nil, err
	}
	if receipt.Event.SourceOwner != r.Sender {
		return empty, nil, nil, bad("incorrect Jetton deposit payer")
	}
	total := new(big.Int)
	for _, t := range txs {
		total.Add(total, tonStepCost(t))
	}
	excess, err := tonNumber(txs[platformRefund].In.Value, 120, true)
	if err != nil || !excess.IsInt64() {
		return empty, nil, nil, bad("invalid Jetton refund")
	}
	funding, _ := tonNumber(recv.In.Value, 120, false)
	senderCost := tonStepCost(txs[senderTransfer])
	advanced, _ := tonNumber(txs[senderTransfer].In.Value, 120, true)
	senderPaid := new(big.Int).Sub(new(big.Int).Add(senderCost, funding), advanced)
	if senderPaid.Sign() < 0 {
		senderPaid.SetInt64(0)
	}
	if !senderPaid.IsInt64() || senderPaid.Cmp(senderCost) > 0 {
		return empty, nil, nil, bad("unexpected transfer from existing sender reserve")
	}
	reserve := new(big.Int).Sub(funding, tonStepCost(recv))
	reserve.Sub(reserve, excess)
	reserve.Sub(reserve, big.NewInt(1))
	if reserve.Sign() < 0 {
		reserve.SetInt64(0)
	}
	surplus := new(big.Int).Sub(big.NewInt(chainassets.TONDepositAttachmentNano), tonStepCost(txs[payerAuthorization]))
	if !reserve.IsInt64() || !surplus.IsInt64() || surplus.Sign() < 0 {
		return empty, nil, nil, ErrExecutionFailed
	}
	return receipt, total, &TONJettonCosts{excess.Int64(), reserve.Int64(), surplus.Int64(), senderPaid.Int64()}, nil
}
