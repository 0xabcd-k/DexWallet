package chainassets

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"reflect"
	"strconv"

	"github.com/0xabcd-k/DexWallet/backend/internal/solanaspl"
	"github.com/0xabcd-k/DexWallet/backend/model/account"
	"github.com/mr-tron/base58"
	"github.com/shopspring/decimal"
)

// SolDepositTerms pins the recipient and the exact principal plus fee recovery,
// and authorizes network fees and optional token account funding separately.
type SolDepositTerms struct {
	Recipient                  string
	GrossLamports, FeeLamports int64
	USDCAtoms                  uint64
	MaxFundingLamports         int64
}

func (t SolDepositTerms) Valid() bool {
	if t.FeeLamports <= 0 {
		return false
	}
	if t.USDCAtoms > 0 {
		return t.GrossLamports == 0 && t.MaxFundingLamports >= 0
	}
	return t.MaxFundingLamports == 0 && t.GrossLamports > t.FeeLamports
}

func (t SolDepositTerms) token(sender string) (solanaspl.Transfer, error) {
	return solanaspl.New(sender, t.Recipient, devnetUSDCMint, strconv.FormatUint(t.USDCAtoms, 10), t.MaxFundingLamports > 0)
}

// Reconstruct the quoted message independently of the stored wire.
// Only the original recent blockhash comes from the signed bytes.
func (t SolDepositTerms) Check(q DepositPreview, payer, userSignature string) ([]byte, error) {
	if len(q.SolOrderIdx) != 32 || !t.Valid() || q.GasAssetID != solanaDepositAsset ||
		!q.WalletFeePreview || q.FeePayer != payer || q.Recipient != t.Recipient ||
		q.EstimatedGas != q.MaxGas || decimal.New(t.FeeLamports, -9).String() != q.MaxGas {
		return nil, errors.New("SOL order differs from authorized Devnet route, gross amount or fee")
	}
	if _, err := hex.DecodeString(q.SolOrderIdx); err != nil {
		return nil, err
	}
	route, err := newSolanaDepositRoute(SolanaDepositFeeConfig{payer, t.Recipient})
	if err != nil {
		return nil, err
	}
	source, err := depositKey(q.WalletAddress, true)
	if err != nil || source == route.payer || source == route.recipient {
		return nil, errors.New("invalid SOL deposit source")
	}
	msg, err := base64.StdEncoding.DecodeString(q.SolMessage)
	if err != nil || fmt.Sprintf("%x", sha256.Sum256(msg)) != q.MessageSHA256 {
		return nil, errors.New("invalid deposit message hash")
	}
	var expected []byte
	if t.USDCAtoms > 0 {
		if q.AssetID != solanaDepositUSDC || decimal.NewFromBigInt(new(big.Int).SetUint64(t.USDCAtoms), -6).String() != q.PayQuantity || decimal.New(t.MaxFundingLamports, -9).String() != q.AccountFunding {
			return nil, errors.New("USDC amount or account funding differs from authorized terms")
		}
		transfer, err := t.token(q.WalletAddress)
		if err != nil {
			return nil, err
		}
		offset := 4 + 32*(len(transfer.Keys())+2) // sponsor and memo program
		if len(msg) < offset+32 {
			return nil, errors.New("truncated USDC deposit message")
		}
		expected, err = transfer.SponsoredMemoMessage(base58.Encode(msg[offset:offset+32]), payer, "dexwallet:deposit:"+q.SolOrderIdx)
		if err != nil {
			return nil, err
		}
	} else {
		const offset = 4 + 5*32
		if q.AssetID != solanaDepositAsset || q.AccountFunding != "" || decimal.New(t.GrossLamports, -9).String() != q.PayQuantity || len(msg) < offset+32 {
			return nil, errors.New("invalid SOL deposit terms")
		}
		expected = route.message(source, msg[offset:offset+32], uint64(t.GrossLamports), q.SolOrderIdx)
	}
	if !bytes.Equal(expected, msg) {
		return nil, errors.New("deposit instructions differ from authorization")
	}
	sig, err := base58.Decode(userSignature)
	if err != nil || len(sig) != ed25519.SignatureSize || !ed25519.Verify(source[:], msg, sig) {
		return nil, errors.New("invalid SOL deposit user signature")
	}
	return msg, nil
}

func (s *Service) CheckDevnetDepositFee(ctx context.Context, msg []byte, terms SolDepositTerms) error {
	var result struct {
		Context struct{ Slot uint64 }
		Value   *uint64
	}
	if err := s.devnetRPC(ctx, "getFeeForMessage", []any{base64.StdEncoding.EncodeToString(msg), map[string]string{"commitment": "confirmed"}}, &result); err != nil {
		return err
	}
	if result.Context.Slot == 0 || result.Value == nil || *result.Value != uint64(terms.FeeLamports) {
		return errors.New("Devnet blockhash unavailable or original fee changed")
	}
	if terms.MaxFundingLamports > 0 {
		var rent uint64
		if err := s.devnetRPC(ctx, "getMinimumBalanceForRentExemption", []any{legacyTokenAccountSize, map[string]string{"commitment": "confirmed"}}, &rent); err != nil {
			return err
		}
		if rent == 0 || rent > uint64(terms.MaxFundingLamports) {
			return errors.New("Devnet storage funding exceeds original sponsor authorization")
		}
	}
	return nil
}

func (s *Service) SubmitSolDeposit(ctx context.Context, wire []byte) error {
	return s.submitSolana(ctx, wire, 2)
}

type SolDepositObservation struct {
	Seen, Finalized, Failed bool
	Slot, Fee, Funding      int64
}

// Read history first. Only finalized metadata for the exact signed wire can
// close an order. Until then a missing result is an observation, not a failure.
func (s *Service) ObserveSolDeposit(ctx context.Context, wire []byte, terms SolDepositTerms) (SolDepositObservation, error) {
	var obs SolDepositObservation
	if len(wire) <= 129 || wire[0] != 2 {
		return obs, errors.New("invalid deposit envelope")
	}
	id := base58.Encode(wire[1:65])
	var statuses struct {
		Context struct{ Slot int64 }
		Value   []*struct {
			Slot               int64
			ConfirmationStatus string
			Err                json.RawMessage
		}
	}
	err := s.devnetRPC(ctx, "getSignatureStatuses", []any{[]string{id}, map[string]bool{"searchTransactionHistory": true}}, &statuses)
	if err != nil {
		return obs, err
	}
	if statuses.Context.Slot <= 0 || len(statuses.Value) != 1 {
		return obs, errors.New("missing Devnet status result")
	}
	st := statuses.Value[0]
	if st != nil {
		obs.Seen = true
		switch st.ConfirmationStatus {
		case "processed", "confirmed":
			return obs, nil
		case "finalized":
		default:
			return obs, errors.New("unknown Solana confirmation status")
		}
	}
	raw, err := s.devnetRPCRaw(ctx, "getTransaction", []any{id, map[string]any{"encoding": "base64", "commitment": "finalized", "maxSupportedTransactionVersion": 0}})
	if err != nil {
		return obs, err
	}
	if string(raw) == "null" {
		return obs, nil
	}
	obs.Seen = true
	// A transaction found without its corresponding status precludes resubmission.
	if st == nil {
		return obs, nil
	}
	var tx struct {
		Slot        int64
		Transaction []string
		Meta        *struct {
			Err                                 json.RawMessage
			Fee                                 int64
			PreBalances, PostBalances           []*uint64
			PreTokenBalances, PostTokenBalances []solanaspl.TokenBalance
		}
	}
	if err := json.Unmarshal(raw, &tx); err != nil {
		return obs, err
	}
	m := tx.Meta
	if st.Slot <= 0 || st.Slot != tx.Slot || m == nil || len(tx.Transaction) != 2 ||
		tx.Transaction[1] != "base64" || tx.Transaction[0] != base64.StdEncoding.EncodeToString(wire) {
		return obs, errors.New("finalized Devnet transaction differs from original wire or status slot")
	}
	if len(m.Err) == 0 || !bytes.Equal(st.Err, m.Err) {
		return obs, errors.New("inconsistent finalized Solana outcome")
	}
	failed := string(m.Err) != "null"
	if failed && m.Err[0] != '{' && m.Err[0] != '"' {
		return obs, errors.New("invalid finalized transaction error")
	}
	if !terms.Valid() || m.Fee != terms.FeeLamports || len(m.PreBalances) != len(m.PostBalances) {
		return obs, errors.New("unexpected finalized fee or balance count")
	}
	for i := range m.PreBalances {
		if m.PreBalances[i] == nil || m.PostBalances[i] == nil {
			return obs, errors.New("missing finalized balance")
		}
	}
	if terms.USDCAtoms > 0 {
		const senderOffset = 129 + 4 + 32
		if len(wire) < senderOffset+32 {
			return obs, errors.New("missing USDC authority")
		}
		t, err := terms.token(base58.Encode(wire[senderOffset : senderOffset+32]))
		if err != nil {
			return obs, err
		}
		funding, err := terms.checkTokenReceipt(t, failed, m.PreBalances, m.PostBalances, m.PreTokenBalances, m.PostTokenBalances)
		if err != nil {
			return obs, err
		}
		return SolDepositObservation{Seen: true, Finalized: true, Failed: failed, Slot: tx.Slot, Fee: m.Fee, Funding: funding}, nil
	}
	if len(m.PreBalances) != 5 {
		return obs, errors.New("finalized SOL fee or account balances differ from original terms")
	}
	pre, post := m.PreBalances, m.PostBalances
	if *pre[0] < *post[0] || *pre[0]-*post[0] != uint64(m.Fee) {
		return obs, errors.New("unexpected finalized fee payer delta")
	}
	gross := uint64(terms.GrossLamports)
	if failed {
		gross = 0
	}
	if *pre[1] < *post[1] || *pre[1]-*post[1] != gross || *post[2] < *pre[2] || *post[2]-*pre[2] != gross {
		return obs, fmt.Errorf("unexpected finalized wallet or recipient balance delta at slot %d", tx.Slot)
	}
	return SolDepositObservation{Seen: true, Finalized: true, Failed: failed, Slot: tx.Slot, Fee: m.Fee}, nil
}

// Reconcile both token accounts and all SOL movement against the original wire.
func (terms SolDepositTerms) checkTokenReceipt(t solanaspl.Transfer, failed bool, pre, post []*uint64, preTokens, postTokens []solanaspl.TokenBalance) (int64, error) {
	if len(pre) != len(t.Keys())+2 {
		return 0, errors.New("unexpected sponsored USDC account count")
	}
	source, dest := 2, 3
	if t.CreateATA {
		source, dest = 3, 2
	}
	for i := 1; i < len(pre); i++ {
		if i != dest && *pre[i] != *post[i] {
			return 0, errors.New("unexpected USDC deposit SOL movement")
		}
	}
	if *pre[dest] > *post[dest] {
		return 0, errors.New("destination ATA lost SOL")
	}
	funding := *post[dest] - *pre[dest]
	if funding > uint64(terms.MaxFundingLamports) || *pre[0] < *post[0] || *pre[0]-*post[0] < funding || *pre[0]-*post[0]-funding != uint64(terms.FeeLamports) {
		return 0, errors.New("USDC sponsor fee and storage funding do not reconcile")
	}
	if funding > 0 {
		for _, balance := range preTokens {
			if balance.AccountIndex != nil && *balance.AccountIndex == dest {
				return 0, errors.New("storage charged for an existing token account")
			}
		}
	}
	if failed {
		if funding != 0 || !reflect.DeepEqual(preTokens, postTokens) {
			return 0, errors.New("failed USDC deposit moved funds")
		}
	} else if err := t.CheckTokenDeltas(source, dest, preTokens, postTokens); err != nil {
		return 0, err
	}
	return int64(funding), nil
}

// Settle at both rates locked by the quote, with one rounding at the USD boundary.
func (q DepositPreview) FinalizedSolUSD(feeLamports, fundingLamports int64, userSignature string) (decimal.Decimal, error) {
	for _, v := range []string{q.USDPerCoin, q.GasUSDPerCoin, q.PayQuantity} {
		if !rateSyntax.MatchString(v) {
			return decimal.Zero, errors.New("invalid locked rate or quantity")
		}
	}
	rate, _ := decimal.NewFromString(q.USDPerCoin)
	gasRate, _ := decimal.NewFromString(q.GasUSDPerCoin)
	gross, _ := decimal.NewFromString(q.PayQuantity)
	terms := SolDepositTerms{Recipient: q.Recipient, FeeLamports: feeLamports}
	units := gross.Shift(9)
	if q.AssetID == solanaDepositUSDC {
		if !rateSyntax.MatchString(q.AccountFunding) {
			return decimal.Zero, errors.New("invalid quoted storage funding")
		}
		cap, _ := decimal.NewFromString(q.AccountFunding)
		if !cap.Shift(9).IsInteger() || !cap.Shift(9).BigInt().IsInt64() {
			return decimal.Zero, errors.New("invalid quoted storage funding")
		}
		terms.MaxFundingLamports = cap.Shift(9).IntPart()
		units = gross.Shift(6)
	} else if !rate.Equal(gasRate) {
		return decimal.Zero, errors.New("inconsistent native SOL price")
	}
	if !units.IsInteger() || !units.BigInt().IsUint64() || fundingLamports < 0 || fundingLamports > terms.MaxFundingLamports {
		return decimal.Zero, errors.New("invalid finalized quantity or funding")
	}
	if q.AssetID == solanaDepositUSDC {
		terms.USDCAtoms = units.BigInt().Uint64()
	} else {
		if !units.BigInt().IsInt64() {
			return decimal.Zero, errors.New("SOL principal exceeds execution budget range")
		}
		terms.GrossLamports = units.IntPart()
	}
	if _, err := terms.Check(q, q.FeePayer, userSignature); err != nil {
		return decimal.Zero, err
	}
	usd, _ := account.Lookup(account.KindUSD)
	feeUSD := decimal.New(feeLamports, -9).Mul(gasRate)
	net := gross.Mul(rate).Sub(feeUSD).Truncate(usd.Scale)
	if !rate.IsPositive() || !gasRate.IsPositive() || !net.IsPositive() || net.StringFixed(usd.Scale) != q.NetUSD || feeUSD.String() != q.GasUSD {
		return decimal.Zero, errors.New("finalized net credit differs from accepted quote")
	}
	return net, usd.ValidateBalances(net, decimal.Zero)
}
