package backoffice

import "github.com/zeromicro/go-zero/core/stores/sqlx"

var _ MerchantUserModel = (*customMerchantUserModel)(nil)

type (
	// MerchantUserModel is an interface to be customized, add more methods here,
	// and implement the added methods in customMerchantUserModel.
	MerchantUserModel interface {
		merchantUserModel
		withSession(session sqlx.Session) MerchantUserModel
	}

	customMerchantUserModel struct {
		*defaultMerchantUserModel
	}
)

// NewMerchantUserModel returns a model for the database table.
func NewMerchantUserModel(conn sqlx.SqlConn) MerchantUserModel {
	return &customMerchantUserModel{
		defaultMerchantUserModel: newMerchantUserModel(conn),
	}
}

func (m *customMerchantUserModel) withSession(session sqlx.Session) MerchantUserModel {
	return NewMerchantUserModel(sqlx.NewSqlConnFromSession(session))
}
