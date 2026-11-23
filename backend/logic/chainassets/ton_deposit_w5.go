package chainassets

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"math/big"
	"net/http"

	"github.com/0xabcd-k/DexWallet/backend/apperror"
	"github.com/shopspring/decimal"
	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/ton/jetton"
	"github.com/xssnick/tonutils-go/ton/wallet"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

type tonW5Account struct {
	addr    *address.Address
	seqno   uint64
	balance decimal.Decimal
}

var tonW5ID = uint64((wallet.V5R1ID{NetworkGlobalID: tonTestnetGlobalID}).Serialized())

func tonDepositCell(raw string) (*cell.Cell, error) {
	b, err := hex.DecodeString(raw)
	if err != nil {
		return nil, err
	}
	roots, err := cell.FromBOCMultiRoot(b)
	if err != nil || len(roots) != 1 || roots[0].GetType() != cell.OrdinaryCellType {
		return nil, invalid("invalid TON cell")
	}
	return roots[0], nil
}

type tonQuoteAccount struct {
	Address, Code, Data, Status string
	Balance                     *int64
}

func (s *Service) tonQuoteAccount(ctx context.Context, n Network, e Endpoint, owner string) (tonQuoteAccount, error) {
	var state tonQuoteAccount
	err := s.request(ctx, n, e, "/v2/blockchain/accounts/"+owner, nil, nil, &state)
	if errors.Is(err, upstreamHTTPStatus(http.StatusNotFound)) {
		// TonAPI omits raw state for absent accounts. Its account summary can still
		// attest the address and status; an HTTP 404 alone is not a zero balance.
		err = s.request(ctx, n, e, "/v2/accounts/"+owner, nil, nil, &state)
		if err == nil && state.Status != "nonexist" && state.Status != "uninit" {
			err = invalid("TON raw account missing despite account summary")
		}
	}
	if err != nil {
		return state, apperror.New(apperror.ServiceUnavailable, err)
	}
	if state.Address != owner || state.Balance == nil || *state.Balance < 0 {
		return state, apperror.New(apperror.ServiceUnavailable, invalid("incorrect TON account response"))
	}
	return state, nil
}
func (s *Service) tonDepositWallet(ctx context.Context, n Network, e Endpoint, owner string) (tonW5Account, error) {
	var empty tonW5Account
	state, err := s.tonQuoteAccount(ctx, n, e, owner)
	if err != nil {
		return empty, err
	}
	if state.Status != "active" {
		return empty, badTarget("TON deposit requires an initialized W5")
	}
	code, err := tonDepositCell(state.Code)
	if err != nil {
		return empty, apperror.New(apperror.ServiceUnavailable, err)
	}
	data, err := tonDepositCell(state.Data)
	if err != nil {
		return empty, apperror.New(apperror.ServiceUnavailable, err)
	}
	d := data.BeginParse()
	// W5R1 basechain, default testnet subwallet, public-key signing, no extensions.
	if d.BitsLeft() != 1+32+32+256+1 || d.RefsNum() != 0 || !d.MustLoadBoolBit() {
		return empty, badTarget("unsupported TON signing state")
	}
	seqno, id, key := d.MustLoadUInt(32), d.MustLoadUInt(32), d.MustLoadSlice(256)
	if seqno == 0 || id != tonW5ID || d.MustLoadBoolBit() {
		return empty, badTarget("unsupported TON wallet ID, extensions or first use")
	}
	init, err := wallet.GetStateInit(key, wallet.ConfigV5R1Final{NetworkGlobalID: tonTestnetGlobalID}, 0)
	if err != nil {
		return empty, err
	}
	addr := init.CalcAddress(0)
	if !bytes.Equal(code.Hash(), init.Code.Hash()) || addr.StringRaw() != owner {
		return empty, badTarget("expected the default testnet W5R1 wallet")
	}
	return tonW5Account{addr, seqno, decimal.New(*state.Balance, -9)}, nil
}

const tonW5SignedInternal = 0x73696e74
const tonW5SignedExternal = 0x7369676e

type tonDepositMessage struct {
	external, authorization, paymentBody *cell.Cell
	queryID                              uint64
}

func tonEmulatedW5Body(seqno, op uint64, deadline uint32, transfer *tlb.InternalMessage) (*cell.Cell, error) {
	actions, err := wallet.PackV5OutActions([]*wallet.Message{{Mode: wallet.PayGasSeparately | wallet.IgnoreErrors, InternalMessage: transfer}})
	if err != nil {
		return nil, err
	}
	// Both signature slots are zero solely for TonAPI's read-only emulator.
	return cell.BeginCell().MustStoreUInt(op, 32).MustStoreUInt(tonW5ID, 32).MustStoreUInt(uint64(deadline), 32).MustStoreUInt(seqno, 32).MustStoreBuilder(actions).MustStoreSlice(make([]byte, 64), 512).EndCell(), nil
}

func buildTONDepositMessage(user, platform tonW5Account, token *tonDepositJetton, atoms *big.Int, deadline uint32) (tonDepositMessage, error) {
	var m tonDepositMessage
	pay := &tlb.InternalMessage{IHRDisabled: true, DstAddr: platform.addr, Amount: tlb.FromNanoTON(atoms), Body: cell.BeginCell().EndCell()}
	relayNano := uint64(tonRelayNano)
	if token != nil {
		body, err := tlb.ToCell(jetton.TransferPayload{QueryID: uint64(deadline), Amount: tlb.FromNanoTON(atoms), Destination: platform.addr, ResponseDestination: platform.addr, ForwardTONAmount: tlb.FromNanoTONU(tonJettonForwardNano), ForwardPayload: cell.BeginCell().EndCell()})
		if err != nil {
			return m, err
		}
		pay = &tlb.InternalMessage{IHRDisabled: true, Bounce: true, DstAddr: token.sender, Amount: tlb.FromNanoTONU(tonJettonActionNano), Body: body}
		relayNano += tonJettonActionNano
	}
	auth, err := tonEmulatedW5Body(user.seqno, tonW5SignedInternal, deadline, pay)
	if err != nil {
		return m, err
	}
	relay := &tlb.InternalMessage{IHRDisabled: true, Bounce: true, DstAddr: user.addr, Amount: tlb.FromNanoTONU(relayNano), Body: auth}
	dispatch, err := tonEmulatedW5Body(platform.seqno, tonW5SignedExternal, deadline, relay)
	if err != nil {
		return m, err
	}
	ext, err := tlb.ToCell(&tlb.ExternalMessage{DstAddr: platform.addr, Body: dispatch})
	return tonDepositMessage{ext, auth, pay.Body, uint64(deadline)}, err
}
