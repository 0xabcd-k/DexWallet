package draw

import "github.com/zeromicro/go-zero/core/stores/sqlx"

var _ DrawRuleVersionModel = (*customDrawRuleVersionModel)(nil)

type (
	// DrawRuleVersionModel is an interface to be customized, add more methods here,
	// and implement the added methods in customDrawRuleVersionModel.
	DrawRuleVersionModel interface {
		drawRuleVersionModel
		withSession(session sqlx.Session) DrawRuleVersionModel
	}

	customDrawRuleVersionModel struct {
		*defaultDrawRuleVersionModel
	}
)

// NewDrawRuleVersionModel returns a model for the database table.
func NewDrawRuleVersionModel(conn sqlx.SqlConn) DrawRuleVersionModel {
	return &customDrawRuleVersionModel{
		defaultDrawRuleVersionModel: newDrawRuleVersionModel(conn),
	}
}

func (m *customDrawRuleVersionModel) withSession(session sqlx.Session) DrawRuleVersionModel {
	return NewDrawRuleVersionModel(sqlx.NewSqlConnFromSession(session))
}
