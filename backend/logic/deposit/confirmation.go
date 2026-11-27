// Package deposit records the terms a user saw before authorizing a testnet deposit.
package deposit

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/apperror"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
	model "github.com/0xabcd-k/DexWallet/backend/model/deposit"
	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

const previewKey = ":deposit_quote:preview:"

var idxSyntax = regexp.MustCompile("^[0-9a-f]{32}$")

type Confirmations struct {
	rows      model.DepositQuoteConfirmationModel
	cache     *redis.Client
	namespace string
}

func NewConfirmations(db sqlx.SqlConn, cache *redis.Client, namespace string) *Confirmations {
	return &Confirmations{model.NewDepositQuoteConfirmationModel(db), cache, namespace}
}

func (s *Confirmations) key(user int64, idx string) string {
	return fmt.Sprintf("%s%s%d:%s", s.namespace, previewKey, user, idx)
}

func (s *Confirmations) Offer(ctx context.Context, user int64, q chainassets.DepositPreview) (chainassets.DepositPreview, error) {
	if q.WalletAddress == "" || !q.WalletFeePreview || q.Recipient == "" || q.FeePayer == "" {
		return q, fmt.Errorf("wallet-specific deposit terms required")
	}
	if s.cache == nil {
		return q, apperror.New(apperror.ServiceUnavailable, errors.New("deposit quote offers disabled"))
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return q, err
	}
	q.QuoteIdx = hex.EncodeToString(nonce[:])
	q.MaxGas = q.EstimatedGas // This confirmation permits no increase above the displayed network fee.
	snapshot, err := json.Marshal(q)
	if err != nil {
		return q, err
	}
	ttl := time.Until(time.UnixMilli(q.ExpiresAt))
	if ttl < time.Millisecond {
		return q, apperror.New(apperror.ServiceUnavailable, errors.New("wallet quote expired"))
	}
	ok, err := s.cache.SetNX(ctx, s.key(user, q.QuoteIdx), snapshot, ttl).Result()
	if err != nil {
		return q, apperror.New(apperror.ServiceUnavailable, err)
	}
	if !ok {
		return q, apperror.New(apperror.ServiceUnavailable, errors.New("quote idx collision"))
	}
	return q, nil
}

type Confirmation struct {
	Quote       chainassets.DepositPreview
	ConfirmedAt time.Time
}

func (s *Confirmations) Get(ctx context.Context, user int64, idx string) (Confirmation, error) {
	var result Confirmation
	if !idxSyntax.MatchString(idx) {
		return result, apperror.New(apperror.InvalidArgument, nil)
	}
	row, err := s.rows.ForUser(ctx, idx, user)
	if errors.Is(err, sql.ErrNoRows) {
		return result, apperror.New(apperror.NotFound, nil)
	}
	if err != nil {
		return result, err
	}
	result.ConfirmedAt = row.ConfirmedAt
	err = json.Unmarshal([]byte(row.SnapshotJson), &result.Quote)
	return result, err
}

func (s *Confirmations) Confirm(ctx context.Context, user int64, idx string) (Confirmation, error) {
	// After a lost response, the durable record wins even if the offer or its rate source is gone.
	existing, err := s.Get(ctx, user, idx)
	if err == nil || s.cache == nil {
		return existing, err
	}
	var business *apperror.Error
	if !errors.As(err, &business) || business.Code != apperror.NotFound {
		return existing, err
	}
	snapshot, err := s.cache.Get(ctx, s.key(user, idx)).Bytes()
	if errors.Is(err, redis.Nil) {
		return s.Get(ctx, user, idx)
	}
	if err != nil {
		return Confirmation{}, apperror.New(apperror.ServiceUnavailable, err)
	}
	var q chainassets.DepositPreview
	if err := json.Unmarshal(snapshot, &q); err != nil {
		return Confirmation{}, err
	}
	if err := s.rows.ConfirmBeforeExpiry(ctx, idx, user, string(snapshot), time.UnixMilli(q.QuotedAt).UTC(), time.UnixMilli(q.ExpiresAt).UTC()); err != nil {
		return Confirmation{}, err
	}
	return s.Get(ctx, user, idx)
}
