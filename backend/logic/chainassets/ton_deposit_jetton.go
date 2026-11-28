package chainassets

import (
	"bytes"
	"context"
	"encoding/base64"

	"github.com/0xabcd-k/DexWallet/backend/apperror"
	"github.com/shopspring/decimal"
	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

// Verified six-decimal community test USDT. This master uses ordinary wallet code cells.
const tonDepositMaster = TONDepositTestMaster
const TONDepositTestUSDT = TONTestnet + ":usdt:" + TONDepositTestMaster
const TONDepositTestMaster = "0:f418a04cf196ebc959366844a6cdf53a6fd6fff1eadafc892f05210bba31593e"
const tonJettonMasterCode = "GNW254D/C7RRJUwsdg0J1uSFY4zRQHq7lweHUsPBye4="
const tonJettonWalletCode = "j0Utek39dAZraCNlF3JZ7QVzRDW+drX9S9XYryt8PWg="
const (
	tonJettonActionNano   = TONJettonTransferNano
	TONJettonTransferNano = 50_000_000
	tonJettonForwardNano  = 1
	tonJettonDecimals     = 6
	jettonSendLocked      = 1
	jettonReceiveLocked   = 2
)

type tonDepositJetton struct {
	sender, receiver *address.Address
	balance          decimal.Decimal
	receiverActive   bool
	code             *cell.Cell
	storage          []*tonStorageUsed
}

type tonJettonWalletData struct {
	Status uint8            `tlb:"## 4"`
	Tokens tlb.Coins        `tlb:"."`
	Owner  *address.Address `tlb:"addr"`
	Master *address.Address `tlb:"addr"`
}

func jettonDepositAddress(owner, master *address.Address, code *cell.Cell) *address.Address {
	data := cell.BeginCell().MustStoreUInt(0, 4).MustStoreCoins(0).MustStoreAddr(owner).MustStoreAddr(master).EndCell()
	return (tlb.StateInit{Code: code, Data: data}).CalcAddress(0)
}

func jettonDepositWalletData(state tonQuoteAccount, owner, master *address.Address, code *cell.Cell) (tonJettonWalletData, error) {
	var out tonJettonWalletData
	observed, err := tonDepositCell(state.Code)
	if err != nil || !bytes.Equal(observed.Hash(), code.Hash()) {
		return out, invalid("unexpected test Jetton wallet code")
	}
	data, err := tonDepositCell(state.Data)
	if err != nil {
		return out, err
	}
	d := data.BeginParse()
	if tlb.LoadFromCell(&out, d) != nil || d.RefsNum() != 0 || d.BitsLeft() != 0 || out.Owner == nil || !out.Owner.Equals(owner) || out.Master == nil || !out.Master.Equals(master) {
		return out, invalid("unexpected test Jetton wallet data")
	}
	return out, nil
}

func (s *Service) tonDepositJetton(ctx context.Context, n Network, e Endpoint, user, platform tonW5Account) (*tonDepositJetton, error) {
	master := address.MustParseRawAddr(tonDepositMaster)
	state, err := s.tonQuoteAccount(ctx, n, e, tonDepositMaster)
	if err != nil {
		return nil, err
	}
	code, err := tonDepositCell(state.Code)
	if state.Status != "active" || err != nil || base64.StdEncoding.EncodeToString(code.Hash()) != tonJettonMasterCode {
		return nil, apperror.New(apperror.ServiceUnavailable, invalid("unexpected test Jetton master code"))
	}
	data, err := tonDepositCell(state.Data)
	if err != nil {
		return nil, apperror.New(apperror.ServiceUnavailable, err)
	}
	// The first reference in this master's storage holds the Jetton wallet code.
	walletCode, err := data.BeginParse().LoadRefCell()
	if err != nil || base64.StdEncoding.EncodeToString(walletCode.Hash()) != tonJettonWalletCode {
		return nil, apperror.New(apperror.ServiceUnavailable, invalid("unexpected test Jetton wallet code in master"))
	}
	token := &tonDepositJetton{code: walletCode, sender: jettonDepositAddress(user.addr, master, walletCode), receiver: jettonDepositAddress(platform.addr, master, walletCode)}
	sender, err := s.tonQuoteAccount(ctx, n, e, token.sender.StringRaw())
	if err != nil {
		return nil, err
	}
	if sender.Status == "nonexist" || sender.Status == "uninit" {
		return nil, apperror.New(apperror.InsufficientBalance, nil)
	}
	sw, err := jettonDepositWalletData(sender, user.addr, master, walletCode)
	if err != nil {
		return nil, apperror.New(apperror.ServiceUnavailable, err)
	}
	if sender.Status != "active" || sw.Status&jettonSendLocked != 0 {
		return nil, badTarget("test Jetton wallet cannot send")
	}
	token.storage = append(token.storage, sender.Storage)
	token.balance = decimal.NewFromBigInt(sw.Tokens.Nano(), -tonJettonDecimals)
	recv, err := s.tonQuoteAccount(ctx, n, e, token.receiver.StringRaw())
	if err != nil {
		return nil, err
	}
	switch recv.Status {
	case "nonexist", "uninit":
	case "active":
		rw, err := jettonDepositWalletData(recv, platform.addr, master, walletCode)
		if err != nil {
			return nil, apperror.New(apperror.ServiceUnavailable, err)
		}
		if rw.Status&jettonReceiveLocked != 0 {
			return nil, apperror.New(apperror.ServiceUnavailable, invalid("platform Jetton wallet cannot receive"))
		}
		token.receiverActive = true
		token.storage = append(token.storage, recv.Storage)
	default:
		return nil, apperror.New(apperror.ServiceUnavailable, invalid("unexpected platform Jetton wallet state"))
	}
	return token, nil
}
