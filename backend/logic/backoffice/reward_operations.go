package backoffice

import (
	"context"
	"fmt"
	"github.com/0xabcd-k/DexWallet/backend/apperror"
	"github.com/0xabcd-k/DexWallet/backend/logic/draw"
	"github.com/0xabcd-k/DexWallet/backend/logic/task"
	drawmodel "github.com/0xabcd-k/DexWallet/backend/model/draw"
	taskmodel "github.com/0xabcd-k/DexWallet/backend/model/task"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

const (
	Task              Realm = "task"
	DrawVersion       Realm = "draw_version"
	ActionSaveTask          = "save_task_draft"
	ActionTaskStatus        = "set_task_status"
	ActionSaveDraw          = "save_draw_version"
	ActionDrawControl       = "set_draw_control"
)

func (s *Service) ManagedTasks(ctx context.Context, a Actor, t *task.Service, before, limit int64) ([]*taskmodel.TaskDefinition, int64, error) {
	if _, err := s.check(ctx, s.store, a, true, false); err != nil {
		return nil, 0, err
	}
	return t.Managed(ctx, before, limit)
}
func (s *Service) ManagedCompletions(ctx context.Context, a Actor, t *task.Service, id, before, limit int64) ([]*taskmodel.TaskCompletion, int64, error) {
	if _, err := s.check(ctx, s.store, a, true, false); err != nil {
		return nil, 0, err
	}
	return t.ManagedCompletions(ctx, id, before, limit)
}
func (s *Service) ManagedDraw(ctx context.Context, a Actor, d *draw.Service, before, limit int64) (*draw.Management, error) {
	if _, err := s.check(ctx, s.store, a, true, false); err != nil {
		return nil, err
	}
	return d.Versions(ctx, before, limit)
}

// Lock the administrator before mutation. The configuration and its audit share
// the same transaction, including retries that observe an already applied write.
func (s *Service) rewardOperation(ctx context.Context, a Actor, reason string, work func(context.Context, sqlx.Session, *store) error) error {
	if a.Realm != Admin {
		return apperror.New(apperror.PermissionDenied, nil)
	}
	if !reasonOK(reason) {
		return apperror.New(apperror.InvalidArgument, nil)
	}
	return s.tx.TransactCtx(ctx, func(ctx context.Context, session sqlx.Session) error {
		m := s.store.session(session)
		if _, err := s.check(ctx, m, a, true, true); err != nil {
			return err
		}
		return work(ctx, session, m)
	})
}
func (s *Service) SaveTask(ctx context.Context, a Actor, t *task.Service, d task.Draft, reason string) (*taskmodel.TaskDefinition, error) {
	var row *taskmodel.TaskDefinition
	d.CreatorID = a.ID
	err := s.rewardOperation(ctx, a, reason, func(ctx context.Context, tx sqlx.Session, m *store) error {
		var changed bool
		var err error
		row, changed, err = t.SaveDraftInSession(ctx, tx, d)
		if err != nil || !changed {
			return err
		}
		return s.audit(ctx, m, a, ActionSaveTask, Task, &Account{Id: row.Id}, fmt.Sprint(d.ExpectedRevision), fmt.Sprintf("revision=%d,status=%s,points=%d", row.Revision, row.Status, row.BasePoints), reason, ResultSuccess)
	})
	return row, err
}
func (s *Service) SetTaskStatus(ctx context.Context, a Actor, t *task.Service, id, expected int64, status, reason string) (*taskmodel.TaskDefinition, error) {
	var row *taskmodel.TaskDefinition
	err := s.rewardOperation(ctx, a, reason, func(ctx context.Context, tx sqlx.Session, m *store) error {
		var changed bool
		var err error
		row, changed, err = t.SetStatusInSession(ctx, tx, id, expected, status)
		if err != nil || !changed {
			return err
		}
		return s.audit(ctx, m, a, ActionTaskStatus, Task, &Account{Id: id}, fmt.Sprint(expected), fmt.Sprintf("revision=%d,status=%s", row.Revision, row.Status), reason, ResultSuccess)
	})
	return row, err
}
func (s *Service) SaveDrawVersion(ctx context.Context, a Actor, d *draw.Service, name string, r draw.Rules, reason string) (*draw.Version, error) {
	var row *draw.Version
	err := s.rewardOperation(ctx, a, reason, func(ctx context.Context, tx sqlx.Session, m *store) error {
		var changed bool
		var err error
		row, changed, err = d.SaveVersionInSession(ctx, tx, name, r)
		if err != nil || !changed {
			return err
		}
		return s.audit(ctx, m, a, ActionSaveDraw, DrawVersion, &Account{Id: row.Row.Id}, "", name, reason, ResultSuccess)
	})
	return row, err
}
func (s *Service) SetDrawControl(ctx context.Context, a Actor, d *draw.Service, id, expected int64, reason string) (*drawmodel.DrawControl, error) {
	var row *drawmodel.DrawControl
	err := s.rewardOperation(ctx, a, reason, func(ctx context.Context, tx sqlx.Session, m *store) error {
		var changed bool
		var err error
		row, changed, err = d.SetControlInSession(ctx, tx, id, expected)
		if err != nil || !changed {
			return err
		}
		return s.audit(ctx, m, a, ActionDrawControl, DrawVersion, &Account{Id: id}, fmt.Sprint(expected), fmt.Sprintf("revision=%d,version=%d", row.Revision, id), reason, ResultSuccess)
	})
	return row, err
}
