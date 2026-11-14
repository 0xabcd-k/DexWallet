package draw

import "github.com/zeromicro/go-zero/core/stores/sqlx"

var _ DrawControlModel = (*customDrawControlModel)(nil)

type (
	// DrawControlModel is an interface to be customized, add more methods here,
	// and implement the added methods in customDrawControlModel.
	DrawControlModel interface {
		drawControlModel
		withSession(session sqlx.Session) DrawControlModel
	}

	customDrawControlModel struct {
		*defaultDrawControlModel
	}
)

// NewDrawControlModel returns a model for the database table.
func NewDrawControlModel(conn sqlx.SqlConn) DrawControlModel {
	return &customDrawControlModel{
		defaultDrawControlModel: newDrawControlModel(conn),
	}
}

func (m *customDrawControlModel) withSession(session sqlx.Session) DrawControlModel {
	return NewDrawControlModel(sqlx.NewSqlConnFromSession(session))
}
