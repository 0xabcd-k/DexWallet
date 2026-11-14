package draw

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"strconv"
	"strings"

	"github.com/0xabcd-k/DexWallet/backend/apperror"
	"github.com/0xabcd-k/DexWallet/backend/logic/account"
	users "github.com/0xabcd-k/DexWallet/backend/logic/user"
	accounts "github.com/0xabcd-k/DexWallet/backend/model/account"
	model "github.com/0xabcd-k/DexWallet/backend/model/draw"
	usermodel "github.com/0xabcd-k/DexWallet/backend/model/user"
	"github.com/0xabcd-k/DexWallet/backend/transaction"
	"github.com/go-sql-driver/mysql"
	"github.com/shopspring/decimal"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

const maxAttempts = 2
const mysqlDeadlock = 1213
const mysqlLockTimeout = 1205
const MaxPageSize = 100

var requestPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{16,64}$`)
var versionNamePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)
var ErrOutcomeUnknown = errors.New("draw outcome unknown; recover original request")

type Transactor interface {
	TransactCtx(context.Context, func(context.Context, sqlx.Session) error) error
}
type Service struct {
	conn     sqlx.SqlConn
	tx       Transactor
	accounts *account.Service
	enabled  bool
}

func New(conn sqlx.SqlConn, tx Transactor, a *account.Service, enabled bool) *Service {
	return &Service{conn, tx, a, enabled}
}

type Request struct {
	Key       string
	VersionID int64
}
type Version struct {
	Row   *model.DrawRuleVersion
	Rules Rules
}
type State struct {
	Version           *Version
	Points, RewardUSD string
}
type Result struct {
	Row         *model.PointDraw
	VersionName string
	Symbols     [3]int64
}

func authorize(u *usermodel.User, a users.Actor) error {
	if u.Status != usermodel.StatusActive {
		return users.ErrDisabled
	}
	if u.AuthVersion != a.AuthVersion {
		return users.ErrStaleSession
	}
	return nil
}
func (s *Service) authorized(ctx context.Context, a users.Actor) error {
	u, err := usermodel.NewUserModel(s.conn).FindOne(ctx, a.UserID)
	if err != nil {
		return err
	}
	return authorize(u, a)
}
func version(ctx context.Context, conn sqlx.SqlConn, id int64) (*Version, error) {
	row, err := model.NewDrawRuleVersionModel(conn).FindOne(ctx, id)
	if err != nil {
		return nil, err
	}
	r, err := ParseRules(row.RulesJson)
	if err != nil {
		return nil, fmt.Errorf("draw version %d: %w", id, err)
	}
	return &Version{row, r}, nil
}
func (s *Service) State(ctx context.Context, a users.Actor) (*State, error) {
	if err := s.authorized(ctx, a); err != nil {
		return nil, err
	}
	state := &State{Points: "0", RewardUSD: "0.00"}
	balances, err := s.accounts.List(ctx, a.UserID)
	if err != nil {
		return nil, err
	}
	for _, b := range balances {
		switch b.Kind {
		case accounts.KindTaskPoints:
			state.Points = b.Balance
		case accounts.KindRewardUSD:
			state.RewardUSD = b.Balance
		}
	}
	if !s.enabled {
		return state, nil
	}
	control, err := model.Current(ctx, s.conn, false)
	if err != nil {
		return nil, err
	}
	if control.VersionId.Valid {
		state.Version, err = version(ctx, s.conn, control.VersionId.Int64)
	}
	return state, err
}

// Publish appends an immutable version and atomically changes the current pointer.
// This iteration exposes it only through the isolated development tool, not HTTP.
func (s *Service) Publish(ctx context.Context, name string, r Rules) (*model.DrawRuleVersion, error) {
	if !s.enabled {
		return nil, apperror.New(apperror.DrawUnavailable, nil)
	}
	if !versionNamePattern.MatchString(name) {
		return nil, apperror.New(apperror.InvalidArgument, nil)
	}
	if err := r.Validate(); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	var row *model.DrawRuleVersion
	err = s.tx.TransactCtx(ctx, func(ctx context.Context, session sqlx.Session) error {
		conn := sqlx.NewSqlConnFromSession(session)
		var control model.DrawControl
		if err := conn.QueryRowCtx(ctx, &control, "SELECT id,version_id FROM draw_control WHERE id=? FOR UPDATE", model.ControlID); err != nil {
			return err
		}
		m := model.NewDrawRuleVersionModel(conn)
		existing, err := m.FindOneByName(ctx, name)
		if err == nil {
			if existing.RulesJson != string(raw) {
				return apperror.New(apperror.Conflict, nil)
			}
			row = existing
		} else if errors.Is(err, model.ErrNotFound) {
			inserted, err := m.Insert(ctx, &model.DrawRuleVersion{Name: name, RulesJson: string(raw)})
			if err != nil {
				return err
			}
			id, err := inserted.LastInsertId()
			if err != nil {
				return err
			}
			row, err = m.FindOne(ctx, id)
			if err != nil {
				return err
			}
		} else {
			return err
		}
		_, err = conn.ExecCtx(ctx, "UPDATE draw_control SET version_id=? WHERE id=?", row.Id, model.ControlID)
		return err
	})
	return row, err
}

func match(row *model.PointDraw, r Request) error {
	if row.VersionId != r.VersionID {
		return apperror.New(apperror.DrawRequestConflict, nil)
	}
	return nil
}
func result(ctx context.Context, conn sqlx.SqlConn, row *model.PointDraw) (*Result, error) {
	if !row.VoucherId.Valid {
		return nil, fmt.Errorf("%w: draw %d missing voucher", ErrOutcomeUnknown, row.Id)
	}
	v, err := model.NewDrawRuleVersionModel(conn).FindOne(ctx, row.VersionId)
	if err != nil {
		return nil, err
	}
	parts := strings.Split(row.Symbols, ",")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid stored draw symbols %d", row.Id)
	}
	r := &Result{Row: row, VersionName: v.Name}
	for i, p := range parts {
		r.Symbols[i], err = strconv.ParseInt(p, 10, 64)
		if err != nil {
			return nil, err
		}
	}
	if code, err := SymbolPrize(r.Symbols); err != nil || code != row.PrizeCode {
		return nil, fmt.Errorf("invalid stored draw result %d", row.Id)
	}
	return r, nil
}
func (s *Service) ByRequest(ctx context.Context, a users.Actor, key string) (*Result, error) {
	if !requestPattern.MatchString(key) {
		return nil, apperror.New(apperror.InvalidArgument, nil)
	}
	if err := s.authorized(ctx, a); err != nil {
		return nil, err
	}
	row, err := model.NewPointDrawModel(s.conn).FindOneByUserIdRequestKey(ctx, a.UserID, key)
	if errors.Is(err, model.ErrNotFound) {
		return nil, apperror.New(apperror.NotFound, nil)
	}
	if err != nil {
		return nil, err
	}
	return result(ctx, s.conn, row)
}
func (s *Service) Detail(ctx context.Context, a users.Actor, id int64) (*Result, error) {
	if err := s.authorized(ctx, a); err != nil {
		return nil, err
	}
	row, err := model.NewPointDrawModel(s.conn).FindOne(ctx, id)
	if errors.Is(err, model.ErrNotFound) || (err == nil && row.UserId != a.UserID) {
		return nil, apperror.New(apperror.NotFound, nil)
	}
	if err != nil {
		return nil, err
	}
	return result(ctx, s.conn, row)
}
func (s *Service) List(ctx context.Context, a users.Actor, before, limit int64) ([]*Result, int64, error) {
	if before < 0 || limit < 1 || limit > MaxPageSize {
		return nil, 0, apperror.New(apperror.InvalidArgument, nil)
	}
	if err := s.authorized(ctx, a); err != nil {
		return nil, 0, err
	}
	rows, err := model.Page(ctx, s.conn, a.UserID, before, limit+1)
	if err != nil {
		return nil, 0, err
	}
	next := int64(0)
	if int64(len(rows)) > limit {
		rows = rows[:limit]
		next = rows[len(rows)-1].Id
	}
	results := []*Result{}
	for _, row := range rows {
		r, err := result(ctx, s.conn, row)
		if err != nil {
			return nil, 0, err
		}
		results = append(results, r)
	}
	return results, next, nil
}

func (s *Service) Draw(ctx context.Context, a users.Actor, r Request) (*Result, error) {
	if r.VersionID <= 0 || !requestPattern.MatchString(r.Key) {
		return nil, apperror.New(apperror.InvalidArgument, nil)
	}
	// Committed requests remain recoverable after a version is paused or replaced.
	original, err := s.ByRequest(ctx, a, r.Key)
	if err == nil {
		return original, match(original.Row, r)
	}
	var app *apperror.Error
	if !errors.As(err, &app) || app.Code != apperror.NotFound {
		return nil, err
	}
	if !s.enabled {
		return nil, apperror.New(apperror.DrawUnavailable, nil)
	}
	var ticket *int64 // A confirmed rollback retry keeps its already selected ticket.
	for attempt := 0; attempt < maxAttempts; attempt++ {
		var settled *Result
		err = s.tx.TransactCtx(ctx, func(ctx context.Context, session sqlx.Session) error {
			var err error
			settled, err = s.settle(ctx, session, a, r, &ticket)
			return err
		})
		if err == nil {
			return settled, nil
		}
		var failure *transaction.Failure
		if errors.As(err, &failure) {
			if failure.Unknown() {
				original, lookupErr := s.ByRequest(ctx, a, r.Key)
				if lookupErr == nil {
					if err := match(original.Row, r); err != nil {
						return nil, err
					}
					logx.WithContext(ctx).Errorf("draw %d recovered after unknown commit: %v", original.Row.Id, err)
					return original, nil
				}
				return nil, fmt.Errorf("%w: %w", ErrOutcomeUnknown, errors.Join(err, lookupErr))
			}
			return nil, err
		}
		var dbErr *mysql.MySQLError
		if errors.As(err, &dbErr) {
			if dbErr.Number == mysqlDeadlock && attempt+1 < maxAttempts && ctx.Err() == nil {
				logx.WithContext(ctx).Errorf("draw user %d retrying rolled-back deadlock: %v", a.UserID, err)
				continue
			}
			if dbErr.Number == mysqlDeadlock || dbErr.Number == mysqlLockTimeout {
				return nil, apperror.New(apperror.DrawBusy, err)
			}
		}
		return nil, err
	}
	panic("unreachable draw attempt")
}

func (s *Service) settle(ctx context.Context, session sqlx.Session, a users.Actor, r Request, ticket **int64) (*Result, error) {
	if err := transaction.RequireSession(ctx, session); err != nil {
		return nil, err
	}
	conn := sqlx.NewSqlConnFromSession(session)
	u, err := usermodel.NewUserModel(conn).FindForUpdate(ctx, a.UserID)
	if err != nil {
		return nil, err
	}
	if err = authorize(u, a); err != nil {
		return nil, err
	}
	draws := model.NewPointDrawModel(conn)
	// First snapshot read follows the user lock, so competing commits are visible.
	row, err := draws.FindOneByUserIdRequestKey(ctx, a.UserID, r.Key)
	if err == nil {
		if err = match(row, r); err != nil {
			return nil, err
		}
		return result(ctx, conn, row)
	}
	if !errors.Is(err, model.ErrNotFound) {
		return nil, err
	}
	control, err := model.Current(ctx, conn, true)
	if err != nil {
		return nil, err
	}
	if !control.VersionId.Valid {
		return nil, apperror.New(apperror.DrawUnavailable, nil)
	}
	if control.VersionId.Int64 != r.VersionID {
		return nil, apperror.New(apperror.DrawVersionChanged, nil)
	}
	v, err := version(ctx, conn, r.VersionID)
	if err != nil {
		return nil, err
	}
	// The account writer rechecks balances under its canonical account-lock order.
	if *ticket == nil {
		n, err := rand.Int(rand.Reader, big.NewInt(TotalWeight))
		if err != nil {
			return nil, err
		}
		value := n.Int64()
		*ticket = &value
	}
	prize, err := v.Rules.Pick(**ticket)
	if err != nil {
		return nil, err
	}
	row = &model.PointDraw{UserId: a.UserID, RequestKey: r.Key, VersionId: r.VersionID, CostPoints: CostPoints, PrizeCode: prize.Code, RewardUsd: prize.RewardUSD, Symbols: fmt.Sprintf("%d,%d,%d", prize.Symbols[0], prize.Symbols[1], prize.Symbols[2]), Ticket: **ticket}
	inserted, err := draws.Insert(ctx, row)
	if err != nil {
		return nil, err
	}
	row.Id, err = inserted.LastInsertId()
	if err != nil {
		return nil, err
	}
	amount := decimal.NewFromInt(CostPoints)
	voucher, err := s.accounts.Post(ctx, session, account.Posting{Source: account.Source{Type: account.BusinessDraw, ID: row.Id, Action: account.ActionPost}, Entries: []account.Entry{
		account.Debit(a.UserID, accounts.KindTaskPoints, amount),
		{System: account.SystemPointConsumption, Side: account.SideCredit, Amount: amount},
		{System: account.SystemRewardExpense, Side: account.SideDebit, Amount: prize.RewardUSD},
		account.Credit(a.UserID, accounts.KindRewardUSD, prize.RewardUSD),
	}})
	if err != nil {
		return nil, err
	}
	row.VoucherId = sql.NullInt64{Int64: voucher, Valid: true}
	if err = draws.Update(ctx, row); err != nil {
		return nil, err
	}
	row, err = draws.FindOne(ctx, row.Id)
	if err != nil {
		return nil, err
	}
	return result(ctx, conn, row)
}
