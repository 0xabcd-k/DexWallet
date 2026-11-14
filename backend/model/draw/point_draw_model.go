package draw

import "github.com/zeromicro/go-zero/core/stores/sqlx"

var _ PointDrawModel = (*customPointDrawModel)(nil)

type (
	// PointDrawModel is an interface to be customized, add more methods here,
	// and implement the added methods in customPointDrawModel.
	PointDrawModel interface {
		pointDrawModel
		withSession(session sqlx.Session) PointDrawModel
	}

	customPointDrawModel struct {
		*defaultPointDrawModel
	}
)

// NewPointDrawModel returns a model for the database table.
func NewPointDrawModel(conn sqlx.SqlConn) PointDrawModel {
	return &customPointDrawModel{
		defaultPointDrawModel: newPointDrawModel(conn),
	}
}

func (m *customPointDrawModel) withSession(session sqlx.Session) PointDrawModel {
	return NewPointDrawModel(sqlx.NewSqlConnFromSession(session))
}
