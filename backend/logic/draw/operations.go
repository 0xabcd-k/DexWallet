package draw

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/0xabcd-k/DexWallet/backend/apperror"
	model "github.com/0xabcd-k/DexWallet/backend/model/draw"
	"github.com/0xabcd-k/DexWallet/backend/transaction"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
	"math"
)

func (s *Service) Writable() bool { return s.enabled }

type Management struct {
	Items   []*Version
	Control *model.DrawControl
	Next    int64
}

func (s *Service) Versions(ctx context.Context, before, limit int64) (*Management, error) {
	if before < 0 || limit < 1 || limit > MaxPageSize {
		return nil, apperror.New(apperror.InvalidArgument, nil)
	}
	rows := []*model.DrawRuleVersion{}
	err := s.conn.QueryRowsCtx(ctx, &rows, "SELECT * FROM draw_rule_version WHERE (?=0 OR id<?) ORDER BY id DESC LIMIT ?", before, before, limit+1)
	if err != nil {
		return nil, err
	}
	out := &Management{Items: []*Version{}}
	if int64(len(rows)) > limit {
		rows = rows[:limit]
		out.Next = rows[len(rows)-1].Id
	}
	for _, row := range rows {
		rules, err := ParseRules(row.RulesJson)
		if err != nil {
			return nil, err
		}
		out.Items = append(out.Items, &Version{Row: row, Rules: rules})
	}
	out.Control, err = model.Current(ctx, s.conn, false)
	return out, err
}
func (s *Service) SaveVersionInSession(ctx context.Context, session sqlx.Session, name string, r Rules) (*Version, bool, error) {
	if err := transaction.RequireSession(ctx, session); err != nil {
		return nil, false, err
	}
	if !s.enabled {
		return nil, false, apperror.New(apperror.DrawUnavailable, nil)
	}
	if !versionNamePattern.MatchString(name) {
		return nil, false, apperror.New(apperror.InvalidArgument, nil)
	}
	if err := r.Validate(); err != nil {
		return nil, false, apperror.New(apperror.InvalidArgument, err)
	}
	conn := sqlx.NewSqlConnFromSession(session)
	var c model.DrawControl
	if err := conn.QueryRowCtx(ctx, &c, "SELECT * FROM draw_control WHERE id=? FOR UPDATE", model.ControlID); err != nil {
		return nil, false, err
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return nil, false, err
	}
	m := model.NewDrawRuleVersionModel(conn)
	row, err := m.FindOneByName(ctx, name)
	if err == nil {
		if row.RulesJson != string(raw) {
			return nil, false, apperror.New(apperror.Conflict, nil)
		}
		return &Version{Row: row, Rules: r}, false, nil
	}
	if !errors.Is(err, model.ErrNotFound) {
		return nil, false, err
	}
	row = &model.DrawRuleVersion{Name: name, RulesJson: string(raw)}
	result, err := m.Insert(ctx, row)
	if err != nil {
		return nil, false, err
	}
	row.Id, err = result.LastInsertId()
	return &Version{Row: row, Rules: r}, true, err
}
func (s *Service) SetControlInSession(ctx context.Context, session sqlx.Session, id, expected int64) (*model.DrawControl, bool, error) {
	if err := transaction.RequireSession(ctx, session); err != nil {
		return nil, false, err
	}
	if id < 0 || expected < 1 {
		return nil, false, apperror.New(apperror.InvalidArgument, nil)
	}
	if id > 0 && !s.enabled {
		return nil, false, apperror.New(apperror.DrawUnavailable, nil)
	}
	conn := sqlx.NewSqlConnFromSession(session)
	var c model.DrawControl
	if err := conn.QueryRowCtx(ctx, &c, "SELECT * FROM draw_control WHERE id=? FOR UPDATE", model.ControlID); err != nil {
		return nil, false, err
	}
	if c.VersionId.Int64 == id && (c.Revision == expected || c.Revision == expected+1) {
		return &c, false, nil
	}
	if c.Revision != expected || c.Revision == math.MaxInt64 {
		return nil, false, apperror.New(apperror.Conflict, nil)
	}
	if id > 0 {
		if _, err := version(ctx, conn, id); err != nil {
			if errors.Is(err, model.ErrNotFound) {
				err = apperror.New(apperror.NotFound, nil)
			}
			return nil, false, err
		}
	}
	c.VersionId.Int64 = id
	c.VersionId.Valid = id > 0
	c.Revision++
	_, err := conn.ExecCtx(ctx, "UPDATE draw_control SET version_id=?,revision=? WHERE id=?", c.VersionId, c.Revision, model.ControlID)
	return &c, true, err
}
