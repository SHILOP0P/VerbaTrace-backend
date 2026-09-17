package action

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"verbatrace/monolit/internal/models"

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
	// A call in the bin freezes its actions: no reminders go out while it is
	// there, and they resume untouched if it comes back.
	rows, err := w.service.db.QueryContext(ctx, `SELECT a.action_uuid,a.assignee_user_uuid,a.title,a.due_at,a.schedule_version FROM call_actions a JOIN calls c ON c.call_uuid=a.call_uuid WHERE a.status IN ('open','in_progress') AND a.assignment_state='valid' AND c.deleted_at IS NULL AND a.due_at <= $1 AND a.grace_expires_at > $2 ORDER BY a.due_at,a.action_uuid LIMIT $3`, now.Add(7*24*time.Hour), now, w.batch)
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
		// An action whose call is in the bin does not fall overdue: the clock stops
		// with the call and starts again if it is restored.
		rows, err := tx.QueryContext(ctx, `SELECT a.action_uuid,a.company_uuid,a.target_department_uuid,a.assignee_user_uuid,a.title,a.lock_version FROM call_actions a JOIN calls c ON c.call_uuid=a.call_uuid WHERE a.status IN ('open','in_progress') AND a.assignment_state='valid' AND c.deleted_at IS NULL AND a.grace_expires_at <= $1 ORDER BY a.grace_expires_at,a.action_uuid FOR UPDATE OF a SKIP LOCKED LIMIT $2`, w.service.now().UTC(), w.batch)
		if err != nil {
			_ = tx.Rollback()
			return
		}
		type candidate struct {
			id, assignee        uuid.UUID
			company, department uuid.NullUUID
			title               string
			version             int64
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
			if c.company.Valid && c.department.Valid {
				leaders, _ := leadersAndManagers(ctx, tx, c.company.UUID, c.department.UUID)
				for _, recipient := range leaders {
					if recipient != c.assignee {
						_ = createNotification(ctx, tx, c.id, recipient, "action_overdue", "Действие просрочено", c.title, c.version+1)
					}
				}
			}
		}
		if tx.Commit() != nil {
			return
		}
	}
}

func (w *Worker) runInvalidAssignments(ctx context.Context) {
	// An action whose call is in the bin is frozen along with the call, so this
	// worker leaves it alone too. It used to be the only one of the three that
	// did not look, and it kept invalidating assignments and notifying people
	// about calls that had been deleted.
	rows, err := w.service.db.QueryContext(ctx, `SELECT a.action_uuid,a.company_uuid,a.target_department_uuid,a.assignee_user_uuid,a.title,a.lock_version FROM call_actions a JOIN calls c ON c.call_uuid=a.call_uuid WHERE a.company_uuid IS NOT NULL AND a.status IN ('open','in_progress','overdue') AND a.assignment_state='valid' AND c.deleted_at IS NULL AND NOT EXISTS(SELECT 1 FROM company_members cm JOIN department_members dm ON dm.user_uuid=cm.user_uuid AND dm.department_uuid=a.target_department_uuid WHERE cm.company_uuid=a.company_uuid AND cm.user_uuid=a.assignee_user_uuid AND cm.status='active' AND dm.status='active') LIMIT $1`, w.batch)
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
		// The work does not stop because the person who had it left. It moves to
		// whoever is responsible for that department — the leader, then the
		// deputy, then the owner — and only stays unassigned when the company has
		// nobody left to take it.
		successor, e := assignmentSuccessor(ctx, tx, company, department, assignee)
		if e != nil {
			_ = tx.Rollback()
			continue
		}

		if successor == uuid.Nil {
			res, e := tx.ExecContext(ctx, `UPDATE call_actions SET assignment_state='invalid',updated_at=$1,lock_version=lock_version+1 WHERE action_uuid=$2 AND lock_version=$3 AND assignment_state='valid'`, w.service.now().UTC(), id, version)
			if e != nil {
				_ = tx.Rollback()
				continue
			}
			if n, _ := res.RowsAffected(); n == 0 {
				_ = tx.Rollback()
				continue
			}
			_ = insertEvent(ctx, tx, id, "assignment_invalid", uuid.Nil, "", nil, nil)
			recipients, _ := leadersAndManagers(ctx, tx, company, department)
			for _, recipient := range recipients {
				_ = createNotification(ctx, tx, id, recipient, "action_assignment_invalid", "Нужно переназначить действие", title, version+1)
			}
			_ = tx.Commit()
			continue
		}

		res, e := tx.ExecContext(ctx, `UPDATE call_actions SET assignee_user_uuid=$1,assignment_state='valid',updated_at=$2,lock_version=lock_version+1,schedule_version=schedule_version+1 WHERE action_uuid=$3 AND lock_version=$4 AND assignment_state='valid'`, successor, w.service.now().UTC(), id, version)
		if e != nil {
			_ = tx.Rollback()
			continue
		}
		if n, _ := res.RowsAffected(); n == 0 {
			_ = tx.Rollback()
			continue
		}
		_ = insertEvent(ctx, tx, id, "reassigned", uuid.Nil, "", map[string]any{"assignee_user_uuid": assignee.String()}, map[string]any{"assignee_user_uuid": successor.String()})
		_ = createNotification(ctx, tx, id, successor, string(models.NotificationTypeActionReassigned), "Действие передано вам", title, version+1)
		_ = tx.Commit()
	}
}

// assignmentSuccessor answers who takes over an action whose assignee is gone:
// the leader of its department first, then the deputy, then the owner. The
// person who left is skipped even if a stale row still names them.
func assignmentSuccessor(ctx context.Context, tx *sql.Tx, company, department, leaving uuid.UUID) (uuid.UUID, error) {
	var successor uuid.NullUUID
	err := tx.QueryRowContext(ctx, `
		SELECT user_uuid FROM (
			SELECT dm.user_uuid, 1 AS rank
			FROM department_members dm
			WHERE dm.department_uuid = $2 AND dm.status = 'active' AND dm.role = 'department_leader'
			UNION ALL
			SELECT cm.user_uuid, CASE cm.role WHEN 'company_deputy' THEN 2 ELSE 3 END
			FROM company_members cm
			WHERE cm.company_uuid = $1 AND cm.status = 'active' AND cm.role IN ('company_deputy','company_manager')
		) candidates
		WHERE user_uuid <> $3
		ORDER BY rank
		LIMIT 1
	`, company, department, leaving).Scan(&successor)
	if errors.Is(err, sql.ErrNoRows) {
		return uuid.Nil, nil
	}
	if err != nil {
		return uuid.Nil, err
	}
	if !successor.Valid {
		return uuid.Nil, nil
	}

	return successor.UUID, nil
}
