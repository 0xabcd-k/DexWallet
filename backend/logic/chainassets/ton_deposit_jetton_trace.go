package chainassets

import (
	"bytes"
	"math/big"
	"time"

	"github.com/shopspring/decimal"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

const (
	tonJettonInternalTransfer = 0x178d4519
	tonJettonNotification     = 0x7362d09c
	tonJettonExcess           = 0xd53276db
)

func (t *tonDepositJetton) budget(b tonDepositBudget, tr tonDepositTrace, w5 tlb.Transaction, m tonDepositMessage, user, platform tonW5Account, pay decimal.Decimal, head time.Time) (tonDepositBudget, error) {
	sender, err := tr.transaction(t.sender, 1, head, tonSendStep)
	if err != nil {
		return b, err
	}
	fw, err := tonDepositLeg(w5, sender, user.addr, t.sender, decimal.New(tonJettonActionNano, -9), true, m.paymentBody)
	if err != nil {
		return b, err
	}
	b.execution = decimal.NewFromBigInt(w5.TotalFees.Coins.Nano(), -9).Add(fw)
	if b.execution.GreaterThan(decimal.New(TONDepositAttachmentNano, -9)) {
		return b, invalid("Jetton W5 execution exceeds platform advance")
	}
	recvTrace := tr.Children[0]
	receiver, err := recvTrace.transaction(t.receiver, 2, head, tonReceiveJettons)
	if err != nil {
		return b, err
	}
	if (receiver.OrigStatus == tlb.AccountStatusActive) != t.receiverActive {
		return b, invalid("Jetton receiver state changed during estimate")
	}
	atoms := pay.Shift(tonJettonDecimals).BigInt()
	internal, notify, excess := tonJettonReceiptBodies(m.queryID, atoms, user, platform)
	in, err := linkedTONMessage(sender, receiver, t.sender, t.receiver, true, internal)
	if err != nil {
		return b, err
	}
	if in.StateInit == nil {
		return b, invalid("missing Jetton recipient StateInit")
	}
	init, err := tlb.ToCell(in.StateInit)
	if err != nil || !bytes.Equal(init.Hash(), t.receiver.Data()) {
		return b, invalid("unexpected Jetton recipient StateInit")
	}
	funding := decimal.NewFromBigInt(in.Amount.Nano(), -9)
	senderCost := decimal.NewFromBigInt(sender.TotalFees.Coins.Nano(), -9).Add(decimal.NewFromBigInt(in.FwdFee.Nano(), -9))
	// The sending Jetton contract may use existing TON for accumulated rent.
	b.senderPaid = decimal.Max(decimal.Zero, senderCost.Add(funding).Sub(decimal.New(tonJettonActionNano, -9)))
	if b.senderPaid.GreaterThan(senderCost) {
		return b, invalid("unexpected redistribution from sender Jetton reserve")
	}
	b.network = b.dispatch.Add(b.execution).Add(senderCost)
	receiverCost := decimal.NewFromBigInt(receiver.TotalFees.Coins.Nano(), -9)
	var notified bool
	for _, leaf := range recvTrace.Children {
		tx, err := leaf.transaction(platform.addr, 0, head, tonNotifyOwner)
		if err != nil {
			return b, err
		}
		msg, ok := tx.IO.In.Msg.(*tlb.InternalMessage)
		if !ok || msg.Body == nil {
			return b, invalid("missing Jetton receipt message")
		}
		switch {
		case bytes.Equal(msg.Body.Hash(), notify.Hash()):
			if notified || msg.Amount.Nano().Cmp(big.NewInt(tonJettonForwardNano)) != 0 {
				return b, invalid("invalid Jetton transfer notification")
			}
			notified = true
		case bytes.Equal(msg.Body.Hash(), excess.Hash()):
			if !b.excess.IsZero() || msg.Amount.Nano().Sign() <= 0 || tx.Description.(tlb.TransactionDescriptionOrdinary).Aborted {
				return b, invalid("invalid Jetton excess refund")
			}
			b.excess = decimal.NewFromBigInt(msg.Amount.Nano(), -9)
		default:
			return b, invalid("unexpected Jetton notification or refund payload")
		}
		if _, err = linkedTONMessage(receiver, tx, t.receiver, platform.addr, false, msg.Body); err != nil {
			return b, err
		}
		if msg.StateInit != nil {
			return b, invalid("unexpected W5 initialization in Jetton refund or notification")
		}
		receiverCost = receiverCost.Add(decimal.NewFromBigInt(msg.FwdFee.Nano(), -9))
		b.network = b.network.Add(decimal.NewFromBigInt(tx.TotalFees.Coins.Nano(), -9))
	}
	if !notified || !b.excess.IsPositive() {
		return b, invalid("missing Jetton notification or platform excess")
	}
	b.network = b.network.Add(receiverCost)
	// Observed change of the receiving Jetton wallet's TON balance, excluding any
	// pre-existing storage consumed. Only a positive addition is shown as funding.
	b.reserve = decimal.Max(decimal.Zero, funding.Sub(receiverCost).Sub(b.excess).Sub(decimal.New(tonJettonForwardNano, -9)))
	return b, nil
}

func tonJettonReceiptBodies(query uint64, atoms *big.Int, user, platform tonW5Account) (*cell.Cell, *cell.Cell, *cell.Cell) {
	internal := cell.BeginCell().MustStoreUInt(tonJettonInternalTransfer, 32).MustStoreUInt(query, 64).MustStoreBigCoins(atoms).MustStoreAddr(user.addr).MustStoreAddr(platform.addr).MustStoreCoins(tonJettonForwardNano).MustStoreBoolBit(false).EndCell()
	notify := cell.BeginCell().MustStoreUInt(tonJettonNotification, 32).MustStoreUInt(query, 64).MustStoreBigCoins(atoms).MustStoreAddr(user.addr).MustStoreBoolBit(false).EndCell()
	excess := cell.BeginCell().MustStoreUInt(tonJettonExcess, 32).MustStoreUInt(query, 64).EndCell()
	return internal, notify, excess
}
