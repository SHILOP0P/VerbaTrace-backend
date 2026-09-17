package bitrix24

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

type bitrixTaskSnapshot struct {
	ID            string     `json:"id"`
	Title         string     `json:"title"`
	Description   string     `json:"description,omitempty"`
	ResponsibleID string     `json:"responsible_id"`
	Deadline      *time.Time `json:"deadline,omitempty"`
	Status        string     `json:"status"`
	ChangedAt     *time.Time `json:"changed_at,omitempty"`
	Link          string     `json:"link,omitempty"`
}

func (s *Service) reconcileOneSyncedActionTask(ctx context.Context) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return
	}
	defer func() { _ = tx.Rollback() }()
	item, err := scanActionSync(tx.QueryRowContext(ctx, actionSyncSelect+` WHERE state='synced' AND review_state IS NULL AND available_at<=now() AND (lease_until IS NULL OR lease_until<now()) ORDER BY available_at,created_at FOR UPDATE SKIP LOCKED LIMIT 1`))
	if err != nil || item.ExternalTaskID == nil {
		return
	}
	lease := s.now().UTC().Add(45 * time.Second)
	if _, err = tx.ExecContext(ctx, `UPDATE call_action_external_syncs SET lease_until=$2,updated_at=now() WHERE sync_uuid=$1`, item.ID, lease); err != nil || tx.Commit() != nil {
		return
	}

	info, token, err := s.systemConnectionToken(ctx, item.ConnectionID)
	if err != nil {
		s.scheduleTaskReconciliation(ctx, item.ID, "oauth_unavailable")
		return
	}
	snapshot, found, err := s.fetchTaskSnapshot(ctx, info, token, *item.ExternalTaskID)
	if err != nil {
		s.scheduleTaskReconciliation(ctx, item.ID, "bitrix_task_read_failed")
		return
	}
	if !found {
		s.markExternalTaskReview(ctx, item, bitrixTaskSnapshot{ID: *item.ExternalTaskID}, "external_task_missing")
		return
	}
	if safeLink, ok := safeBitrixTaskLink(snapshot.Link, info.Domain); ok {
		snapshot.Link = safeLink
	} else {
		snapshot.Link = ""
	}
	reason, err := s.externalTaskConflictReason(ctx, item.ActionID, item.ConnectionID, snapshot)
	if err != nil {
		s.scheduleTaskReconciliation(ctx, item.ID, "task_reconciliation_failed")
		return
	}
	if reason != "" {
		s.markExternalTaskReview(ctx, item, snapshot, reason)
		return
	}
	raw, _ := json.Marshal(snapshot)
	now := s.now().UTC()
	_, _ = s.db.ExecContext(ctx, `UPDATE call_action_external_syncs SET external_snapshot=$2,external_task_url=COALESCE(NULLIF($3,''),external_task_url),last_confirmed_external_version=NULLIF($4,''),last_checked_at=$5,available_at=$5+interval '5 minutes',lease_until=NULL,last_error_code=NULL,lock_version=lock_version+1,updated_at=$5 WHERE sync_uuid=$1 AND state='synced' AND review_state IS NULL`, item.ID, raw, snapshot.Link, externalSnapshotVersion(snapshot), now)
}

func (s *Service) scheduleTaskReconciliation(ctx context.Context, syncID uuid.UUID, code string) {
	_, _ = s.db.ExecContext(ctx, `UPDATE call_action_external_syncs SET lease_until=NULL,last_error_code=$2,available_at=now()+interval '5 minutes',last_checked_at=now(),lock_version=lock_version+1,updated_at=now() WHERE sync_uuid=$1 AND state='synced' AND review_state IS NULL`, syncID, code)
}

// markExternalTaskReview parks a task that no longer matches its action and
// tells the person who asked for the sync. Both happen in one transaction: a
// review nobody is told about is worse than no review at all, so a failed
// notification leaves the record alone and the reconciler tries again.
func (s *Service) markExternalTaskReview(ctx context.Context, item models.ActionExternalSync, snapshot bitrixTaskSnapshot, reason string) {
	raw, _ := json.Marshal(snapshot)
	now := s.now().UTC()
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `UPDATE call_action_external_syncs SET external_snapshot=$2,external_task_url=COALESCE(NULLIF($3,''),external_task_url),review_state='needs_review',review_reason=$4,last_checked_at=$5,lease_until=NULL,last_error_code=$4,lock_version=lock_version+1,updated_at=$5 WHERE sync_uuid=$1 AND state='synced' AND review_state IS NULL`, item.ID, raw, snapshot.Link, reason, now)
	if err != nil {
		return
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO notifications(notification_uuid,user_uuid,type,title,body,entity_type,entity_uuid) VALUES($1,$2,$3,'Изменения задачи Bitrix24 требуют решения',$4,'action_external_sync',$5)`, uuid.New(), item.RequesterUserID, models.NotificationTypeActionExternalSyncConflict, reviewReasonLabel(reason), item.ID); err != nil {
		return
	}
	_ = tx.Commit()
}

func (s *Service) externalTaskConflictReason(ctx context.Context, actionID, connectionID uuid.UUID, snapshot bitrixTaskSnapshot) (string, error) {
	var title, status, responsible string
	var dueAt time.Time
	err := s.db.QueryRowContext(ctx, `SELECT a.title,a.due_at,a.status,COALESCE(m.external_user_id,'') FROM call_actions a LEFT JOIN integration_external_user_mappings m ON m.connection_uuid=$2 AND m.internal_user_uuid=a.assignee_user_uuid AND m.status='mapped' WHERE a.action_uuid=$1`, actionID, connectionID).Scan(&title, &dueAt, &status, &responsible)
	if err != nil {
		return "", err
	}
	if snapshot.Status == "completed" && status != "completed" {
		return "external_task_completed", nil
	}
	if snapshot.Status == "declined" || snapshot.Status == "deferred" {
		return "external_task_status_changed", nil
	}
	if snapshot.Title != "" && snapshot.Title != title || snapshot.ResponsibleID != "" && snapshot.ResponsibleID != responsible || snapshot.Deadline != nil && !sameInstant(*snapshot.Deadline, dueAt) {
		return "external_task_fields_changed", nil
	}
	return "", nil
}

func (s *Service) ResolveActionSync(ctx context.Context, in models.ResolveActionExternalSyncInput) (models.ActionExternalSync, error) {
	in.Resolution = strings.TrimSpace(in.Resolution)
	in.Reason = strings.TrimSpace(in.Reason)
	if in.SyncID == uuid.Nil || in.ActorID == uuid.Nil || in.ExpectedLockVersion < 1 || len([]rune(in.Reason)) < 10 || len([]rune(in.Reason)) > 2000 {
		return models.ActionExternalSync{}, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return models.ActionExternalSync{}, err
	}
	defer func() { _ = tx.Rollback() }()
	item, err := scanActionSync(tx.QueryRowContext(ctx, actionSyncSelect+` WHERE sync_uuid=$1 FOR UPDATE`, in.SyncID))
	if err != nil {
		return item, err
	}
	if item.LockVersion != in.ExpectedLockVersion {
		return item, ErrConflict
	}
	allowed, err := s.canResolveActionSync(ctx, item.ActionID, in.ActorID)
	if err != nil || !allowed {
		return item, ErrForbidden
	}

	if item.State == "needs_review" && item.ExternalTaskID == nil {
		switch in.Resolution {
		case "retry_create":
			if item.LastErrorCode == nil || (*item.LastErrorCode != "ambiguous_provider_outcome" && *item.LastErrorCode != "worker_lease_expired_after_send" && *item.LastErrorCode != "invalid_provider_response") {
				return item, ErrConflict
			}
			_, err = tx.ExecContext(ctx, `UPDATE call_action_external_syncs SET state='queued',available_at=now(),last_error_code=NULL,reviewed_by_user_uuid=$2,reviewed_at=now(),lock_version=lock_version+1,updated_at=now() WHERE sync_uuid=$1`, item.ID, in.ActorID)
		case "cancel":
			_, err = tx.ExecContext(ctx, `UPDATE call_action_external_syncs SET state='cancelled',reviewed_by_user_uuid=$2,reviewed_at=now(),lock_version=lock_version+1,updated_at=now() WHERE sync_uuid=$1`, item.ID, in.ActorID)
		case "confirm_task":
			if strings.TrimSpace(in.ExternalTaskID) == "" {
				return item, ErrInvalid
			}
			if _, err = tx.ExecContext(ctx, `UPDATE call_action_external_syncs SET lease_until=now()+interval '45 seconds',lock_version=lock_version+1,updated_at=now() WHERE sync_uuid=$1`, item.ID); err != nil {
				return item, err
			}
			if err = tx.Commit(); err != nil {
				return item, err
			}
			return s.confirmAmbiguousTask(ctx, item, in, strings.TrimSpace(in.ExternalTaskID))
		default:
			return item, ErrInvalid
		}
		if err != nil {
			return item, err
		}
		if err = insertActionSyncResolutionEvent(ctx, tx, item, in, in.Resolution); err != nil {
			return item, err
		}
		if err = tx.Commit(); err != nil {
			return item, err
		}
		return s.GetActionSyncByID(ctx, item.ID, in.ActorID)
	}

	if item.State != "synced" || item.ReviewState == nil || item.ExternalTaskID == nil {
		return item, ErrConflict
	}
	switch in.Resolution {
	case "accept_external":
		if err = s.acceptExternalTaskSnapshot(ctx, tx, item, in); err != nil {
			return item, err
		}
	case "unlink":
		_, err = tx.ExecContext(ctx, `UPDATE call_action_external_syncs SET state='unlinked',external_task_id=NULL,external_task_url=NULL,review_state=NULL,review_reason=NULL,reviewed_by_user_uuid=$2,reviewed_at=now(),lease_until=NULL,last_error_code=NULL,lock_version=lock_version+1,updated_at=now() WHERE sync_uuid=$1`, item.ID, in.ActorID)
	case "restore_verbatrace":
		if _, err = tx.ExecContext(ctx, `UPDATE call_action_external_syncs SET lease_until=now()+interval '45 seconds',lock_version=lock_version+1,updated_at=now() WHERE sync_uuid=$1`, item.ID); err != nil {
			return item, err
		}
		if err = tx.Commit(); err != nil {
			return item, err
		}
		return s.restoreVerbaTraceTask(ctx, item, in)
	default:
		return item, ErrInvalid
	}
	if err != nil {
		return item, err
	}
	if err = insertActionSyncResolutionEvent(ctx, tx, item, in, in.Resolution); err != nil {
		return item, err
	}
	if err = tx.Commit(); err != nil {
		return item, err
	}
	return s.GetActionSyncByID(ctx, item.ID, in.ActorID)
}

func (s *Service) confirmAmbiguousTask(ctx context.Context, item models.ActionExternalSync, in models.ResolveActionExternalSyncInput, taskID string) (models.ActionExternalSync, error) {
	if len(taskID) == 0 || len(taskID) > 20 || strings.IndexFunc(taskID, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
		return item, ErrInvalid
	}
	info, token, err := s.systemConnectionToken(ctx, item.ConnectionID)
	if err != nil {
		s.releaseResolutionLease(ctx, item.ID, "oauth_unavailable")
		return item, ErrUnavailable
	}
	snapshot, found, err := s.fetchTaskSnapshot(ctx, info, token, taskID)
	if err != nil || !found || !strings.Contains(snapshot.Description, item.IdempotencyMarker) {
		s.releaseResolutionLease(ctx, item.ID, "task_marker_not_confirmed")
		return item, ErrConflict
	}
	if safeLink, ok := safeBitrixTaskLink(snapshot.Link, info.Domain); ok {
		snapshot.Link = safeLink
	} else {
		snapshot.Link = ""
	}
	raw, _ := json.Marshal(snapshot)
	now := s.now().UTC()
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return item, err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `UPDATE call_action_external_syncs SET state='synced',external_task_id=$2,external_task_url=NULLIF($3,''),external_snapshot=$4,reviewed_by_user_uuid=$5,reviewed_at=$6,synced_at=COALESCE(synced_at,$6),last_checked_at=$6,available_at=$6+interval '5 minutes',lease_until=NULL,last_error_code=NULL,lock_version=lock_version+1,updated_at=$6 WHERE sync_uuid=$1 AND state='needs_review' AND lock_version=$7`, item.ID, taskID, snapshot.Link, raw, in.ActorID, now, in.ExpectedLockVersion+1)
	if err != nil {
		return item, err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return item, ErrConflict
	}
	if err = insertActionSyncResolutionEvent(ctx, tx, item, in, "confirm_task"); err != nil {
		return item, err
	}
	if err = tx.Commit(); err != nil {
		return item, err
	}
	return s.GetActionSyncByID(ctx, item.ID, in.ActorID)
}

func (s *Service) acceptExternalTaskSnapshot(ctx context.Context, tx *sql.Tx, item models.ActionExternalSync, in models.ResolveActionExternalSyncInput) error {
	var snapshot bitrixTaskSnapshot
	if len(item.ExternalSnapshot) == 0 || json.Unmarshal(item.ExternalSnapshot, &snapshot) != nil {
		return ErrConflict
	}
	var currentTitle, currentStatus string
	var currentDue time.Time
	var currentAssignee, currentDepartment uuid.UUID
	var actionVersion int64
	if err := tx.QueryRowContext(ctx, `SELECT title,due_at,status,assignee_user_uuid,target_department_uuid,lock_version FROM call_actions WHERE action_uuid=$1 FOR UPDATE`, item.ActionID).Scan(&currentTitle, &currentDue, &currentStatus, &currentAssignee, &currentDepartment, &actionVersion); err != nil {
		return err
	}
	newTitle, newDue, newAssignee, newDepartment := currentTitle, currentDue, currentAssignee, currentDepartment
	if strings.TrimSpace(snapshot.Title) != "" {
		newTitle = strings.TrimSpace(snapshot.Title)
	}
	if snapshot.Deadline != nil {
		newDue = snapshot.Deadline.UTC()
	}
	if snapshot.ResponsibleID != "" {
		err := tx.QueryRowContext(ctx, `SELECT m.internal_user_uuid,m.department_uuid FROM integration_external_user_mappings m JOIN company_members cm ON cm.user_uuid=m.internal_user_uuid AND cm.status='active' JOIN department_members dm ON dm.user_uuid=m.internal_user_uuid AND dm.department_uuid=m.department_uuid AND dm.status='active' WHERE m.connection_uuid=$1 AND m.external_user_id=$2 AND m.status='mapped'`, item.ConnectionID, snapshot.ResponsibleID).Scan(&newAssignee, &newDepartment)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrConflict
		}
		if err != nil {
			return err
		}
	}
	newStatus := actionStatusForExternal(snapshot.Status, currentStatus, newDue, s.now().UTC())
	now := s.now().UTC()
	result, err := tx.ExecContext(ctx, `UPDATE call_actions SET title=$2,due_at=$3,grace_expires_at=$3+interval '24 hours',assignee_user_uuid=$4,target_department_uuid=$5,status=$6,completed_at=CASE WHEN $6='completed' THEN COALESCE(completed_at,$7) ELSE NULL END,completed_by_user_uuid=CASE WHEN $6='completed' THEN $8 ELSE NULL END,cancelled_at=CASE WHEN $6='cancelled' THEN COALESCE(cancelled_at,$7) ELSE NULL END,cancelled_by_user_uuid=CASE WHEN $6='cancelled' THEN $8 ELSE NULL END,cancel_reason=CASE WHEN $6='cancelled' THEN $9 ELSE NULL END,updated_at=$7,lock_version=lock_version+1,schedule_version=schedule_version+1 WHERE action_uuid=$1 AND lock_version=$10`, item.ActionID, newTitle, newDue, newAssignee, newDepartment, newStatus, now, in.ActorID, in.Reason, actionVersion)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return ErrConflict
	}
	oldData, _ := json.Marshal(map[string]any{"title": currentTitle, "due_at": currentDue, "assignee_user_uuid": currentAssignee, "status": currentStatus})
	newData, _ := json.Marshal(map[string]any{"title": newTitle, "due_at": newDue, "assignee_user_uuid": newAssignee, "department_uuid": newDepartment, "status": newStatus})
	if _, err = tx.ExecContext(ctx, `INSERT INTO call_action_events(event_uuid,action_uuid,event_type,actor_user_uuid,reason,old_data,new_data) VALUES($1,$2,'external_sync_accepted',$3,$4,$5,$6)`, uuid.New(), item.ActionID, in.ActorID, in.Reason, oldData, newData); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE call_action_external_syncs SET review_state=NULL,review_reason=NULL,reviewed_by_user_uuid=$2,reviewed_at=$3,lease_until=NULL,last_error_code=NULL,available_at=$3+interval '5 minutes',lock_version=lock_version+1,updated_at=$3 WHERE sync_uuid=$1`, item.ID, in.ActorID, now)
	return err
}

func (s *Service) restoreVerbaTraceTask(ctx context.Context, item models.ActionExternalSync, in models.ResolveActionExternalSyncInput) (models.ActionExternalSync, error) {
	info, token, err := s.systemConnectionToken(ctx, item.ConnectionID)
	if err != nil {
		s.releaseResolutionLease(ctx, item.ID, "oauth_unavailable")
		return item, ErrUnavailable
	}
	var title, status, responsible string
	var dueAt time.Time
	err = s.db.QueryRowContext(ctx, `SELECT a.title,a.due_at,a.status,m.external_user_id FROM call_actions a JOIN integration_external_user_mappings m ON m.connection_uuid=$2 AND m.internal_user_uuid=a.assignee_user_uuid AND m.status='mapped' WHERE a.action_uuid=$1`, item.ActionID, item.ConnectionID).Scan(&title, &dueAt, &status, &responsible)
	if err != nil {
		s.releaseResolutionLease(ctx, item.ID, "assignee_not_mapped")
		return item, ErrConflict
	}
	fields := map[string]any{"TITLE": title, "DEADLINE": dueAt.Format(time.RFC3339), "RESPONSIBLE_ID": responsible, "STATUS": externalStatusCodeForAction(status)}
	if err = s.call(ctx, info.Domain, "tasks.task.update", token, map[string]any{"taskId": *item.ExternalTaskID, "fields": fields}, nil); err != nil {
		s.releaseResolutionLease(ctx, item.ID, "bitrix_task_restore_failed")
		return item, ErrUnavailable
	}
	now := s.now().UTC()
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return item, err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `UPDATE call_action_external_syncs SET review_state=NULL,review_reason=NULL,reviewed_by_user_uuid=$2,reviewed_at=$3,last_checked_at=$3,available_at=$3+interval '1 minute',lease_until=NULL,last_error_code=NULL,lock_version=lock_version+1,updated_at=$3 WHERE sync_uuid=$1 AND state='synced' AND lock_version=$4`, item.ID, in.ActorID, now, in.ExpectedLockVersion+1)
	if err != nil {
		return item, err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return item, ErrConflict
	}
	if err = insertActionSyncResolutionEvent(ctx, tx, item, in, "restore_verbatrace"); err != nil {
		return item, err
	}
	if err = tx.Commit(); err != nil {
		return item, err
	}
	return s.GetActionSyncByID(ctx, item.ID, in.ActorID)
}

func (s *Service) releaseResolutionLease(ctx context.Context, syncID uuid.UUID, code string) {
	_, _ = s.db.ExecContext(ctx, `UPDATE call_action_external_syncs SET lease_until=NULL,last_error_code=$2,lock_version=lock_version+1,updated_at=now() WHERE sync_uuid=$1`, syncID, code)
}

func insertActionSyncResolutionEvent(ctx context.Context, tx *sql.Tx, item models.ActionExternalSync, in models.ResolveActionExternalSyncInput, resolution string) error {
	metadata, _ := json.Marshal(map[string]any{"resolution": resolution})
	_, err := tx.ExecContext(ctx, `INSERT INTO integration_audit_events(audit_event_uuid,application_uuid,connection_uuid,actor_type,actor_uuid,event_type,entity_type,entity_uuid,metadata_safe) SELECT $1,c.application_uuid,c.connection_uuid,'user',$2,'task_sync.review_resolved','action_external_sync',$3,$4 FROM integration_connections c WHERE c.connection_uuid=$5`, uuid.New(), in.ActorID, item.ID, metadata, item.ConnectionID)
	return err
}

func (s *Service) fetchTaskSnapshot(ctx context.Context, info connectionInfo, token, taskID string) (bitrixTaskSnapshot, bool, error) {
	var result any
	err := s.call(ctx, info.Domain, "tasks.task.get", token, map[string]any{"taskId": taskID, "select": []string{"ID", "TITLE", "DESCRIPTION", "RESPONSIBLE_ID", "DEADLINE", "STATUS", "CHANGED_DATE", "LINK"}}, &result)
	if err != nil {
		return bitrixTaskSnapshot{}, false, err
	}
	object := taskObject(result)
	if len(object) == 0 {
		return bitrixTaskSnapshot{}, false, nil
	}
	snapshot := bitrixTaskSnapshot{
		ID: stringMapValue(object, "id", "ID"), Title: stringMapValue(object, "title", "TITLE"), Description: stringMapValue(object, "description", "DESCRIPTION"),
		ResponsibleID: stringMapValue(object, "responsibleId", "RESPONSIBLE_ID"), Status: normalizeExternalTaskStatus(stringMapValue(object, "status", "STATUS")), Link: stringMapValue(object, "link", "LINK"),
	}
	if snapshot.ResponsibleID == "" {
		if responsible, ok := object["responsible"].(map[string]any); ok {
			snapshot.ResponsibleID = stringMapValue(responsible, "id", "ID")
		}
	}
	snapshot.Deadline = parseExternalTime(stringMapValue(object, "deadline", "DEADLINE"))
	snapshot.ChangedAt = parseExternalTime(stringMapValue(object, "changed", "changedDate", "CHANGED_DATE"))
	if snapshot.ID == "" {
		return bitrixTaskSnapshot{}, false, nil
	}
	return snapshot, true, nil
}

func taskObject(value any) map[string]any {
	object, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	for _, key := range []string{"task", "item"} {
		if nested, nestedOK := object[key].(map[string]any); nestedOK {
			return nested
		}
	}
	return object
}

func stringMapValue(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := values[key]; ok {
			if result := stringValue(value); result != "" {
				return result
			}
		}
	}
	return ""
}

func parseExternalTime(value string) *time.Time {
	if value == "" {
		return nil
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05-07:00"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			parsed = parsed.UTC()
			return &parsed
		}
	}
	return nil
}

func normalizeExternalTaskStatus(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "2", "pending":
		return "pending"
	case "3", "in_progress":
		return "in_progress"
	case "4", "supposedly_completed":
		return "supposedly_completed"
	case "5", "completed":
		return "completed"
	case "6", "deferred":
		return "deferred"
	case "7", "declined":
		return "declined"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func actionStatusForExternal(external, current string, due, now time.Time) string {
	switch external {
	case "completed":
		return "completed"
	case "declined":
		return "cancelled"
	case "in_progress", "supposedly_completed":
		if !due.After(now) {
			return "overdue"
		}
		return "in_progress"
	case "pending", "deferred":
		if !due.After(now) {
			return "overdue"
		}
		return "open"
	default:
		return current
	}
}

func externalStatusCodeForAction(status string) string {
	switch status {
	case "completed":
		return "5"
	case "in_progress":
		return "3"
	case "cancelled":
		// The classic tasks.task.update contract accepts task statuses 2..6.
		// It has no separate "cancelled" state, so keep a cancelled VerbaTrace
		// action as a deferred Bitrix24 task instead of sending invalid status 7.
		return "6"
	default:
		return "2"
	}
}

func externalSnapshotVersion(snapshot bitrixTaskSnapshot) string {
	if snapshot.ChangedAt != nil {
		return snapshot.ChangedAt.Format(time.RFC3339Nano)
	}
	return snapshot.Status
}

func sameInstant(left, right time.Time) bool {
	delta := left.Sub(right)
	return delta > -time.Second && delta < time.Second
}

func safeBitrixTaskLink(raw, domain string) (string, bool) {
	if strings.TrimSpace(raw) == "" {
		return "", false
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || !strings.EqualFold(parsed.Hostname(), domain) || parsed.Port() != "" || parsed.User != nil {
		return "", false
	}
	return parsed.String(), true
}

func reviewReasonLabel(reason string) string {
	switch reason {
	case "external_task_completed":
		return "Задача завершена в Bitrix24. Подтвердите, нужно ли завершить действие VerbaTrace."
	case "external_task_missing":
		return "Задача больше не доступна в Bitrix24. Действие VerbaTrace сохранено."
	case "external_task_status_changed":
		return "Статус задачи изменён в Bitrix24 и требует подтверждения."
	default:
		return "Поля задачи изменены в Bitrix24. Выберите источник актуальных данных."
	}
}
