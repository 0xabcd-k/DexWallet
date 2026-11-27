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

	"github.com/0xabcd-k/DexWallet/backend/model/account"
	"github.com/mr-tron/base58"
	"github.com/shopspring/decimal"
)

// SolDepositTerms pins the recipient and the exact principal plus fee recovery,
// and limits the test sponsor's exposure to the fee for this single order.
type SolDepositTerms struct {
	Recipient                  string
	GrossLamports, FeeLamports int64
}

// Reconstruct the two-instruction message independently of the stored wire.
// Only the original recent blockhash comes from the signed bytes.
func (t SolDepositTerms) Check(q DepositPreview, payer, userSignature string) ([]byte, error) {
	if len(q.SolOrderIdx) != 32 || q.AssetID != solanaDepositAsset || q.GasAssetID != solanaDepositAsset ||
		!q.WalletFeePreview || q.FeePayer != payer || q.Recipient != t.Recipient || t.FeeLamports <= 0 || t.GrossLamports <= t.FeeLamports ||
		q.EstimatedGas != q.MaxGas || decimal.New(t.FeeLamports, -9).String() != q.MaxGas || decimal.New(t.GrossLamports, -9).String() != q.PayQuantity {
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
	const blockhashOffset = 4 + 5*32 // legacy header + account count, then five public keys
	if err != nil || len(msg) < blockhashOffset+32 || fmt.Sprintf("%x", sha256.Sum256(msg)) != q.MessageSHA256 ||
		!bytes.Equal(msg, route.message(source, msg[blockhashOffset:blockhashOffset+32], uint64(t.GrossLamports), q.SolOrderIdx)) {
		return nil, errors.New("invalid SOL deposit message")
	}
	sig, err := base58.Decode(userSignature)
	if err != nil || len(sig) != ed25519.SignatureSize || !ed25519.Verify(source[:], msg, sig) {
		return nil, errors.New("invalid SOL deposit user signature")
	}
	return msg, nil
}

func (s *Service) CheckDevnetDepositFee(ctx context.Context, msg []byte, want int64) error {
	var result struct {
		Context struct{ Slot uint64 }
		Value   *uint64
	}
	if err := s.devnetRPC(ctx, "getFeeForMessage", []any{base64.StdEncoding.EncodeToString(msg), map[string]string{"commitment": "confirmed"}}, &result); err != nil {
		return err
	}
	if result.Context.Slot == 0 || result.Value == nil || *result.Value != uint64(want) {
		return errors.New("Devnet blockhash unavailable or original fee changed")
	}
	return nil
}

func (s *Service) SubmitSolDeposit(ctx context.Context, wire []byte) error {
	return s.submitSolana(ctx, wire, 2)
}

type SolDepositObservation struct {
	Seen, Finalized, Failed bool
	Slot, Fee               int64
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
			Err                       json.RawMessage
			Fee                       int64
			PreBalances, PostBalances []uint64
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
	if m.Fee != terms.FeeLamports || len(m.PreBalances) != 5 || len(m.PostBalances) != 5 {
		return obs, errors.New("finalized SOL fee or account balances differ from original terms")
	}
	pre, post := m.PreBalances, m.PostBalances
	if pre[0] < post[0] || pre[0]-post[0] != uint64(m.Fee) {
		return obs, errors.New("unexpected finalized fee payer delta")
	}
	gross := uint64(terms.GrossLamports)
	if failed {
		gross = 0
	}
	if pre[1] < post[1] || pre[1]-post[1] != gross || post[2] < pre[2] || post[2]-pre[2] != gross {
		return obs, fmt.Errorf("unexpected finalized wallet or recipient balance delta at slot %d", tx.Slot)
	}
	return SolDepositObservation{Seen: true, Finalized: true, Failed: failed, Slot: tx.Slot, Fee: m.Fee}, nil
}

// FinalizedSolUSD uses the exact gross SOL transfer verified on finalization
// and its actual fee. Both are denominated in SOL at the same locked rate.
func (q DepositPreview) FinalizedSolUSD(feeLamports int64, userSignature string) (decimal.Decimal, error) {
	fee := decimal.New(feeLamports, -9)
	if q.USDPerCoin != q.GasUSDPerCoin || !rateSyntax.MatchString(q.USDPerCoin) || !rateSyntax.MatchString(q.PayQuantity) {
		return decimal.Zero, errors.New("invalid SOL quantity or locked exchange rate")
	}
	rate, _ := decimal.NewFromString(q.USDPerCoin)
	gross, _ := decimal.NewFromString(q.PayQuantity)
	lamports := gross.Shift(9)
	if !lamports.IsInteger() || !lamports.BigInt().IsInt64() {
		return decimal.Zero, errors.New("invalid finalized SOL quantity")
	}
	terms := SolDepositTerms{Recipient: q.Recipient, GrossLamports: lamports.IntPart(), FeeLamports: feeLamports}
	if _, err := terms.Check(q, q.FeePayer, userSignature); err != nil {
		return decimal.Zero, err
	}
	usd, _ := account.Lookup(account.KindUSD)
	net := gross.Sub(fee).Mul(rate).Truncate(usd.Scale)
	if !rate.IsPositive() || !net.IsPositive() || net.StringFixed(usd.Scale) != q.NetUSD || fee.Mul(rate).String() != q.GasUSD {
		return decimal.Zero, errors.New("finalized SOL net credit differs from accepted quote")
	}
	return net, usd.ValidateBalances(net, decimal.Zero)
}
