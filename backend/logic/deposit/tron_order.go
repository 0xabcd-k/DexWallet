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
	"github.com/go-sql-driver/mysql"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

const (
	TronSigned   = "signed"
	TronReview   = "review"
	TronCredited = "credited"
	TronExpired  = "expired"
)

type TronOrders struct {
	conn   sqlx.SqlConn
	quotes *Confirmations
	users  *user.Service
	tx     *transaction.Runner
	chain  *chainassets.Service
}

func NewTronOrders(conn sqlx.SqlConn, quotes *Confirmations, users *user.Service, tx *transaction.Runner, chain *chainassets.Service) *TronOrders {
	return &TronOrders{conn, quotes, users, tx, chain}
}
func (s *TronOrders) Get(ctx context.Context, owner int64, idx string) (*model.TronDepositOrder, error) {
	record, err := s.quotes.Get(ctx, owner, idx)
	if err != nil {
		return nil, err
	}
	if _, err := record.Quote.NileDepositTransfer(); err != nil {
		return nil, apperror.New(apperror.Unprocessable, err)
	}
	o, err := model.NewTronDepositOrderModel(s.conn).FindOneByQuoteIdx(ctx, idx)
	if errors.Is(err, sql.ErrNoRows) {
		state := AwaitingSignature
		if time.Now().UnixMilli() >= record.Quote.ExpiresAt {
			state = TronExpired
		}
		return &model.TronDepositOrder{QuoteIdx: idx, State: state}, nil
	}
	return o, err
}

func (s *TronOrders) Authorize(ctx context.Context, actor user.Actor, idx, signature, orderSig string) (*model.TronDepositOrder, error) {
	if len(signature) != 130 || len(orderSig) != 130 {
		return nil, apperror.New(apperror.InvalidArgument, nil)
	}
	signature = strings.ToLower(signature)
	orderSig = strings.ToLower(orderSig)
	record, err := s.quotes.Get(ctx, actor.UserID, idx)
	if err != nil {
		return nil, err
	}
	q := record.Quote
	_, err = q.NileDepositTransfer()
	if err != nil {
		return nil, apperror.New(apperror.Unprocessable, err)
	}
	_, err = q.BindNileDeposit(signature, orderSig)
	if err != nil {
		return nil, apperror.New(apperror.InvalidArgument, err)
	}
	rows := model.NewTronDepositOrderModel(s.conn)
	replay := func(o *model.TronDepositOrder) (*model.TronDepositOrder, error) {
		if o.UserSignature != signature || o.OrderSignature != orderSig {
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
	if err := s.chain.AdmitNileDeposit(ctx, actor.UserID, q); err != nil {
		return nil, err
	}
	err = s.tx.TransactCtx(ctx, func(ctx context.Context, session sqlx.Session) error {
		u, err := s.users.CheckActorInSession(ctx, session, actor)
		if err != nil {
			return err
		}
		lockedRows := model.NewTronDepositOrderModel(sqlx.NewSqlConnFromSession(session))
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
		_, err = lockedRows.Insert(ctx, &model.TronDepositOrder{QuoteIdx: idx, TxId: q.MessageSHA256, UserSignature: signature, OrderSignature: orderSig, State: TronSigned, AcceptedAt: now})
		var duplicate *mysql.MySQLError
		if errors.As(err, &duplicate) && duplicate.Number == 1062 {
			return apperror.New(apperror.Conflict, errors.New("Nile transaction already belongs to a deposit"))
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	return rows.FindOneByQuoteIdx(ctx, idx)
}
