package chainassets

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"math/big"
	"net/url"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/apperror"
	"github.com/shopspring/decimal"
	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/ton/wallet"
)

type TONDepositTerms struct {
	SenderPaidGas                           string `json:",omitempty"`
	JettonWalletCode                        string `json:",omitempty"`
	SimulatedGas                            string
	UserKey, PlatformKey, AuthorizationHash string
	UserSeqno, PlatformSeqno, ValidUntil    uint32
}

type TONJettonRelay struct {
	SenderWallet, ReceiverWallet, PaymentHash, InternalHash, NotificationHash, ExcessHash string
}

func TONJettonPaymentKey(sender, body string) string {
	sum := sha256.Sum256([]byte(sender + "/" + body))
	return hex.EncodeToString(sum[:])
}

type TONDepositRelay struct {
	Jetton                                    *TONJettonRelay
	Wire                                      []byte
	UserDigest, PlatformDigest                []byte
	AuthHash, DispatchHash, Sender, Recipient string
	Atoms                                     *big.Int
}

func (q DepositPreview) TONOrderDigest() [32]byte {
	domain := "DexWallet/1/ton-testnet/ton/self-deposit/"
	if q.AssetID == TONDepositTestUSDT {
		domain = "DexWallet/1/" + q.AssetID + "/self-deposit/"
	}
	return sha256.Sum256([]byte(domain + q.QuoteIdx + "/" + q.TON.AuthorizationHash))
}

func tonQuotedWallet(raw, key string, seq uint32) (tonW5Account, error) {
	var a tonW5Account
	b, err := hex.DecodeString(key)
	if err != nil || len(b) != ed25519.PublicKeySize || seq == 0 {
		return a, errors.New("invalid testnet W5 terms")
	}
	init, err := wallet.GetStateInit(b, wallet.ConfigV5R1Final{NetworkGlobalID: tonTestnetGlobalID}, 0)
	if err != nil {
		return a, err
	}
	normalized, err := normalizeAddress(Network{ID: TONTestnet, Family: TON, Testnet: true}, raw)
	addr := init.CalcAddress(0)
	if err != nil || addr.StringRaw() != normalized {
		return a, errors.New("TON key differs from quoted W5")
	}
	return tonW5Account{addr: addr, seqno: uint64(seq), key: b}, nil
}

// An empty signature builds a digest for signing. A supplied signature must
// authenticate the exact same W5 payload before its wire or body hash is used.
func (q DepositPreview) TONRelay(userSig, platformSig []byte) (TONDepositRelay, error) {
	var r TONDepositRelay
	t := q.TON
	scale, attached := 9, int64(TONDepositAttachmentNano)
	isToken := q.AssetID == TONDepositTestUSDT
	if isToken {
		scale = tonJettonDecimals
		attached += tonJettonActionNano
	}
	if t == nil || (!isToken && q.AssetID != tonDepositAsset) || q.GasAssetID != tonDepositAsset || q.PayDecimals != scale || q.GasDecimals != 9 || !q.WalletFeePreview || q.Recipient != q.FeePayer || q.MaxGas != q.EstimatedGas || q.RelayAttached != decimal.New(attached, -9).String() || int64(t.ValidUntil)*1000 <= q.ExpiresAt || !rateSyntax.MatchString(q.PayQuantity) {
		return r, errors.New("confirmed native TON relay quote required")
	}
	user, err := tonQuotedWallet(q.WalletAddress, t.UserKey, t.UserSeqno)
	if err != nil {
		return r, err
	}
	sponsor, err := tonQuotedWallet(q.FeePayer, t.PlatformKey, t.PlatformSeqno)
	if err != nil {
		return r, err
	}
	amount, _ := decimal.NewFromString(q.PayQuantity)
	var token *tonDepositJetton
	if isToken {
		code, err := tonDepositCell(t.JettonWalletCode)
		if err != nil || base64.StdEncoding.EncodeToString(code.Hash()) != tonJettonWalletCode {
			return r, errors.New("invalid quoted test Jetton code")
		}
		master := address.MustParseRawAddr(TONDepositTestMaster)
		token = &tonDepositJetton{sender: jettonDepositAddress(user.addr, master, code), receiver: jettonDepositAddress(sponsor.addr, master, code)}
	} else if t.JettonWalletCode != "" {
		return r, errors.New("unexpected Jetton code")
	}
	nanos := amount.Shift(int32(scale))
	if !nanos.IsInteger() || !nanos.IsPositive() || nanos.BigInt().BitLen() > 120 || user.addr.Equals(sponsor.addr) {
		return r, errors.New("invalid TON deposit amount or payer")
	}
	if (len(userSig) != 0 && len(userSig) != 64) || (len(platformSig) != 0 && len(platformSig) != 64) {
		return r, errors.New("invalid W5 signature length")
	}
	blank := make([]byte, 64)
	unsigned, err := buildTONRelay(user, sponsor, token, nanos.BigInt(), t.ValidUntil, blank, blank)
	if err != nil {
		return r, err
	}
	if hex.EncodeToString(unsigned.external.Hash()) != q.EmulatedMessageHash || hex.EncodeToString(unsigned.userPayload.Hash()) != t.AuthorizationHash {
		return r, errors.New("TON relay differs from original emulation")
	}
	us, ps := userSig, platformSig
	if len(us) == 0 {
		us = blank
	} else if !ed25519.Verify(user.key, unsigned.userPayload.Hash(), us) {
		return r, errors.New("invalid TON user authorization")
	}
	if len(ps) == 0 {
		ps = blank
	}
	m, err := buildTONRelay(user, sponsor, token, nanos.BigInt(), t.ValidUntil, us, ps)
	if err != nil {
		return r, err
	}
	if len(platformSig) > 0 && (len(userSig) == 0 || !ed25519.Verify(sponsor.key, m.platformPayload.Hash(), ps)) {
		return r, errors.New("invalid TON platform signature")
	}
	var jr *TONJettonRelay
	if token != nil {
		internal, notice, excess := tonJettonReceiptBodies(m.queryID, nanos.BigInt(), user, sponsor)
		jr = &TONJettonRelay{token.sender.StringRaw(), token.receiver.StringRaw(), hex.EncodeToString(m.paymentBody.Hash()), hex.EncodeToString(internal.Hash()), hex.EncodeToString(notice.Hash()), hex.EncodeToString(excess.Hash())}
	}
	return TONDepositRelay{Jetton: jr, Wire: m.external.ToBOC(), UserDigest: m.userPayload.Hash(), PlatformDigest: m.platformPayload.Hash(), AuthHash: hex.EncodeToString(m.authorization.Hash()), DispatchHash: hex.EncodeToString(m.dispatch.Hash()), Sender: user.addr.StringRaw(), Recipient: sponsor.addr.StringRaw(), Atoms: nanos.BigInt()}, nil
}

func (s *Service) CheckTONDeposit(ctx context.Context, q DepositPreview, userSig, platformSig []byte) error {
	route := s.quotes.ton[q.AssetID]
	if !s.quotes.tonExecution[q.AssetID] || route == nil || route.platform != q.FeePayer {
		return apperror.New(apperror.Unprocessable, errors.New("TON testnet deposit route disabled or changed"))
	}
	return s.checkTONRelay(ctx, q, userSig, platformSig, route)
}

// RecheckTONRelay is used by the single-order test operator. Only settlement
// reads the locked price; a broadcast uses this explicitly pinned test route.
func (s *Service) RecheckTONRelay(ctx context.Context, q DepositPreview, userSig, platformSig []byte, c TONDepositFeeConfig) error {
	n, _, err := s.tonTestnet()
	if err != nil {
		return err
	}
	route, err := newTONDepositRoute(c, n)
	if err != nil {
		return err
	}
	if route.platform != q.FeePayer {
		return errors.New("operator TON collection address differs from the confirmed quote")
	}
	return s.checkTONRelay(ctx, q, userSig, platformSig, route)
}
func (s *Service) checkTONRelay(ctx context.Context, q DepositPreview, userSig, platformSig []byte, route *tonDepositRoute) error {
	r, err := q.TONRelay(userSig, platformSig)
	if err != nil {
		return err
	}
	now := time.Now()
	if now.Unix() >= int64(q.TON.ValidUntil) {
		return apperror.New(apperror.Conflict, errors.New("TON relay expired"))
	}
	n, _, err := s.tonTestnet()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	user, err := s.tonDepositWallet(ctx, n, route.emulator, r.Sender)
	if err != nil {
		return err
	}
	sponsor, err := s.tonDepositWallet(ctx, n, route.emulator, r.Recipient)
	if err != nil {
		return err
	}
	pay := decimal.NewFromBigInt(r.Atoms, -int32(q.PayDecimals))
	balance := user.balance
	var token *tonDepositJetton
	if r.Jetton != nil {
		token, err = s.tonDepositJetton(ctx, n, route.emulator, user, sponsor)
		if err != nil {
			return err
		}
		if token.sender.StringRaw() != r.Jetton.SenderWallet || token.receiver.StringRaw() != r.Jetton.ReceiverWallet {
			return errors.New("quoted Jetton wallets changed")
		}
		balance = token.balance
	}
	if user.seqno != uint64(q.TON.UserSeqno) || sponsor.seqno != uint64(q.TON.PlatformSeqno) {
		return apperror.New(apperror.Conflict, errors.New("TON W5 sequence changed"))
	}
	if balance.LessThan(pay) {
		return apperror.New(apperror.InsufficientBalance, nil)
	}
	if len(platformSig) == 0 {
		return nil
	}
	at, err := s.tonDepositHead(ctx, n, route.emulator, time.Now)
	if err != nil {
		return err
	}
	// Signatures have been checked locally. An emulator must never receive a spendable authorization.
	m, err := buildTONDepositMessage(user, sponsor, token, r.Atoms, q.TON.ValidUntil)
	if err != nil {
		return err
	}
	var trace tonDepositTrace
	if err = s.request(ctx, n, route.emulator, "/v2/traces/emulate", url.Values{"ignore_signature_check": {"true"}}, map[string]string{"boc": base64.StdEncoding.EncodeToString(m.external.ToBOC())}, &trace); err != nil {
		return err
	}
	b, err := trace.budget(m, user, sponsor, token, pay, at)
	if err != nil {
		return err
	}
	cap, err := decimal.NewFromString(q.MaxGas)
	attached, _ := decimal.NewFromString(q.RelayAttached)
	if err != nil || b.network.GreaterThan(cap) || sponsor.balance.LessThan(attached.Add(b.dispatch)) {
		return errors.New("signed TON execution exceeds confirmed fee or platform balance")
	}
	return nil
}

func (q DepositPreview) BindTONDeposit(userSig, orderSig string) (TONDepositRelay, error) {
	var r TONDepositRelay
	u, err := hex.DecodeString(userSig)
	if err != nil || len(u) != 64 {
		return r, errors.New("invalid TON user signature")
	}
	r, err = q.TONRelay(u, nil)
	if err != nil {
		return r, err
	}
	proof, err := hex.DecodeString(orderSig)
	key, _ := hex.DecodeString(q.TON.UserKey)
	digest := q.TONOrderDigest()
	if err != nil || !ed25519.Verify(key, digest[:], proof) {
		return r, errors.New("TON signature does not bind this order")
	}
	return r, nil
}

func (s *Service) AdmitTONDeposit(ctx context.Context, user int64, q DepositPreview, signature string) error {
	if s.limiter == nil {
		return apperror.New(apperror.ServiceUnavailable, errors.New("TON deposit limiter missing"))
	}
	if err := s.limiter.Allow(ctx, user); err != nil {
		return err
	}
	sig, err := hex.DecodeString(signature)
	if err != nil {
		return err
	}
	return s.CheckTONDeposit(ctx, q, sig, nil)
}
