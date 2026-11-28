package chainassets

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/apperror"
	"github.com/mr-tron/base58"
	"github.com/shopspring/decimal"
)

// Check the configured route and simulate exactly the priced message against Devnet.
// The sponsor signature slot remains empty. Simulation never broadcasts a transaction.
func (s *Service) CheckSolDeposit(ctx context.Context, user int64, q DepositPreview, msg, userSignature []byte) error {
	route := s.quotes.solana[q.AssetID]
	if (q.Beneficiary != nil && !s.SupportsBeneficiaryDeposit(q.AssetID)) || q.SolOrderIdx == "" || route == nil || s.quotes.solReviewAfter[q.AssetID] <= 0 ||
		q.Recipient != base58.Encode(route.recipient[:]) || q.FeePayer != base58.Encode(route.payer[:]) {
		return apperror.New(apperror.Unprocessable, errors.New("SOL deposit authorization unavailable for this quote"))
	}
	if time.Now().UnixMilli() >= q.ExpiresAt {
		return apperror.New(apperror.Conflict, errors.New("quote expired"))
	}
	if s.limiter == nil {
		return apperror.New(apperror.ServiceUnavailable, errors.New("missing chain query limiter"))
	}
	if err := s.limiter.Allow(ctx, user); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	height, err := s.SolanaBlockHeight(ctx) // also checks the genesis hash
	if err != nil {
		return apperror.New(apperror.ServiceUnavailable, err)
	}
	if height > q.SolLastValidHeight {
		return apperror.New(apperror.Conflict, errors.New("quoted blockhash expired"))
	}
	n := s.quotes.assets[q.AssetID].network
	fee, err := s.messageFee(ctx, n, msg)
	if err != nil {
		return apperror.New(apperror.ServiceUnavailable, err)
	}
	if fee > uint64(1<<63-1) || decimal.New(int64(fee), -9).String() != q.MaxGas {
		return apperror.New(apperror.Conflict, errors.New("network fee differs from confirmed quote"))
	}
	const slots = 2
	wire := make([]byte, 1+slots*ed25519.SignatureSize, 1+slots*ed25519.SignatureSize+len(msg))
	wire[0] = slots
	copy(wire[1+ed25519.SignatureSize:], userSignature)
	wire = append(wire, msg...)
	var result struct {
		Context struct{ Slot uint64 }
		Value   struct{ Err json.RawMessage }
	}
	err = s.rpc(ctx, n, s.endpoints[n.ID], "simulateTransaction", []any{base64.StdEncoding.EncodeToString(wire), map[string]any{"encoding": "base64", "commitment": "confirmed", "sigVerify": false, "replaceRecentBlockhash": false}}, &result)
	if err != nil {
		return apperror.New(apperror.ServiceUnavailable, err)
	}
	if result.Context.Slot == 0 || len(result.Value.Err) == 0 {
		return apperror.New(apperror.ServiceUnavailable, errors.New("missing SOL simulation result"))
	}
	if string(result.Value.Err) != "null" {
		return apperror.New(apperror.Unprocessable, fmt.Errorf("Devnet deposit simulation failed: %s", result.Value.Err))
	}
	return nil
}
