package auth

import (
	"context"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

type AuthOperationModel interface {
	authOperationModel
	WithSession(sqlx.Session) AuthOperationModel
	Reserve(context.Context, *AuthOperation) error
	Lock(context.Context, string) (*AuthOperation, error)
}
type customAuthOperationModel struct{ *defaultAuthOperationModel }

func NewAuthOperationModel(conn sqlx.SqlConn) AuthOperationModel {
	return &customAuthOperationModel{newAuthOperationModel(conn)}
}
func (m *customAuthOperationModel) WithSession(s sqlx.Session) AuthOperationModel {
	return NewAuthOperationModel(sqlx.NewSqlConnFromSession(s))
}
func (m *customAuthOperationModel) Reserve(ctx context.Context, op *AuthOperation) error {
	_, err := m.conn.ExecCtx(ctx, "INSERT INTO auth_operation(request_digest,input_digest,purpose,identity_type,actor_id,actor_version,expires_at) VALUES(?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE id=LAST_INSERT_ID(id)", op.RequestDigest, op.InputDigest, op.Purpose, op.IdentityType, op.ActorId, op.ActorVersion, op.ExpiresAt)
	return err
}
func (m *customAuthOperationModel) Lock(ctx context.Context, digest string) (*AuthOperation, error) {
	var row AuthOperation
	err := m.conn.QueryRowCtx(ctx, &row, "SELECT "+authOperationRows+" FROM auth_operation WHERE request_digest=? FOR UPDATE", digest)
	if err != nil {
		return nil, err
	}
	return &row, nil
}
