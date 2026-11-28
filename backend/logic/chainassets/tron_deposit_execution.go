package chainassets

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"github.com/0xabcd-k/DexWallet/backend/apperror"
	"github.com/0xabcd-k/DexWallet/backend/internal/tronnative"
	"github.com/shopspring/decimal"
)

// The chain signature is public and covers no off-chain beneficiary. Bind it
// separately to the authenticated user's immutable quote without a paid memo.
func NileDepositOrderDigest(idx, txID string) [32]byte {
	return sha256.Sum256([]byte("DexWallet/1/tron-nile/trx/self-deposit/" + idx + "/" + txID))
}

func (q DepositPreview) NileDepositTransfer() (tronnative.Transfer, error) {
	var t tronnative.Transfer
	if q.AssetID != TRONNileTRX || q.GasAssetID != TRONNileTRX || !q.WalletFeePreview || q.TRONReference == nil || q.EstimatedGas != "0" || q.MaxGas != "0" || q.DelegatedEnergy != 0 || !rateSyntax.MatchString(q.PayQuantity) {
		return t, errors.New("signed Nile TRX wallet quote required")
	}
	if _, err := tronnative.AddressBytes(q.FeePayer); err != nil || q.FeePayer == q.WalletAddress {
		return t, errors.New("invalid Nile resource owner")
	}
	amount, _ := decimal.NewFromString(q.PayQuantity)
	sun := amount.Shift(6)
	if !sun.IsInteger() || !sun.IsPositive() || !sun.BigInt().IsInt64() {
		return t, errors.New("invalid TRX precision or amount")
	}
	t = tronnative.Transfer{Reference: *q.TRONReference, Expiration: q.TRONReference.Timestamp + tronnative.Lifetime.Milliseconds(), Sender: q.WalletAddress, Recipient: q.Recipient, Sun: sun.IntPart()}
	raw, err := t.Raw()
	if err != nil || tronnative.TxID(raw) != q.MessageSHA256 || tronnative.SingleSignatureBandwidth(raw) != q.DelegatedBandwidth || t.Expiration <= q.ExpiresAt {
		return t, errors.New("Nile deposit differs from confirmed transfer or bandwidth")
	}
	return t, nil
}

func (q DepositPreview) BindNileDeposit(userSig, orderSig string) (tronnative.Signed, error) {
	t, err := q.NileDepositTransfer()
	if err != nil {
		return tronnative.Signed{}, err
	}
	if _, err = tronnative.VerifyOwner(t.Sender, NileDepositOrderDigest(q.QuoteIdx, q.MessageSHA256), orderSig); err != nil {
		return tronnative.Signed{}, err
	}
	raw, _ := t.Raw()
	return t.Bind(hex.EncodeToString(raw), q.MessageSHA256, userSig)
}

func (s *Service) AdmitNileDeposit(ctx context.Context, user int64, q DepositPreview) error {
	r := s.quotes.tron[q.AssetID]
	if !s.quotes.tronTRXExecution || r == nil || r.resourceOwner != q.FeePayer || r.recipient != q.Recipient {
		return apperror.New(apperror.Unprocessable, errors.New("Nile deposit route disabled or changed"))
	}
	if s.limiter == nil {
		return apperror.New(apperror.ServiceUnavailable, errors.New("missing Nile limiter"))
	}
	if err := s.limiter.Allow(ctx, user); err != nil {
		return err
	}
	return s.CheckNileDepositResources(ctx, q)
}

// Recheck the original payer, single owner, active recipient and full platform
// delegation immediately before broadcasting the exact previously signed wire.
func (s *Service) CheckNileDepositResources(ctx context.Context, q DepositPreview) error {
	t, err := q.NileDepositTransfer()
	if err != nil {
		return err
	}
	route, err := newNileDepositRoute(TRONDepositFeeConfig{ResourceOwner: q.FeePayer, Recipient: q.Recipient})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	n, e, err := s.nileFull(ctx)
	if err != nil {
		return err
	}
	head, err := s.nileDepositHead(ctx, n, e)
	if err != nil {
		return err
	}
	if head.Height < t.Reference.Height || head.Timestamp < t.Reference.Timestamp || (head.Height == t.Reference.Height && head.ID != t.Reference.ID) {
		return apperror.New(apperror.ServiceUnavailable, errors.New("Nile resource node behind or inconsistent with original reference"))
	}
	wallet, _ := tronnative.AddressBytes(t.Sender)
	_, err = estimateNileResources(s.nileQuoteReader(ctx, n, e), hex.EncodeToString(wallet), route, t, q.DelegatedBandwidth)
	return err
}
