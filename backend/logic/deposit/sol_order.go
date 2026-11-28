package deposit

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/apperror"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
	"github.com/0xabcd-k/DexWallet/backend/logic/user"
	model "github.com/0xabcd-k/DexWallet/backend/model/deposit"
	usermodel "github.com/0xabcd-k/DexWallet/backend/model/user"
	"github.com/0xabcd-k/DexWallet/backend/transaction"
	"github.com/mr-tron/base58"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

const (
	AwaitingSignature = "awaiting_signature"
	Accepted          = "accepted"
)

type SolOrders struct {
	rows   model.SolDepositOrderModel
	quotes *Confirmations
	users  *user.Service
	tx     *transaction.Runner
	chain  *chainassets.Service
}

func NewSolOrders(conn sqlx.SqlConn, quotes *Confirmations, users *user.Service, tx *transaction.Runner, chain *chainassets.Service) *SolOrders {
	return &SolOrders{model.NewSolDepositOrderModel(conn), quotes, users, tx, chain}
}

type SolOrder struct {
	*model.SolDepositOrder
	QuoteExpiresAt int64
	BeneficiaryIdx string
}

func (s *SolOrders) Get(ctx context.Context, owner int64, idx string) (SolOrder, error) {
	record, err := s.quotes.Get(ctx, owner, idx)
	if err != nil {
		return SolOrder{}, err
	}
	row, err := s.rows.FindOneByQuoteIdx(ctx, idx)
	if errors.Is(err, sql.ErrNoRows) {
		return SolOrder{}, apperror.New(apperror.NotFound, nil)
	}
	return SolOrder{row, record.Quote.ExpiresAt, record.Quote.Beneficiary.PublicIdx()}, err
}

func (s *SolOrders) Prepare(ctx context.Context, actor user.Actor, idx string) (SolOrder, error) {
	return s.process(ctx, actor, idx, "")
}
func (s *SolOrders) Authorize(ctx context.Context, actor user.Actor, idx, signature string) (SolOrder, error) {
	if len(signature) < 64 || len(signature) > 88 {
		return SolOrder{}, apperror.New(apperror.InvalidArgument, nil)
	}
	return s.process(ctx, actor, idx, signature)
}

// One immutable quote, one order, one sponsored message. A second confirmation never
// changes the recipient, blockhash, price or message of an outstanding attempt.
func (s *SolOrders) process(ctx context.Context, actor user.Actor, idx, signature string) (SolOrder, error) {
	record, err := s.quotes.Get(ctx, actor.UserID, idx)
	if err != nil {
		return SolOrder{}, err
	}
	q := record.Quote
	if q.SolOrderIdx == "" {
		return SolOrder{}, apperror.New(apperror.Unprocessable, errors.New("quote has no SOL signing terms"))
	}
	row, err := s.rows.FindOneByQuoteIdx(ctx, idx)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return SolOrder{}, err
	}
	if row == nil && signature != "" {
		return SolOrder{}, apperror.New(apperror.NotFound, nil)
	}
	if row != nil && (signature == "" || row.AcceptedAt.Valid) {
		return solReplay(row, q, signature)
	}
	msg, err := base64.StdEncoding.DecodeString(q.SolMessage)
	if err != nil || fmt.Sprintf("%x", sha256.Sum256(msg)) != q.MessageSHA256 {
		return SolOrder{}, errors.New("stored SOL quote message hash mismatch")
	}
	var sig []byte
	if signature != "" {
		if row.AssetId != q.AssetID || row.MessageBase64 != q.SolMessage || row.OrderIdx != q.SolOrderIdx || row.MessageSha256 != q.MessageSHA256 || row.LastValidHeight != q.SolLastValidHeight {
			return SolOrder{}, errors.New("SOL order differs from its confirmed quote")
		}
		pub, e := base58.Decode(q.WalletAddress)
		if e != nil || len(pub) != ed25519.PublicKeySize {
			return SolOrder{}, errors.New("invalid stored SOL signer")
		}
		sig, err = base58.Decode(signature)
		if err != nil || len(sig) != ed25519.SignatureSize || !ed25519.Verify(pub, msg, sig) {
			return SolOrder{}, apperror.New(apperror.InvalidArgument, errors.New("incorrect wallet signature for deposit order"))
		}
	}
	if err = s.chain.CheckSolDeposit(ctx, actor.UserID, q, msg, sig); err != nil {
		return SolOrder{}, err
	}
	return s.persist(ctx, actor, record, signature)
}

// The same user lock serializes order admission, signature acceptance and identity changes.
// RPC and cryptographic checks finish before acquiring it. Recheck the quote deadline after waiting.
func (s *SolOrders) persist(ctx context.Context, actor user.Actor, record Confirmation, signature string) (SolOrder, error) {
	q := record.Quote
	var result SolOrder
	err := s.tx.TransactCtx(ctx, func(ctx context.Context, session sqlx.Session) error {
		u, err := s.users.CheckActorInSession(ctx, session, actor)
		if err != nil {
			return err
		}
		conn := sqlx.NewSqlConnFromSession(session)
		rows := model.NewSolDepositOrderModel(conn)
		existing, err := rows.FindOneByQuoteIdx(ctx, q.QuoteIdx)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if existing != nil && (signature == "" || existing.AcceptedAt.Valid) {
			result, err = solReplay(existing, q, signature)
			return err
		}
		var clock struct {
			Now time.Time `db:"now_utc"`
		}
		if err := session.QueryRowCtx(ctx, &clock, "SELECT UTC_TIMESTAMP(3) AS now_utc"); err != nil {
			return err
		}
		now := clock.Now.UTC()
		if now.UnixMilli() < record.ConfirmedAt.UnixMilli() || now.UnixMilli() >= q.ExpiresAt {
			return apperror.New(apperror.Conflict, errors.New("confirmed quote outside admission window"))
		}
		if u.PaymentRestrictedUntil.Valid && now.Before(u.PaymentRestrictedUntil.Time) {
			return apperror.New(apperror.PaymentRestricted, nil)
		}
		if b := q.Beneficiary; b != nil {
			target, err := usermodel.NewUserModel(conn).FindOne(ctx, b.UserID)
			if errors.Is(err, usermodel.ErrNotFound) || err == nil && target.Status != usermodel.StatusActive {
				return apperror.New(apperror.TransferRecipientUnavailable, nil)
			}
			if err != nil {
				return err
			}
		}
		if signature != "" {
			if existing == nil {
				return apperror.New(apperror.NotFound, nil)
			}
			existing.UserSignature, existing.AcceptedAt = signature, sql.NullTime{Time: now, Valid: true}
			if err := rows.Update(ctx, existing); err != nil {
				return err
			}
		} else {
			existing = &model.SolDepositOrder{AssetId: q.AssetID, ExecutionState: SolWaiting, QuoteIdx: q.QuoteIdx, OrderIdx: q.SolOrderIdx, MessageBase64: q.SolMessage, MessageSha256: q.MessageSHA256, LastValidHeight: q.SolLastValidHeight, ReviewAfter: time.UnixMilli(q.SolReviewAt).UTC()}
			if _, err := rows.Insert(ctx, existing); err != nil {
				return err
			}
		}
		result = SolOrder{existing, q.ExpiresAt, q.Beneficiary.PublicIdx()}
		return nil
	})
	return result, err
}

func solReplay(row *model.SolDepositOrder, q chainassets.DepositPreview, signature string) (SolOrder, error) {
	if signature != "" && row.UserSignature != signature {
		return SolOrder{}, apperror.New(apperror.Conflict, errors.New("order already accepted another signature"))
	}
	return SolOrder{row, q.ExpiresAt, q.Beneficiary.PublicIdx()}, nil
}

func (s *SolOrders) BeneficiaryPayments(ctx context.Context, payer int64, before string) ([]model.BeneficiaryPayment, string, error) {
	if before != "" && !idxSyntax.MatchString(before) {
		return nil, "", apperror.New(apperror.InvalidArgument, nil)
	}
	const pageSize = 20
	rows, err := s.rows.ListBeneficiaryPayments(ctx, payer, before, pageSize+1)
	if err != nil {
		return nil, "", err
	}
	if len(rows) > pageSize {
		return rows[:pageSize], rows[pageSize-1].QuoteIdx, nil
	}
	return rows, "", nil
}
