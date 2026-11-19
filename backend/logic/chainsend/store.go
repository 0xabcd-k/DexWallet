package chainsend

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"

	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
	model "github.com/0xabcd-k/DexWallet/backend/model/chainsend"
	"github.com/0xabcd-k/DexWallet/backend/transaction"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

type Transactor interface {
	TransactCtx(context.Context, func(context.Context, sqlx.Session) error) error
}

type Store struct {
	conn sqlx.SqlConn
	tx   Transactor
}

func NewStore(conn sqlx.SqlConn, tx Transactor) *Store { return &Store{conn, tx} }

func (s *Store) Find(ctx context.Context, key string) (View, error) {
	r, err := s.load(ctx, key)
	if err != nil {
		return View{}, err
	}
	return view(r), nil
}

func (s *Store) load(ctx context.Context, key string) (*model.ChainSendIntent, error) {
	if !requestKey.MatchString(key) {
		return nil, ErrInvalidRequest
	}
	return model.NewChainSendIntentModel(s.conn).FindOneByNetworkRequestKey(ctx, chainassets.TONTestnet, key)
}

func sameIntent(a, b *model.ChainSendIntent) bool {
	return a.Network == b.Network && a.RequestKey == b.RequestKey && a.Sender == b.Sender &&
		a.Recipient == b.Recipient && a.AssetId == b.AssetId && a.AmountAtoms == b.AmountAtoms &&
		a.PublicKey == b.PublicKey && a.SubwalletId == b.SubwalletId && a.Seqno == b.Seqno &&
		a.ValidUntil == b.ValidUntil && a.PayloadHash == b.PayloadHash
}

func unknownCommit(err error) bool {
	var failure *transaction.Failure
	return errors.As(err, &failure) && failure.Unknown()
}

func (s *Store) prepare(ctx context.Context, candidate *model.ChainSendIntent) (*model.ChainSendIntent, error) {
	var saved *model.ChainSendIntent
	err := s.tx.TransactCtx(ctx, func(ctx context.Context, session sqlx.Session) error {
		conn := sqlx.NewSqlConnFromSession(session)
		if err := model.Reserve(ctx, conn, candidate); err != nil {
			return err
		}
		var err error
		saved, err = model.LockKey(ctx, conn, candidate.Network, candidate.RequestKey)
		if errors.Is(err, model.ErrNotFound) {
			now, clockErr := model.Now(ctx, conn)
			if clockErr != nil {
				return clockErr
			}
			if now >= candidate.ValidUntil {
				return ErrExpired
			}
			return ErrSeqnoReserved
		}
		if err != nil {
			return err
		}
		if !sameIntent(saved, candidate) {
			return ErrConflict
		}
		return nil
	})
	if err == nil {
		return saved, nil
	}
	if unknownCommit(err) {
		found, lookupErr := s.load(ctx, candidate.RequestKey)
		if lookupErr == nil && sameIntent(found, candidate) {
			return found, nil
		}
		if lookupErr == nil {
			lookupErr = ErrConflict
		}
		return nil, fmt.Errorf("%w: %w", ErrOutcomeUnknown, errors.Join(err, lookupErr))
	}
	return nil, err
}

func (s *Store) saveSigned(ctx context.Context, original *model.ChainSendIntent, raw []byte, messageHash, bodyHash string) (*model.ChainSendIntent, error) {
	encoded := base64.StdEncoding.EncodeToString(raw)
	matches := func(r *model.ChainSendIntent) bool {
		return sameIntent(r, original) && r.SignedBoc.Valid && r.SignedBoc.String == encoded && r.MessageHash == messageHash && r.BodyHash == bodyHash
	}
	var saved *model.ChainSendIntent
	err := s.tx.TransactCtx(ctx, func(ctx context.Context, session sqlx.Session) error {
		conn := sqlx.NewSqlConnFromSession(session)
		var err error
		saved, err = model.LockID(ctx, conn, original.Id)
		if err != nil {
			return err
		}
		if !sameIntent(saved, original) {
			return ErrConflict
		}
		if saved.State != Prepared {
			if !matches(saved) {
				return ErrConflict
			}
			return nil
		}
		saved.SignedBoc = sql.NullString{String: encoded, Valid: true}
		saved.MessageHash, saved.BodyHash, saved.State = messageHash, bodyHash, Signed
		return model.UpdateExecution(ctx, conn, saved)
	})
	if err == nil {
		return saved, nil
	}
	if unknownCommit(err) {
		found, lookupErr := s.load(ctx, original.RequestKey)
		if lookupErr == nil && matches(found) {
			return found, nil
		}
		if lookupErr == nil {
			lookupErr = ErrConflict
		}
		return nil, fmt.Errorf("%w: %w", ErrOutcomeUnknown, errors.Join(err, lookupErr))
	}
	return nil, err
}

func (s *Store) beginBroadcast(ctx context.Context, original *model.ChainSendIntent) (*model.ChainSendIntent, bool, error) {
	var saved *model.ChainSendIntent
	send := false
	err := s.tx.TransactCtx(ctx, func(ctx context.Context, session sqlx.Session) error {
		conn := sqlx.NewSqlConnFromSession(session)
		var err error
		saved, err = model.LockID(ctx, conn, original.Id)
		if err != nil {
			return err
		}
		if !sameIntent(saved, original) || saved.SignedBoc != original.SignedBoc || saved.MessageHash != original.MessageHash || saved.BodyHash != original.BodyHash {
			return ErrConflict
		}
		if saved.State == Submitted {
			return nil
		}
		if saved.State != Signed && saved.State != BroadcastUnknown {
			return ErrConflict
		}
		now, err := model.Now(ctx, conn)
		if err != nil {
			return err
		}
		if now >= saved.ValidUntil {
			return ErrExpired
		}
		saved.State, saved.Attempts = BroadcastUnknown, saved.Attempts+1
		if err := model.UpdateExecution(ctx, conn, saved); err != nil {
			return err
		}
		send = true
		return nil
	})
	// No broadcast is allowed after an unacknowledged marker commit. A later
	// explicit execution can read it and submit the same durable artifact.
	if unknownCommit(err) {
		err = fmt.Errorf("%w: %w", ErrOutcomeUnknown, err)
	}
	return saved, send && err == nil, err
}

func (s *Store) broadcastResult(ctx context.Context, id int64, accepted bool, note string) error {
	return s.tx.TransactCtx(ctx, func(ctx context.Context, session sqlx.Session) error {
		conn := sqlx.NewSqlConnFromSession(session)
		r, err := model.LockID(ctx, conn, id)
		if err != nil {
			return err
		}
		if r.State == Submitted {
			return nil
		} // A late failure cannot downgrade acceptance.
		if r.State != BroadcastUnknown {
			return ErrConflict
		}
		if accepted {
			r.State, r.LastError = Submitted, ""
		} else {
			const maxErrorRunes = 4096
			text := []rune(note)
			if len(text) > maxErrorRunes {
				text = text[:maxErrorRunes]
			}
			r.LastError = string(text)
		}
		return model.UpdateExecution(ctx, conn, r)
	})
}
