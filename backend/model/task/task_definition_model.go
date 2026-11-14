package task

import (
	"context"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

var _ TaskDefinitionModel = (*customTaskDefinitionModel)(nil)

type (
	// TaskDefinitionModel is an interface to be customized, add more methods here,
	// and implement the added methods in customTaskDefinitionModel.
	TaskDefinitionModel interface {
		taskDefinitionModel
		ListVisible(context.Context, string, int64, int64) ([]*TaskDefinition, error)
		WithSession(session sqlx.Session) TaskDefinitionModel
	}

	customTaskDefinitionModel struct {
		*defaultTaskDefinitionModel
	}
)

// NewTaskDefinitionModel returns a model for the database table.
func NewTaskDefinitionModel(conn sqlx.SqlConn) TaskDefinitionModel {
	return &customTaskDefinitionModel{
		defaultTaskDefinitionModel: newTaskDefinitionModel(conn),
	}
}

func (m *customTaskDefinitionModel) WithSession(session sqlx.Session) TaskDefinitionModel {
	return NewTaskDefinitionModel(sqlx.NewSqlConnFromSession(session))
}

func (m *customTaskDefinitionModel) ListVisible(ctx context.Context, mode string, before, limit int64) ([]*TaskDefinition, error) {
	rows := []*TaskDefinition{}
	query := "SELECT " + taskDefinitionRows + " FROM " + m.table + " WHERE verification_mode=? AND status IN ('active','paused')"
	args := []any{mode}
	if before > 0 {
		query += " AND id<?"
		args = append(args, before)
	}
	query += " ORDER BY id DESC LIMIT ?"
	args = append(args, limit)
	err := m.conn.QueryRowsCtx(ctx, &rows, query, args...)
	return rows, err
}
