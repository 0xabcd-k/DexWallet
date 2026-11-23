package chainassets

import (
	"bytes"
	"context"
	"encoding/hex"
	"math/big"

	"github.com/0xabcd-k/DexWallet/backend/apperror"
	"github.com/shopspring/decimal"
	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
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

func (s *Service) tonDepositWallet(ctx context.Context, n Network, e Endpoint, owner string) (tonW5Account, error) {
	var empty tonW5Account
	var state struct {
		Address, Code, Data, Status string
		Balance                     *int64
	}
	if err := s.request(ctx, n, e, "/v2/blockchain/accounts/"+owner, nil, nil, &state); err != nil {
		return empty, apperror.New(apperror.ServiceUnavailable, err)
	}
	if state.Address != owner || state.Balance == nil || *state.Balance < 0 {
		return empty, apperror.New(apperror.ServiceUnavailable, invalid("incorrect TON account response"))
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

type tonDepositMessage struct{ external, authorization *cell.Cell }

func tonEmulatedW5Body(seqno, op uint64, deadline uint32, transfer *tlb.InternalMessage) (*cell.Cell, error) {
	actions, err := wallet.PackV5OutActions([]*wallet.Message{{Mode: wallet.PayGasSeparately | wallet.IgnoreErrors, InternalMessage: transfer}})
	if err != nil {
		return nil, err
	}
	// Both signature slots are zero solely for TonAPI's read-only emulator.
	return cell.BeginCell().MustStoreUInt(op, 32).MustStoreUInt(tonW5ID, 32).MustStoreUInt(uint64(deadline), 32).MustStoreUInt(seqno, 32).MustStoreBuilder(actions).MustStoreSlice(make([]byte, 64), 512).EndCell(), nil
}

func buildTONDepositMessage(user, platform tonW5Account, atoms *big.Int, deadline uint32) (tonDepositMessage, error) {
	var m tonDepositMessage
	pay := &tlb.InternalMessage{IHRDisabled: true, DstAddr: platform.addr, Amount: tlb.FromNanoTON(atoms), Body: cell.BeginCell().EndCell()}
	auth, err := tonEmulatedW5Body(user.seqno, tonW5SignedInternal, deadline, pay)
	if err != nil {
		return m, err
	}
	relay := &tlb.InternalMessage{IHRDisabled: true, Bounce: true, DstAddr: user.addr, Amount: tlb.FromNanoTONU(tonRelayNano), Body: auth}
	dispatch, err := tonEmulatedW5Body(platform.seqno, tonW5SignedExternal, deadline, relay)
	if err != nil {
		return m, err
	}
	ext, err := tlb.ToCell(&tlb.ExternalMessage{DstAddr: platform.addr, Body: dispatch})
	return tonDepositMessage{ext, auth}, err
}
