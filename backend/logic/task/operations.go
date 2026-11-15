package task

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/0xabcd-k/DexWallet/backend/apperror"
	accounts "github.com/0xabcd-k/DexWallet/backend/model/account"
	model "github.com/0xabcd-k/DexWallet/backend/model/task"
	"github.com/0xabcd-k/DexWallet/backend/transaction"
	"github.com/shopspring/decimal"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
	"math"
	"strings"
	"time"
)

type Draft struct {
	ID, ExpectedRevision, CreatorID int64
	RequestKey, Title, Description  string
	BasePoints, ChannelID           int64
	StartsAt, EndsAt                time.Time
}

func (s *Service) Writable() bool { return s.membership != nil }
func validPage(before, limit int64) error {
	if before < 0 || limit < 1 || limit > MaxPageSize {
		return apperror.New(apperror.InvalidArgument, nil)
	}
	return nil
}
func (s *Service) Managed(ctx context.Context, before, limit int64) ([]*model.TaskDefinition, int64, error) {
	if err := validPage(before, limit); err != nil {
		return nil, 0, err
	}
	rows := []*model.TaskDefinition{}
	err := s.conn.QueryRowsCtx(ctx, &rows, "SELECT * FROM task_definition WHERE (?=0 OR id<?) ORDER BY id DESC LIMIT ?", before, before, limit+1)
	var next int64
	if int64(len(rows)) > limit {
		rows = rows[:limit]
		next = rows[len(rows)-1].Id
	}
	return rows, next, err
}
func (s *Service) ManagedCompletions(ctx context.Context, id, before, limit int64) ([]*model.TaskCompletion, int64, error) {
	if err := validPage(before, limit); err != nil {
		return nil, 0, err
	}
	if _, err := s.definitions.FindOne(ctx, id); errors.Is(err, model.ErrNotFound) {
		return nil, 0, apperror.New(apperror.NotFound, nil)
	} else if err != nil {
		return nil, 0, err
	}
	rows := []*model.TaskCompletion{}
	err := s.conn.QueryRowsCtx(ctx, &rows, "SELECT * FROM task_completion WHERE task_id=? AND (?=0 OR id<?) ORDER BY id DESC LIMIT ?", id, before, before, limit+1)
	var next int64
	if int64(len(rows)) > limit {
		rows = rows[:limit]
		next = rows[len(rows)-1].Id
	}
	return rows, next, err
}
func (s *Service) SaveDraftInSession(ctx context.Context, session sqlx.Session, d Draft) (*model.TaskDefinition, bool, error) {
	if err := transaction.RequireSession(ctx, session); err != nil {
		return nil, false, err
	}
	if !s.Writable() {
		return nil, false, apperror.New(apperror.TaskUnavailable, nil)
	}
	d.Title = strings.TrimSpace(d.Title)
	d.Description = strings.TrimSpace(d.Description)
	d.StartsAt = d.StartsAt.UTC().Truncate(time.Microsecond)
	d.EndsAt = d.EndsAt.UTC().Truncate(time.Microsecond)
	if d.ID < 0 || d.CreatorID <= 0 || !claimKeyPattern.MatchString(d.RequestKey) || d.ExpectedRevision < 0 || d.Title == "" || len([]rune(d.Title)) > 128 || len([]rune(d.Description)) > 2048 || d.BasePoints < 1 || d.ChannelID >= 0 || d.StartsAt.Year() < 1000 || d.EndsAt.Year() > 9999 || !d.EndsAt.After(d.StartsAt) {
		return nil, false, apperror.New(apperror.InvalidArgument, nil)
	}
	// Validate the largest single task award using the existing account precision.
	definition, _ := accounts.Lookup(accounts.KindTaskPoints)
	if err := definition.ValidateBalances(decimal.NewFromInt(d.BasePoints).Mul(decimal.NewFromInt(ProMultiplier)), decimal.Zero); err != nil {
		return nil, false, apperror.New(apperror.InvalidArgument, err)
	}
	conn := sqlx.NewSqlConnFromSession(session)
	m := model.NewTaskDefinitionModel(conn)
	if d.ID == 0 {
		if d.ExpectedRevision != 0 {
			return nil, false, apperror.New(apperror.InvalidArgument, nil)
		}
		raw, _ := json.Marshal(d)
		hash := fmt.Sprintf("%x", sha256.Sum256(raw))
		key := fmt.Sprintf("%d:%s", d.CreatorID, d.RequestKey)
		row, err := m.FindOneByCreateKey(ctx, sql.NullString{String: key, Valid: true})
		if err == nil {
			if row.CreateHash.String != hash {
				return nil, false, apperror.New(apperror.Conflict, nil)
			}
			return row, false, nil
		}
		if !errors.Is(err, model.ErrNotFound) {
			return nil, false, err
		}
		row = &model.TaskDefinition{Kind: KindTelegramChannelFollow, Status: StatusDraft, VerificationMode: ModeMock, Revision: 1, CreateKey: sql.NullString{String: key, Valid: true}, CreateHash: sql.NullString{String: hash, Valid: true}}
		applyDraft(row, d)
		r, err := m.Insert(ctx, row)
		if err != nil {
			return nil, false, err
		}
		row.Id, err = r.LastInsertId()
		return row, true, err
	}
	var row model.TaskDefinition
	if err := conn.QueryRowCtx(ctx, &row, "SELECT * FROM task_definition WHERE id=? FOR UPDATE", d.ID); err != nil {
		if errors.Is(err, sqlx.ErrNotFound) {
			err = apperror.New(apperror.NotFound, nil)
		}
		return nil, false, err
	}
	if row.Status != StatusDraft || row.VerificationMode != ModeMock || row.Kind != KindTelegramChannelFollow {
		return nil, false, apperror.New(apperror.Conflict, nil)
	}
	same := row.Title == d.Title && row.Description == d.Description && row.BasePoints == d.BasePoints && row.ChannelId == d.ChannelID && row.StartsAt.Equal(d.StartsAt) && row.EndsAt.Equal(d.EndsAt)
	if same && (row.Revision == d.ExpectedRevision || row.Revision == d.ExpectedRevision+1) {
		return &row, false, nil
	}
	if row.Revision != d.ExpectedRevision || row.Revision == math.MaxInt64 {
		return nil, false, apperror.New(apperror.Conflict, nil)
	}
	applyDraft(&row, d)
	row.Revision++
	err := m.Update(ctx, &row)
	return &row, true, err
}
func applyDraft(row *model.TaskDefinition, d Draft) {
	row.Title = d.Title
	row.Description = d.Description
	row.BasePoints = d.BasePoints
	row.ChannelId = d.ChannelID
	row.StartsAt = d.StartsAt
	row.EndsAt = d.EndsAt
	row.ChannelUrl = ""
}
func (s *Service) SetStatusInSession(ctx context.Context, session sqlx.Session, id, expected int64, status string) (*model.TaskDefinition, bool, error) {
	if err := transaction.RequireSession(ctx, session); err != nil {
		return nil, false, err
	}
	if expected < 1 || (status != StatusActive && status != StatusPaused) {
		return nil, false, apperror.New(apperror.InvalidArgument, nil)
	}
	if status == StatusActive && !s.Writable() {
		return nil, false, apperror.New(apperror.TaskUnavailable, nil)
	}
	conn := sqlx.NewSqlConnFromSession(session)
	var row model.TaskDefinition
	if err := conn.QueryRowCtx(ctx, &row, "SELECT * FROM task_definition WHERE id=? FOR UPDATE", id); err != nil {
		if errors.Is(err, sqlx.ErrNotFound) {
			err = apperror.New(apperror.NotFound, nil)
		}
		return nil, false, err
	}
	if row.VerificationMode != ModeMock || row.Kind != KindTelegramChannelFollow {
		return nil, false, apperror.New(apperror.Conflict, nil)
	}
	if row.Status == status && (row.Revision == expected || row.Revision == expected+1) {
		return &row, false, nil
	}
	if row.Revision != expected || row.Revision == math.MaxInt64 || (row.Status == StatusDraft && status != StatusActive) {
		return nil, false, apperror.New(apperror.Conflict, nil)
	}
	row.Status = status
	row.Revision++
	err := model.NewTaskDefinitionModel(conn).Update(ctx, &row)
	return &row, true, err
}
