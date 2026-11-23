package chainassets

import (
	"bytes"
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
type tonDepositBudget struct{ network, dispatch, execution decimal.Decimal }

// Read the transaction BoCs: trace JSON summaries can omit outbound messages.
func (t tonDepositTrace) transaction(owner *address.Address, children int, head time.Time) (tlb.Transaction, error) {
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
		tx.OrigStatus != tlb.AccountStatusActive || tx.EndStatus != tlb.AccountStatusActive ||
		int(tx.OutMsgCount) != children || time.Unix(int64(tx.Now), 0).Sub(head).Abs() >= depositPreviewTTL {
		return tx, invalid("unexpected TON relay transaction or timestamp")
	}
	d, ok := tx.Description.(tlb.TransactionDescriptionOrdinary)
	vm, ran := d.ComputePhase.Phase.(tlb.ComputePhaseVM)
	a := d.ActionPhase
	if !ok || d.Aborted || d.Destroyed || d.BouncePhase != nil || !ran || !vm.Success || vm.Details.ExitCode != 0 ||
		a == nil || !a.Valid || !a.Success || a.ResultCode != 0 || a.SkippedActions != 0 ||
		int(a.TotalActions) != children || int(a.MessagesCreated) != children {
		return tx, invalid("TON emulation did not execute every expected action")
	}
	return tx, nil
}

// Follow each outgoing message to the identical input of the next transaction.
func tonDepositLeg(parent, child tlb.Transaction, from, to *address.Address, amount decimal.Decimal, bounce bool, body *cell.Cell) (decimal.Decimal, error) {
	if parent.IO.Out == nil {
		return decimal.Zero, invalid("missing TON relay output")
	}
	out, err := parent.IO.Out.ToSlice()
	if err != nil || len(out) != 1 {
		return decimal.Zero, invalid("ambiguous TON relay output")
	}
	m, ok := child.IO.In.Msg.(*tlb.InternalMessage)
	if !ok || m.SrcAddr == nil || m.DstAddr == nil || !m.SrcAddr.Equals(from) || !m.DstAddr.Equals(to) ||
		m.Bounced || m.Bounce != bounce || !m.IHRDisabled || m.StateInit != nil || m.Body == nil ||
		!bytes.Equal(m.Body.Hash(), body.Hash()) || !decimal.NewFromBigInt(m.Amount.Nano(), -9).Equal(amount) {
		return decimal.Zero, invalid("TON relay transfer does not match the quote")
	}
	sent, err := tlb.ToCell(out[0].Msg)
	if err != nil {
		return decimal.Zero, err
	}
	received, err := tlb.ToCell(m)
	if err != nil || !bytes.Equal(sent.Hash(), received.Hash()) {
		return decimal.Zero, invalid("disconnected TON relay emulation")
	}
	return decimal.NewFromBigInt(m.FwdFee.Nano(), -9), nil
}

func (t tonDepositTrace) budget(m tonDepositMessage, user, platform tonW5Account, pay decimal.Decimal, head time.Time) (tonDepositBudget, error) {
	var b tonDepositBudget
	first, err := t.transaction(platform.addr, 1, head)
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
	second, err := mid.transaction(user.addr, 1, head)
	if err != nil {
		return b, err
	}
	last, err := mid.Children[0].transaction(platform.addr, 0, head)
	if err != nil {
		return b, err
	}
	attached := decimal.New(tonRelayNano, -9)
	fwd1, err := tonDepositLeg(first, second, platform.addr, user.addr, attached, true, m.authorization)
	if err != nil {
		return b, err
	}
	fwd2, err := tonDepositLeg(second, last, user.addr, platform.addr, pay, false, cell.BeginCell().EndCell())
	if err != nil {
		return b, err
	}
	b.dispatch = decimal.NewFromBigInt(first.TotalFees.Coins.Nano(), -9).Add(fwd1)
	b.execution = decimal.NewFromBigInt(second.TotalFees.Coins.Nano(), -9).Add(fwd2)
	b.network = b.dispatch.Add(b.execution).Add(decimal.NewFromBigInt(last.TotalFees.Coins.Nano(), -9))
	if !b.network.IsPositive() || b.execution.GreaterThan(attached) {
		return b, invalid("TON relay does not cover the user's execution cost")
	}
	return b, nil
}
