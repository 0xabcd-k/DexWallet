package user

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

type BindingRow struct {
	Id             int64         `db:"id"`
	UserId         sql.NullInt64 `db:"user_id"`
	BindingVersion int64         `db:"binding_version"`
}

func identityTable(kind IdentityType) (string, error) {
	switch kind {
	case IdentityEmail:
		return "`email_identity`", nil
	case IdentityTelegram:
		return "`telegram_identity`", nil
	}
	return "", errors.New("unknown identity type")
}
func LockBinding(ctx context.Context, session sqlx.Session, kind IdentityType, id int64) (*BindingRow, error) {
	table, err := identityTable(kind)
	if err != nil {
		return nil, err
	}
	var row BindingRow
	if err = session.QueryRowCtx(ctx, &row, "SELECT id,user_id,binding_version FROM "+table+" WHERE id=? FOR UPDATE", id); err != nil {
		return nil, err
	}
	return &row, nil
}

// ChangeBinding requires the caller to have locked this row and its product user.
func ChangeBinding(ctx context.Context, session sqlx.Session, kind IdentityType, row *BindingRow, owner sql.NullInt64) error {
	table, err := identityTable(kind)
	if err != nil {
		return err
	}
	result, err := session.ExecCtx(ctx, "UPDATE "+table+" SET user_id=?,bound_at=IF(? IS NULL,NULL,UTC_TIMESTAMP(6)),binding_version=binding_version+1 WHERE id=? AND binding_version=?", owner, owner, row.Id, row.BindingVersion)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("binding changed concurrently")
	}
	return nil
}
