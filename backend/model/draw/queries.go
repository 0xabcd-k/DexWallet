package draw

import (
	"context"

	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

const ControlID int64 = 1

func Current(ctx context.Context, conn sqlx.SqlConn, lock bool) (*DrawControl, error) {
	q := "SELECT " + drawControlRows + " FROM draw_control WHERE id=?"
	if lock {
		q += " FOR SHARE"
	}
	var row DrawControl
	err := conn.QueryRowCtx(ctx, &row, q, ControlID)
	return &row, err
}

func Page(ctx context.Context, conn sqlx.SqlConn, userID, before, limit int64) ([]*PointDraw, error) {
	rows := []*PointDraw{}
	q := "SELECT " + pointDrawRows + " FROM point_draw WHERE user_id=?"
	args := []any{userID}
	if before > 0 {
		q += " AND id<?"
		args = append(args, before)
	}
	q += " ORDER BY id DESC LIMIT ?"
	args = append(args, limit)
	err := conn.QueryRowsCtx(ctx, &rows, q, args...)
	return rows, err
}
