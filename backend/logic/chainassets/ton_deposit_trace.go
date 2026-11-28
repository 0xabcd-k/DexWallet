package chainassets

import (
	"bytes"
	"math/big"
	"time"

	"github.com/shopspring/decimal"
	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

type tonDepositTrace struct {
	Emulated    bool
	Transaction struct{ Raw string }
	Children    []tonDepositTrace
}
type tonDepositBudget struct{ network, dispatch, execution, excess, reserve decimal.Decimal }
type tonTraceStep int

const (
	tonSendStep tonTraceStep = iota
	tonReceiveJettons
	tonNotifyOwner
)

// Read the transaction BoCs: trace JSON summaries can omit outbound messages.
func (t tonDepositTrace) transaction(owner *address.Address, children int, head time.Time, step tonTraceStep) (tlb.Transaction, error) {
	var tx tlb.Transaction
	if !t.Emulated || len(t.Children) != children {
		return tx, invalid("incomplete TON relay trace")
	}
	raw, err := tonDepositCell(t.Transaction.Raw)
	if err != nil {
		return tx, err
	}
	if err = tlb.LoadFromCell(&tx, raw.BeginParse()); err != nil {
		return tx, err
	}
	if !bytes.Equal(tx.AccountAddr, owner.Data()) || tx.IO.In == nil ||
		tx.EndStatus != tlb.AccountStatusActive ||
		int(tx.OutMsgCount) != children || time.Unix(int64(tx.Now), 0).Sub(head).Abs() >= depositPreviewTTL {
		return tx, invalid("unexpected TON relay transaction or timestamp")
	}
	if tx.OrigStatus != tlb.AccountStatusActive && !(step == tonReceiveJettons && (tx.OrigStatus == tlb.AccountStatusNonExist || tx.OrigStatus == tlb.AccountStatusUninit)) {
		return tx, invalid("unexpected TON relay account activation")
	}
	d, ok := tx.Description.(tlb.TransactionDescriptionOrdinary)
	if step == tonNotifyOwner && ok && d.Aborted {
		skip, ok := d.ComputePhase.Phase.(tlb.ComputePhaseSkipped)
		if !ok || skip.Reason.Type != tlb.ComputeSkipReasonNoGas || d.Destroyed || d.BouncePhase != nil || d.ActionPhase != nil || d.CreditPhase == nil || d.CreditPhase.Credit.Coins.Nano().Cmp(big.NewInt(tonJettonForwardNano)) != 0 {
			return tx, invalid("unexpected Jetton notification failure")
		}
		return tx, nil
	}
	actions := children
	if step == tonReceiveJettons {
		actions++
	} // The receiver reserves TON before sending the excess.
	vm, ran := d.ComputePhase.Phase.(tlb.ComputePhaseVM)
	a := d.ActionPhase
	if !ok || d.Aborted || d.Destroyed || d.BouncePhase != nil || !ran || !vm.Success || vm.Details.ExitCode != 0 ||
		a == nil || !a.Valid || !a.Success || a.ResultCode != 0 || a.SkippedActions != 0 ||
		int(a.SpecActions) != actions-children || int(a.TotalActions) != actions || int(a.MessagesCreated) != children {
		return tx, invalid("TON emulation did not execute every expected action")
	}
	return tx, nil
}

// Match the parent's output to the child's complete input, including forwarding fees.
func linkedTONMessage(parent, child tlb.Transaction, from, to *address.Address, bounce bool, body *cell.Cell) (*tlb.InternalMessage, error) {
	if parent.IO.Out == nil {
		return nil, invalid("missing TON relay output")
	}
	m, ok := child.IO.In.Msg.(*tlb.InternalMessage)
	if !ok || m.SrcAddr == nil || m.DstAddr == nil || !m.SrcAddr.Equals(from) || !m.DstAddr.Equals(to) ||
		m.Bounced || m.Bounce != bounce || !m.IHRDisabled || m.Body == nil || !bytes.Equal(m.Body.Hash(), body.Hash()) {
		return nil, invalid("TON relay transfer does not match the quote")
	}
	received, err := tlb.ToCell(m)
	if err != nil {
		return nil, err
	}
	out, err := parent.IO.Out.ToSlice()
	if err != nil {
		return nil, err
	}
	if len(out) != int(parent.OutMsgCount) {
		return nil, invalid("incorrect TON relay output count")
	}
	for _, v := range out {
		sent, err := tlb.ToCell(v.Msg)
		if err != nil {
			return nil, err
		}
		if bytes.Equal(sent.Hash(), received.Hash()) {
			return m, nil
		}
	}
	return nil, invalid("disconnected TON relay emulation")
}
func tonDepositLeg(parent, child tlb.Transaction, from, to *address.Address, amount decimal.Decimal, bounce bool, body *cell.Cell) (decimal.Decimal, error) {
	m, err := linkedTONMessage(parent, child, from, to, bounce, body)
	if err != nil {
		return decimal.Zero, err
	}
	if m.StateInit != nil || !decimal.NewFromBigInt(m.Amount.Nano(), -9).Equal(amount) {
		return decimal.Zero, invalid("incorrect TON relay amount or initialization")
	}
	return decimal.NewFromBigInt(m.FwdFee.Nano(), -9), nil
}

func (t tonDepositTrace) budget(m tonDepositMessage, user, platform tonW5Account, token *tonDepositJetton, pay decimal.Decimal, head time.Time) (tonDepositBudget, error) {
	var b tonDepositBudget
	first, err := t.transaction(platform.addr, 1, head, tonSendStep)
	if err != nil {
		return b, err
	}
	ext, ok := first.IO.In.Msg.(*tlb.ExternalMessage)
	if !ok {
		return b, invalid("wrong TON relay input")
	}
	input, err := tlb.ToCell(ext)
	if err != nil || !bytes.Equal(input.Hash(), m.external.Hash()) {
		return b, invalid("TON emulation belongs to another request")
	}
	mid := t.Children[0]
	second, err := mid.transaction(user.addr, 1, head, tonSendStep)
	if err != nil {
		return b, err
	}
	attachmentNano := int64(TONDepositAttachmentNano)
	if token != nil {
		attachmentNano += tonJettonActionNano
	}
	attached := decimal.New(attachmentNano, -9)
	fwd1, err := tonDepositLeg(first, second, platform.addr, user.addr, attached, true, m.authorization)
	if err != nil {
		return b, err
	}
	b.dispatch = decimal.NewFromBigInt(first.TotalFees.Coins.Nano(), -9).Add(fwd1)
	if token != nil {
		return token.budget(b, mid.Children[0], second, m, user, platform, pay, head)
	}
	last, err := mid.Children[0].transaction(platform.addr, 0, head, tonSendStep)
	if err != nil {
		return b, err
	}
	fwd2, err := tonDepositLeg(second, last, user.addr, platform.addr, pay, false, cell.BeginCell().EndCell())
	if err != nil {
		return b, err
	}
	b.execution = decimal.NewFromBigInt(second.TotalFees.Coins.Nano(), -9).Add(fwd2)
	b.network = b.dispatch.Add(b.execution).Add(decimal.NewFromBigInt(last.TotalFees.Coins.Nano(), -9))
	if !b.network.IsPositive() || b.execution.GreaterThan(attached) {
		return b, invalid("TON relay does not cover the user's execution cost")
	}
	return b, nil
}
