package action

import (
	"context"
	"database/sql"
	"time"

	"github.com/google/uuid"
)

type Worker struct {
	service  *Service
	interval time.Duration
	batch    int
}

func NewWorker(service *Service, interval time.Duration, batch int) *Worker {
	if interval <= 0 {
		interval = time.Hour
	}
	if batch <= 0 {
		batch = 500
	}
	return &Worker{service: service, interval: interval, batch: batch}
}

func (w *Worker) Run(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		w.RunOnce(ctx)
		ticker := time.NewTicker(w.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				w.RunOnce(ctx)
			}
		}
	}()
	return done
}

func (w *Worker) RunOnce(ctx context.Context) {
	w.runReminders(ctx)
	w.runInvalidAssignments(ctx)
}

func (w *Worker) runReminders(ctx context.Context) {
	now := w.service.now().UTC()
	rows, err := w.service.db.QueryContext(ctx, `SELECT action_uuid,assignee_user_uuid,title,due_at,schedule_version FROM call_actions WHERE status IN ('open','in_progress') AND assignment_state='valid' AND due_at <= $1 AND grace_expires_at > $2 ORDER BY due_at,action_uuid LIMIT $3`, now.Add(7*24*time.Hour), now, w.batch)
	if err != nil {
		return
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id, user uuid.UUID
		var title string
		var due time.Time
		var version int64
		if rows.Scan(&id, &user, &title, &due, &version) != nil {
			continue
		}
		kind, notificationTitle := reminderFor(due.Sub(now))
		tx, beginErr := w.service.db.BeginTx(ctx, nil)
		if beginErr != nil {
			continue
		}
		created, createErr := createReminderNotification(ctx, tx, id, user, kind, notificationTitle, title, version)
		if createErr == nil && created {
			createErr = insertEvent(ctx, tx, id, "reminder_sent", uuid.Nil, kind, nil, nil)
		}
		if createErr != nil || tx.Commit() != nil {
			_ = tx.Rollback()
		}
	}
}

func reminderFor(remaining time.Duration) (string, string) {
	switch {
	case remaining > 5*24*time.Hour:
		return "action_reminder_7d", "До срока действия 7 дней"
	case remaining > 2*24*time.Hour:
		return "action_reminder_5d", "До срока действия 5 дней"
	case remaining > 24*time.Hour:
		return "action_reminder_2d", "До срока действия 2 дня"
	case remaining > 0:
		return "action_reminder_1d", "До срока действия 1 день"
	case remaining > -12*time.Hour:
		return "action_grace_started", "Срок действия истёк"
	default:
		return "action_grace_12h", "Осталось 12 часов до просрочки"
	}
}

func createReminderNotification(ctx context.Context, tx *sql.Tx, actionID, userID uuid.UUID, deliveryKind, title, body string, schedule int64) (bool, error) {
	notificationType := "action_reminder"
	if deliveryKind == "action_grace_started" {
		notificationType = "action_grace_started"
	}
	deliveryID, notificationID := uuid.New(), uuid.New()
	res, err := tx.ExecContext(ctx, `INSERT INTO call_action_notification_deliveries(delivery_uuid,action_uuid,recipient_uuid,kind,schedule_version,notification_uuid) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING`, deliveryID, actionID, userID, deliveryKind, schedule, notificationID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return false, nil
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO notifications(notification_uuid,user_uuid,type,title,body,entity_type,entity_uuid,created_at) VALUES($1,$2,$3,$4,$5,'call_action',$6,now())`, notificationID, userID, notificationType, title, body, actionID)
	return err == nil, err
}

type OverdueWorker struct{ *Worker }

func NewOverdueWorker(service *Service, interval time.Duration, batch int) *OverdueWorker {
	return &OverdueWorker{Worker: NewWorker(service, interval, batch)}
}

func (w *OverdueWorker) Run(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		w.RunOnce(ctx)
		ticker := time.NewTicker(w.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				w.RunOnce(ctx)
			}
		}
	}()
	return done
}

func (w *OverdueWorker) RunOnce(ctx context.Context) {
	for {
		tx, err := w.service.db.BeginTx(ctx, nil)
		if err != nil {
			return
		}
		rows, err := tx.QueryContext(ctx, `SELECT action_uuid,company_uuid,target_department_uuid,assignee_user_uuid,title,lock_version FROM call_actions WHERE status IN ('open','in_progress') AND assignment_state='valid' AND grace_expires_at <= $1 ORDER BY grace_expires_at,action_uuid FOR UPDATE SKIP LOCKED LIMIT $2`, w.service.now().UTC(), w.batch)
		if err != nil {
			_ = tx.Rollback()
			return
		}
		type candidate struct {
			id, company, department, assignee uuid.UUID
			title                             string
			version                           int64
		}
		items := []candidate{}
		for rows.Next() {
			var c candidate
			if rows.Scan(&c.id, &c.company, &c.department, &c.assignee, &c.title, &c.version) == nil {
				items = append(items, c)
			}
		}
		_ = rows.Close()
		if len(items) == 0 {
			_ = tx.Commit()
			return
		}
		for _, c := range items {
			res, e := tx.ExecContext(ctx, `UPDATE call_actions SET status='overdue',updated_at=$1,lock_version=lock_version+1 WHERE action_uuid=$2 AND lock_version=$3 AND status IN ('open','in_progress')`, w.service.now().UTC(), c.id, c.version)
			if e != nil {
				continue
			}
			n, _ := res.RowsAffected()
			if n == 0 {
				continue
			}
			_ = insertEvent(ctx, tx, c.id, "overdue", uuid.Nil, "", map[string]any{"status": "open"}, map[string]any{"status": "overdue"})
			_ = createNotification(ctx, tx, c.id, c.assignee, "action_overdue", "Действие просрочено", c.title, c.version+1)
			leaders, _ := leadersAndManagers(ctx, tx, c.company, c.department)
			for _, recipient := range leaders {
				if recipient != c.assignee {
					_ = createNotification(ctx, tx, c.id, recipient, "action_overdue", "Действие просрочено", c.title, c.version+1)
				}
			}
		}
		if tx.Commit() != nil {
			return
		}
	}
}

func (w *Worker) runInvalidAssignments(ctx context.Context) {
	rows, err := w.service.db.QueryContext(ctx, `SELECT a.action_uuid,a.company_uuid,a.target_department_uuid,a.assignee_user_uuid,a.title,a.lock_version FROM call_actions a WHERE a.status IN ('open','in_progress','overdue') AND a.assignment_state='valid' AND NOT EXISTS(SELECT 1 FROM company_members cm JOIN department_members dm ON dm.user_uuid=cm.user_uuid AND dm.department_uuid=a.target_department_uuid WHERE cm.company_uuid=a.company_uuid AND cm.user_uuid=a.assignee_user_uuid AND cm.status='active' AND dm.status='active') LIMIT $1`, w.batch)
	if err != nil {
		return
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id, company, department, assignee uuid.UUID
		var title string
		var version int64
		if rows.Scan(&id, &company, &department, &assignee, &title, &version) != nil {
			continue
		}
		tx, e := w.service.db.BeginTx(ctx, nil)
		if e != nil {
			continue
		}
		res, e := tx.ExecContext(ctx, `UPDATE call_actions SET assignment_state='invalid',updated_at=$1,lock_version=lock_version+1 WHERE action_uuid=$2 AND lock_version=$3 AND assignment_state='valid'`, w.service.now().UTC(), id, version)
		if e != nil {
			_ = tx.Rollback()
			continue
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			_ = tx.Rollback()
			continue
		}
		_ = insertEvent(ctx, tx, id, "assignment_invalid", uuid.Nil, "", nil, nil)
		recipients, _ := leadersAndManagers(ctx, tx, company, department)
		for _, recipient := range recipients {
			_ = createNotification(ctx, tx, id, recipient, "action_assignment_invalid", "Нужно переназначить действие", title, version+1)
		}
		_ = tx.Commit()
	}
}

var _ *sql.DB
