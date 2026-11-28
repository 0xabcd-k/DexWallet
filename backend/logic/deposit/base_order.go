package deposit

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/apperror"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
	"github.com/0xabcd-k/DexWallet/backend/logic/user"
	model "github.com/0xabcd-k/DexWallet/backend/model/deposit"
	"github.com/0xabcd-k/DexWallet/backend/transaction"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

const (
	BaseQuoteExpired = "expired"
	BaseSigned       = "signed"
	BaseReview       = "review"
	BaseCredited     = "credited"
)

type BaseOrders struct {
	conn   sqlx.SqlConn
	quotes *Confirmations
	users  *user.Service
	tx     *transaction.Runner
	chain  *chainassets.Service
}

func NewBaseOrders(conn sqlx.SqlConn, quotes *Confirmations, users *user.Service, tx *transaction.Runner, chain *chainassets.Service) *BaseOrders {
	return &BaseOrders{conn, quotes, users, tx, chain}
}
func (s *BaseOrders) Get(ctx context.Context, owner int64, idx string) (*model.BaseDepositOrder, error) {
	record, err := s.quotes.Get(ctx, owner, idx)
	if err != nil {
		return nil, err
	}
	if _, err := record.Quote.BaseAuthorization(); err != nil {
		return nil, apperror.New(apperror.Unprocessable, err)
	}
	o, err := model.NewBaseDepositOrderModel(s.conn).FindOneByQuoteIdx(ctx, idx)
	if errors.Is(err, sql.ErrNoRows) {
		state := AwaitingSignature
		if time.Now().UnixMilli() >= record.Quote.ExpiresAt {
			state = BaseQuoteExpired
		}
		return &model.BaseDepositOrder{QuoteIdx: idx, State: state}, nil
	}
	return o, err
}

func (s *BaseOrders) Authorize(ctx context.Context, actor user.Actor, idx, signature string) (*model.BaseDepositOrder, error) {
	if len(signature) != 132 {
		return nil, apperror.New(apperror.InvalidArgument, nil)
	}
	signature = strings.ToLower(signature)
	record, err := s.quotes.Get(ctx, actor.UserID, idx)
	if err != nil {
		return nil, err
	}
	q := record.Quote
	a, err := q.BaseAuthorization()
	if err != nil {
		return nil, apperror.New(apperror.Unprocessable, err)
	}
	data, err := a.Data(signature)
	if err != nil {
		return nil, apperror.New(apperror.InvalidArgument, err)
	}
	rows := model.NewBaseDepositOrderModel(s.conn)
	replay := func(o *model.BaseDepositOrder) (*model.BaseDepositOrder, error) {
		if o.UserSignature != signature {
			return nil, apperror.New(apperror.Conflict, errors.New("order already authorized"))
		}
		return o, nil
	}
	if o, err := rows.FindOneByQuoteIdx(ctx, idx); err == nil {
		return replay(o)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if time.Now().UnixMilli() >= q.ExpiresAt {
		return nil, apperror.New(apperror.Conflict, errors.New("quote expired"))
	}
	if err := s.chain.AdmitBaseUSDC(ctx, actor.UserID, q, data); err != nil {
		return nil, err
	}
	err = s.tx.TransactCtx(ctx, func(ctx context.Context, session sqlx.Session) error {
		u, err := s.users.CheckActorInSession(ctx, session, actor)
		if err != nil {
			return err
		}
		lockedRows := model.NewBaseDepositOrderModel(sqlx.NewSqlConnFromSession(session))
		if o, err := lockedRows.FindOneByQuoteIdx(ctx, idx); err == nil {
			_, err = replay(o)
			return err
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		var clock struct {
			Now time.Time `db:"now_utc"`
		}
		if err := session.QueryRowCtx(ctx, &clock, "SELECT UTC_TIMESTAMP(3) AS now_utc"); err != nil {
			return err
		}
		now := clock.Now.UTC()
		if now.Before(record.ConfirmedAt) || now.UnixMilli() >= q.ExpiresAt {
			return apperror.New(apperror.Conflict, errors.New("original quote expired"))
		}
		if u.PaymentRestrictedUntil.Valid && now.Before(u.PaymentRestrictedUntil.Time) {
			return apperror.New(apperror.PaymentRestricted, nil)
		}
		_, err = lockedRows.Insert(ctx, &model.BaseDepositOrder{QuoteIdx: idx, UserSignature: signature, State: Accepted, Sponsor: strings.ToLower(q.FeePayer), AcceptedAt: now})
		return err
	})
	if err != nil {
		return nil, err
	}
	return rows.FindOneByQuoteIdx(ctx, idx)
}
