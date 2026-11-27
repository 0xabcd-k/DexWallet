package deposit

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
	model "github.com/0xabcd-k/DexWallet/backend/model/deposit"
	"github.com/mr-tron/base58"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

const devnetSponsorPolicy = "devnet-sol-single-order-v1"

const (
	SolWaiting   = "waiting"
	SolSigned    = "signed"
	SolSubmitted = "submitted"
	SolReview    = "review"
	SolFinalized = "finalized"
	SolFailed    = "failed"
)

type SolDepositNetwork interface {
	SolanaBlockHeight(context.Context) (int64, error)
	CheckDevnetDepositFee(context.Context, []byte, chainassets.SolDepositTerms) error
	SubmitSolDeposit(context.Context, []byte) error
	ObserveSolDeposit(context.Context, []byte, chainassets.SolDepositTerms) (chainassets.SolDepositObservation, error)
}

// Each operator invocation authorizes one quote and exact amounts. No signing key
// or automatic test fee budget is installed in the API service.
type SolExecution struct {
	idx    string
	terms  chainassets.SolDepositTerms
	key    ed25519.PrivateKey
	rows   model.SolDepositOrderModel
	quotes model.DepositQuoteConfirmationModel
	chain  SolDepositNetwork
}

func NewSolExecution(conn sqlx.SqlConn, chain SolDepositNetwork, idx string, terms chainassets.SolDepositTerms, key ed25519.PrivateKey) (*SolExecution, error) {
	if !idxSyntax.MatchString(idx) || terms.Recipient == "" || !terms.Valid() || len(key) != ed25519.PrivateKeySize ||
		!key.Equal(ed25519.NewKeyFromSeed(key.Seed())) {
		return nil, errors.New("valid dedicated Devnet sponsor, quote, recipient, gross and fee authorization required")
	}
	return &SolExecution{idx, terms, key, model.NewSolDepositOrderModel(conn), model.NewDepositQuoteConfirmationModel(conn), chain}, nil
}

// Step commits the first signature (the transaction ID) separately from any
// submission. With the immutable user signature and message this is the full wire.
func (s *SolExecution) Step(ctx context.Context) (*model.SolDepositOrder, error) {
	o, err := s.rows.FindOneByQuoteIdx(ctx, s.idx)
	if err != nil {
		return nil, err
	}
	if !o.AcceptedAt.Valid {
		return o, errors.New("SOL order has no accepted user authorization")
	}
	record, err := s.quotes.FindOne(ctx, s.idx)
	if err != nil {
		return o, err
	}
	var q chainassets.DepositPreview
	if err = json.Unmarshal([]byte(record.SnapshotJson), &q); err != nil {
		return o, err
	}
	if q.AssetID != o.AssetId || q.QuoteIdx != o.QuoteIdx || q.SolOrderIdx != o.OrderIdx || q.SolMessage != o.MessageBase64 || q.MessageSHA256 != o.MessageSha256 ||
		q.SolLastValidHeight != o.LastValidHeight || q.SolReviewAt != o.ReviewAfter.UnixMilli() || q.ExpiresAt != record.QuoteExpiresAt.UnixMilli() ||
		o.AcceptedAt.Time.Before(record.ConfirmedAt) || o.AcceptedAt.Time.UnixMilli() >= q.ExpiresAt {
		return o, errors.New("SOL order or acceptance window differs from confirmed quote")
	}
	pub := s.key.Public().(ed25519.PublicKey)
	msg, err := s.terms.Check(q, base58.Encode(pub), o.UserSignature)
	if err != nil {
		return o, err
	}
	if o.ExecutionState == SolReview && !o.TxSignature.Valid {
		return o, nil
	}
	advance := func(state string, signature sql.NullString, fee, slot, funding int64) (*model.SolDepositOrder, error) {
		policy := o.SponsorPolicy
		if state == SolSigned {
			policy = devnetSponsorPolicy
		}
		if err := s.rows.AdvanceExecution(ctx, o, state, signature, policy, fee, slot, funding); err != nil {
			return o, err
		}
		return s.rows.FindOneByQuoteIdx(ctx, s.idx)
	}
	var wire []byte
	if o.TxSignature.Valid {
		if o.SponsorPolicy != devnetSponsorPolicy {
			return o, errors.New("unknown durable Devnet sponsor policy")
		}
		sig, err := base58.Decode(o.TxSignature.String)
		if err != nil || len(sig) != ed25519.SignatureSize || !ed25519.Verify(pub, msg, sig) {
			return o, errors.New("invalid durable sponsor signature")
		}
		if o.ExecutionState == SolFinalized || o.ExecutionState == SolFailed {
			return o, nil
		}
		userSig, _ := base58.Decode(o.UserSignature) // verified against msg above
		wire = append([]byte{2}, sig...)
		wire = append(wire, userSig...)
		wire = append(wire, msg...)
		obs, err := s.chain.ObserveSolDeposit(ctx, wire, s.terms)
		if err == nil && obs.Finalized {
			state := SolFinalized
			if obs.Failed {
				state = SolFailed
			}
			return advance(state, o.TxSignature, obs.Fee, obs.Slot, obs.Funding)
		}
		if o.ExecutionState == SolReview {
			return o, err
		}
		if !time.Now().Before(o.ReviewAfter) {
			row, updateErr := advance(SolReview, o.TxSignature, 0, 0, 0)
			return row, errors.Join(err, updateErr)
		}
		if err != nil || obs.Seen {
			if err == nil && o.ExecutionState == SolSigned {
				return advance(SolSubmitted, o.TxSignature, 0, 0, 0)
			}
			return o, err
		}
	}
	if !time.Now().Before(o.ReviewAfter) {
		return advance(SolReview, o.TxSignature, 0, 0, 0)
	}
	height, err := s.chain.SolanaBlockHeight(ctx)
	if err != nil {
		return o, err
	}
	if height > o.LastValidHeight {
		return advance(SolReview, o.TxSignature, 0, 0, 0)
	}
	if err = s.chain.CheckDevnetDepositFee(ctx, msg, s.terms); err != nil {
		return o, err
	}
	if !time.Now().Before(o.ReviewAfter) {
		return advance(SolReview, o.TxSignature, 0, 0, 0)
	}
	if !o.TxSignature.Valid {
		id := base58.Encode(ed25519.Sign(s.key, msg))
		return advance(SolSigned, sql.NullString{String: id, Valid: true}, 0, 0, 0)
	}
	// A timeout is ambiguous. No replacement blockhash, message or signature
	// is generated. The next step always observes this ID before resubmission.
	if err = s.chain.SubmitSolDeposit(ctx, wire); err != nil {
		return o, err
	}
	if o.ExecutionState == SolSubmitted {
		return o, nil
	}
	return advance(SolSubmitted, o.TxSignature, 0, 0, 0)
}
