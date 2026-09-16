package action

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

func (s *Service) Start(ctx context.Context, in UpdateInput) (Item, error) {
	return s.statusMutation(ctx, in, "in_progress", "started")
}
func (s *Service) Complete(ctx context.Context, in UpdateInput) (Item, error) {
	return s.statusMutation(ctx, in, "completed", "completed")
}

func (s *Service) statusMutation(ctx context.Context, in UpdateInput, status, event string) (Item, error) {
	if in.Admin && len([]rune(strings.TrimSpace(in.Reason))) < 10 {
		return Item{}, ErrInvalidInput
	}
	item, err := s.Get(ctx, in.ActionUUID, in.ActorUserUUID, in.Admin)
	if err != nil {
		return Item{}, err
	}
	if status == "in_progress" && !item.Capabilities.CanStart || status == "completed" && !item.Capabilities.CanComplete {
		return Item{}, ErrForbidden
	}
	if item.Status == "completed" || item.Status == "cancelled" {
		return Item{}, ErrConflict
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Item{}, err
	}
	defer func() { _ = tx.Rollback() }()
	now := s.now().UTC()
	var res sql.Result
	if status == "completed" {
		res, err = tx.ExecContext(ctx, `UPDATE call_actions SET status='completed',completed_at=$1,completed_by_user_uuid=$2,updated_at=$1,lock_version=lock_version+1 WHERE action_uuid=$3 AND lock_version=$4 AND status NOT IN ('completed','cancelled')`, now, in.ActorUserUUID, in.ActionUUID, in.ExpectedVersion)
	} else {
		res, err = tx.ExecContext(ctx, `UPDATE call_actions SET status='in_progress',started_at=COALESCE(started_at,$1),updated_at=$1,lock_version=lock_version+1 WHERE action_uuid=$2 AND lock_version=$3 AND status IN ('open','overdue')`, now, in.ActionUUID, in.ExpectedVersion)
	}
	if err != nil {
		return Item{}, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return Item{}, ErrConflict
	}
	if err = insertEvent(ctx, tx, in.ActionUUID, event, in.ActorUserUUID, in.Reason, map[string]any{"status": item.Status}, map[string]any{"status": status}); err != nil {
		return Item{}, err
	}
	if status == "completed" && in.ActorUserUUID != item.AssigneeUserUUID {
		if err = createNotification(ctx, tx, in.ActionUUID, item.AssigneeUserUUID, "action_completed", "Действие завершено", item.Title, item.LockVersion+1); err != nil {
			return Item{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return Item{}, err
	}
	return s.Get(ctx, in.ActionUUID, in.ActorUserUUID, in.Admin)
}

// Edit changes the wording of an action. Only the person who set it may do
// that: the title and the description are their instruction to somebody else.
func (s *Service) Edit(ctx context.Context, in EditInput) (Item, error) {
	in.Title, in.Description = strings.TrimSpace(in.Title), strings.TrimSpace(in.Description)
	if in.Title == "" || len([]rune(in.Title)) > 200 || len([]rune(in.Description)) > 10000 {
		return Item{}, ErrInvalidInput
	}
	item, err := s.Get(ctx, in.ActionUUID, in.ActorUserUUID, in.Admin)
	if err != nil {
		return Item{}, err
	}
	if !item.Capabilities.CanEditFields {
		return Item{}, ErrForbidden
	}
	if in.Title == item.Title && in.Description == item.Description {
		return item, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Item{}, err
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.ExecContext(ctx, `UPDATE call_actions SET title=$1,description=$2,updated_at=$3,lock_version=lock_version+1 WHERE action_uuid=$4 AND lock_version=$5 AND status NOT IN ('completed','cancelled')`, in.Title, in.Description, s.now().UTC(), in.ActionUUID, in.ExpectedVersion)
	if err != nil {
		return Item{}, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return Item{}, ErrConflict
	}
	if err = insertEvent(ctx, tx, in.ActionUUID, "edited", in.ActorUserUUID, in.Reason, map[string]any{"title": item.Title, "description": item.Description}, map[string]any{"title": in.Title, "description": in.Description}); err != nil {
		return Item{}, err
	}
	if err = tx.Commit(); err != nil {
		return Item{}, err
	}
	return s.Get(ctx, in.ActionUUID, in.ActorUserUUID, in.Admin)
}

// RevertStatus takes back the last status change within an hour of it. A wrong
// click should not need a new action to fix, and an hour later it is history.
func (s *Service) RevertStatus(ctx context.Context, in UpdateInput) (Item, error) {
	item, err := s.Get(ctx, in.ActionUUID, in.ActorUserUUID, in.Admin)
	if err != nil {
		return Item{}, err
	}
	if !item.Capabilities.CanRevertStatus {
		return Item{}, ErrForbidden
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Item{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var eventType string
	var oldData []byte
	var eventAt time.Time
	err = tx.QueryRowContext(ctx, `SELECT event_type,old_data,created_at FROM call_action_events WHERE action_uuid=$1 AND event_type IN ('started','completed','cancelled') ORDER BY created_at DESC,event_uuid DESC LIMIT 1 FOR UPDATE`, in.ActionUUID).Scan(&eventType, &oldData, &eventAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Item{}, ErrConflict
	}
	if err != nil {
		return Item{}, err
	}
	if !s.now().UTC().Before(eventAt.Add(statusRevertWindow)) {
		return Item{}, ErrConflict
	}
	var previous struct {
		Status string `json:"status"`
	}
	if json.Unmarshal(oldData, &previous) != nil || previous.Status == "" {
		return Item{}, ErrConflict
	}

	now := s.now().UTC()
	res, err := tx.ExecContext(ctx, `UPDATE call_actions SET status=$1,completed_at=NULL,completed_by_user_uuid=NULL,cancelled_at=NULL,cancelled_by_user_uuid=NULL,cancel_reason=NULL,started_at=CASE WHEN $1='open' THEN NULL ELSE started_at END,updated_at=$2,lock_version=lock_version+1 WHERE action_uuid=$3 AND lock_version=$4`, previous.Status, now, in.ActionUUID, in.ExpectedVersion)
	if err != nil {
		return Item{}, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return Item{}, ErrConflict
	}
	if err = insertEvent(ctx, tx, in.ActionUUID, "status_reverted", in.ActorUserUUID, in.Reason, map[string]any{"status": item.Status}, map[string]any{"status": previous.Status}); err != nil {
		return Item{}, err
	}
	if in.ActorUserUUID != item.AssigneeUserUUID {
		if err = createNotification(ctx, tx, in.ActionUUID, item.AssigneeUserUUID, "action_status_reverted", "Статус действия возвращён", item.Title, item.LockVersion+1); err != nil {
			return Item{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return Item{}, err
	}
	return s.Get(ctx, in.ActionUUID, in.ActorUserUUID, in.Admin)
}

func (s *Service) Cancel(ctx context.Context, in UpdateInput) (Item, error) {
	in.Reason = strings.TrimSpace(in.Reason)
	if len([]rune(in.Reason)) < 10 || len([]rune(in.Reason)) > 2000 {
		return Item{}, ErrInvalidInput
	}
	item, err := s.Get(ctx, in.ActionUUID, in.ActorUserUUID, in.Admin)
	if err != nil {
		return Item{}, err
	}
	if !item.Capabilities.CanCancel {
		return Item{}, ErrForbidden
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Item{}, err
	}
	defer func() { _ = tx.Rollback() }()
	now := s.now().UTC()
	res, err := tx.ExecContext(ctx, `UPDATE call_actions SET status='cancelled',cancelled_at=$1,cancelled_by_user_uuid=$2,cancel_reason=$3,updated_at=$1,lock_version=lock_version+1 WHERE action_uuid=$4 AND lock_version=$5 AND status NOT IN ('completed','cancelled')`, now, in.ActorUserUUID, in.Reason, in.ActionUUID, in.ExpectedVersion)
	if err != nil {
		return Item{}, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return Item{}, ErrConflict
	}
	_, _ = tx.ExecContext(ctx, `UPDATE call_action_transfer_requests SET status='withdrawn',resolved_at=$2 WHERE action_uuid=$1 AND status='pending'`, in.ActionUUID, now)
	if err = insertEvent(ctx, tx, in.ActionUUID, "cancelled", in.ActorUserUUID, in.Reason, map[string]any{"status": item.Status}, map[string]any{"status": "cancelled"}); err != nil {
		return Item{}, err
	}
	if in.ActorUserUUID != item.AssigneeUserUUID {
		if err = createNotification(ctx, tx, in.ActionUUID, item.AssigneeUserUUID, "action_cancelled", "Действие отменено", item.Title, item.LockVersion+1); err != nil {
			return Item{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return Item{}, err
	}
	return s.Get(ctx, in.ActionUUID, in.ActorUserUUID, in.Admin)
}

func (s *Service) Reschedule(ctx context.Context, in RescheduleInput) (Item, error) {
	in.Reason = strings.TrimSpace(in.Reason)
	minimumReasonLength := 3
	if in.Admin {
		minimumReasonLength = 10
	}
	if len([]rune(in.Reason)) < minimumReasonLength || !in.DueAt.After(s.now()) {
		return Item{}, ErrInvalidInput
	}
	item, err := s.Get(ctx, in.ActionUUID, in.ActorUserUUID, in.Admin)
	if err != nil {
		return Item{}, err
	}
	if !item.Capabilities.CanReschedule {
		return Item{}, ErrForbidden
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Item{}, err
	}
	defer func() { _ = tx.Rollback() }()
	now := s.now().UTC()
	res, err := tx.ExecContext(ctx, `UPDATE call_actions SET due_at=$1,grace_expires_at=$2,status=CASE WHEN status='overdue' THEN 'open' ELSE status END,updated_at=$3,lock_version=lock_version+1,schedule_version=schedule_version+1 WHERE action_uuid=$4 AND lock_version=$5 AND status NOT IN ('completed','cancelled')`, in.DueAt.UTC(), in.DueAt.UTC().Add(24*time.Hour), now, in.ActionUUID, in.ExpectedVersion)
	if err != nil {
		return Item{}, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return Item{}, ErrConflict
	}
	if err = insertEvent(ctx, tx, in.ActionUUID, "rescheduled", in.ActorUserUUID, in.Reason, map[string]any{"due_at": item.DueAt}, map[string]any{"due_at": in.DueAt}); err != nil {
		return Item{}, err
	}
	if in.ActorUserUUID != item.AssigneeUserUUID {
		if err = createNotification(ctx, tx, in.ActionUUID, item.AssigneeUserUUID, "action_due_changed", "Срок действия изменён", item.Title, item.LockVersion+1); err != nil {
			return Item{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return Item{}, err
	}
	return s.Get(ctx, in.ActionUUID, in.ActorUserUUID, in.Admin)
}

func (s *Service) Reassign(ctx context.Context, in ReassignInput) (Item, error) {
	in.Reason = strings.TrimSpace(in.Reason)
	if len([]rune(in.Reason)) < 10 {
		return Item{}, ErrInvalidInput
	}
	item, err := s.Get(ctx, in.ActionUUID, in.ActorUserUUID, in.Admin)
	if err != nil {
		return Item{}, err
	}
	if !item.Capabilities.CanReassign {
		return Item{}, ErrForbidden
	}
	if item.CompanyUUID == nil || item.TargetDepartmentUUID == nil {
		return Item{}, ErrForbidden
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Item{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if ok, checkErr := validAssignment(ctx, tx, *item.CompanyUUID, in.TargetDepartmentUUID, in.AssigneeUserUUID); checkErr != nil {
		return Item{}, checkErr
	} else if !ok {
		return Item{}, ErrInvalidInput
	}
	now := s.now().UTC()
	res, err := tx.ExecContext(ctx, `UPDATE call_actions SET assignee_user_uuid=$1,target_department_uuid=$2,assignment_state='valid',updated_at=$3,lock_version=lock_version+1,schedule_version=schedule_version+1 WHERE action_uuid=$4 AND lock_version=$5 AND status NOT IN ('completed','cancelled')`, in.AssigneeUserUUID, in.TargetDepartmentUUID, now, in.ActionUUID, in.ExpectedVersion)
	if err != nil {
		return Item{}, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return Item{}, ErrConflict
	}
	_, _ = tx.ExecContext(ctx, `UPDATE call_action_transfer_requests SET status='withdrawn',resolved_at=$2 WHERE action_uuid=$1 AND status='pending'`, in.ActionUUID, now)
	if err = insertEvent(ctx, tx, in.ActionUUID, "reassigned", in.ActorUserUUID, in.Reason, map[string]any{"assignee": item.AssigneeUserUUID}, map[string]any{"assignee": in.AssigneeUserUUID}); err != nil {
		return Item{}, err
	}
	if err = createNotification(ctx, tx, in.ActionUUID, item.AssigneeUserUUID, "action_reassigned", "Ответственность передана", item.Title, item.LockVersion+1); err != nil {
		return Item{}, err
	}
	if in.AssigneeUserUUID != item.AssigneeUserUUID {
		if err = createNotification(ctx, tx, in.ActionUUID, in.AssigneeUserUUID, "action_assigned", "Вам назначено действие", item.Title, item.LockVersion+1); err != nil {
			return Item{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return Item{}, err
	}
	return s.Get(ctx, in.ActionUUID, in.ActorUserUUID, in.Admin)
}

func (s *Service) CreateTransfer(ctx context.Context, in TransferInput) (TransferRequest, error) {
	in.Reason = strings.TrimSpace(in.Reason)
	if len([]rune(in.Reason)) < 10 || len([]rune(in.Reason)) > 2000 {
		return TransferRequest{}, ErrInvalidInput
	}
	item, err := s.Get(ctx, in.ActionUUID, in.ActorUserUUID, false)
	if err != nil {
		return TransferRequest{}, err
	}
	if !item.Capabilities.CanRequestTransfer {
		return TransferRequest{}, ErrForbidden
	}
	if item.CompanyUUID == nil || item.TargetDepartmentUUID == nil {
		return TransferRequest{}, ErrForbidden
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return TransferRequest{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if in.ProposedAssignee.Valid {
		if !in.ProposedDepartment.Valid {
			return TransferRequest{}, ErrInvalidInput
		}
		ok, checkErr := validAssignment(ctx, tx, *item.CompanyUUID, in.ProposedDepartment.UUID, in.ProposedAssignee.UUID)
		if checkErr != nil {
			return TransferRequest{}, checkErr
		}
		if !ok {
			return TransferRequest{}, ErrInvalidInput
		}
	}
	id := uuid.New()
	_, err = tx.ExecContext(ctx, `INSERT INTO call_action_transfer_requests(request_uuid,action_uuid,requested_by_user_uuid,proposed_assignee_user_uuid,proposed_department_uuid,reason,action_lock_version_at_create) VALUES($1,$2,$3,$4,$5,$6,$7)`, id, in.ActionUUID, in.ActorUserUUID, nullableArg(in.ProposedAssignee), nullableArg(in.ProposedDepartment), in.Reason, item.LockVersion)
	if err != nil {
		if strings.Contains(err.Error(), "uq_call_action_transfer_pending") {
			return TransferRequest{}, ErrConflict
		}
		return TransferRequest{}, err
	}
	if err = insertEvent(ctx, tx, in.ActionUUID, "transfer_requested", in.ActorUserUUID, in.Reason, nil, map[string]any{"request_uuid": id}); err != nil {
		return TransferRequest{}, err
	}
	recipients, err := leadersAndManagers(ctx, tx, *item.CompanyUUID, *item.TargetDepartmentUUID)
	if err != nil {
		return TransferRequest{}, err
	}
	for _, r := range recipients {
		if r != in.ActorUserUUID {
			if err = createNotification(ctx, tx, in.ActionUUID, r, "action_transfer_requested", "Запрос на передачу действия", item.Title, item.LockVersion); err != nil {
				return TransferRequest{}, err
			}
		}
	}
	if err = tx.Commit(); err != nil {
		return TransferRequest{}, err
	}
	return s.getTransfer(ctx, id)
}

func nullableArg(v uuid.NullUUID) any {
	if v.Valid {
		return v.UUID
	}
	return nil
}
func leadersAndManagers(ctx context.Context, tx *sql.Tx, company, department uuid.UUID) ([]uuid.UUID, error) {
	rows, err := tx.QueryContext(ctx, `SELECT user_uuid FROM company_members WHERE company_uuid=$1 AND status='active' AND role IN ('company_manager','company_deputy') UNION SELECT user_uuid FROM department_members WHERE department_uuid=$2 AND status='active' AND role='department_leader'`, company, department)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (s *Service) getTransfer(ctx context.Context, id uuid.UUID) (TransferRequest, error) {
	var r TransferRequest
	var assignee, dept uuid.NullUUID
	var comment sql.NullString
	var resolved sql.NullTime
	err := s.db.QueryRowContext(ctx, `SELECT request_uuid,action_uuid,requested_by_user_uuid,proposed_assignee_user_uuid,proposed_department_uuid,reason,status,resolution_comment,created_at,resolved_at FROM call_action_transfer_requests WHERE request_uuid=$1`, id).Scan(&r.ID, &r.ActionUUID, &r.RequestedBy, &assignee, &dept, &r.Reason, &r.Status, &comment, &r.CreatedAt, &resolved)
	r.ProposedAssignee = nullableUUID(assignee)
	r.ProposedDepartment = nullableUUID(dept)
	r.ResolutionComment = ptrString(comment)
	r.ResolvedAt = ptrTime(resolved)
	return r, err
}

func (s *Service) ResolveTransfer(ctx context.Context, in ResolveTransferInput) (Item, error) {
	item, err := s.Get(ctx, in.ActionUUID, in.ActorUserUUID, false)
	if err != nil {
		return Item{}, err
	}
	if !item.Capabilities.CanResolveTransfer {
		return Item{}, ErrForbidden
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Item{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var requester uuid.UUID
	var assignee, dept uuid.NullUUID
	var requestStatus string
	err = tx.QueryRowContext(ctx, `SELECT requested_by_user_uuid,proposed_assignee_user_uuid,proposed_department_uuid,status FROM call_action_transfer_requests WHERE request_uuid=$1 AND action_uuid=$2 FOR UPDATE`, in.RequestUUID, in.ActionUUID).Scan(&requester, &assignee, &dept, &requestStatus)
	if err == sql.ErrNoRows {
		return Item{}, ErrNotFound
	}
	if err != nil {
		return Item{}, err
	}
	if requestStatus != "pending" {
		return Item{}, ErrConflict
	}
	now := s.now().UTC()
	nextStatus, event := "rejected", "transfer_rejected"
	if in.Approve {
		if !assignee.Valid || !dept.Valid {
			return Item{}, ErrInvalidInput
		}
		if item.CompanyUUID == nil {
			return Item{}, ErrForbidden
		}
		ok, checkErr := validAssignment(ctx, tx, *item.CompanyUUID, dept.UUID, assignee.UUID)
		if checkErr != nil {
			return Item{}, checkErr
		}
		if !ok {
			return Item{}, ErrInvalidInput
		}
		res, updateErr := tx.ExecContext(ctx, `UPDATE call_actions SET assignee_user_uuid=$1,target_department_uuid=$2,updated_at=$3,lock_version=lock_version+1,schedule_version=schedule_version+1 WHERE action_uuid=$4 AND lock_version=$5 AND status NOT IN ('completed','cancelled')`, assignee.UUID, dept.UUID, now, in.ActionUUID, in.ExpectedVersion)
		if updateErr != nil {
			return Item{}, updateErr
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return Item{}, ErrConflict
		}
		nextStatus, event = "approved", "transfer_approved"
	}
	_, err = tx.ExecContext(ctx, `UPDATE call_action_transfer_requests SET status=$1,resolved_by_user_uuid=$2,resolution_comment=NULLIF($3,''),resolved_at=$4 WHERE request_uuid=$5`, nextStatus, in.ActorUserUUID, strings.TrimSpace(in.Comment), now, in.RequestUUID)
	if err != nil {
		return Item{}, err
	}
	if err = insertEvent(ctx, tx, in.ActionUUID, event, in.ActorUserUUID, in.Comment, nil, map[string]any{"request_uuid": in.RequestUUID}); err != nil {
		return Item{}, err
	}
	kind, title := "action_transfer_rejected", "Передача отклонена"
	if in.Approve {
		kind, title = "action_transfer_approved", "Передача одобрена"
	}
	if err = createNotification(ctx, tx, in.ActionUUID, requester, kind, title, item.Title, item.LockVersion+1); err != nil {
		return Item{}, err
	}
	if in.Approve {
		_ = createNotification(ctx, tx, in.ActionUUID, item.AssigneeUserUUID, "action_reassigned", "Ответственность передана", item.Title, item.LockVersion+1)
		_ = createNotification(ctx, tx, in.ActionUUID, assignee.UUID, "action_assigned", "Вам назначено действие", item.Title, item.LockVersion+1)
	}
	if err = tx.Commit(); err != nil {
		return Item{}, err
	}
	return s.Get(ctx, in.ActionUUID, in.ActorUserUUID, false)
}

var _ = time.Hour
