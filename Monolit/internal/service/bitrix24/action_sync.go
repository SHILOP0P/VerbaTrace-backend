package bitrix24

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func (s *Service) PreviewActionSync(ctx context.Context, actionID, actor uuid.UUID) (models.ActionExternalSyncPreview, error) {
	var item models.ActionExternalSyncPreview
	var companyID uuid.UUID
	err := s.db.QueryRowContext(ctx, `SELECT a.action_uuid,a.title,a.due_at,a.assignee_user_uuid,a.company_uuid
		FROM call_actions a
		WHERE a.action_uuid=$1 AND a.company_uuid IS NOT NULL AND a.status IN ('open','in_progress','overdue')
		AND (a.created_by_user_uuid=$2 OR a.assignee_user_uuid=$2)`, actionID, actor).
		Scan(&item.ActionID, &item.Title, &item.DueAt, &item.AssigneeUserID, &companyID)
	if errors.Is(err, sql.ErrNoRows) {
		return item, ErrNotFound
	}
	if err != nil {
		return item, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT c.connection_uuid,c.name,COALESCE(c.settings->>'portal_domain_display',''),m.external_user_id
		FROM integration_connections c
		LEFT JOIN integration_external_user_mappings m ON m.connection_uuid=c.connection_uuid AND m.internal_user_uuid=$2 AND m.status='mapped'
		WHERE c.company_uuid=$1 AND c.provider='bitrix24' AND c.status='active'
		ORDER BY lower(c.name),c.connection_uuid`, companyID, item.AssigneeUserID)
	if err != nil {
		return item, err
	}
	defer func() { _ = rows.Close() }()
	item.Options = make([]models.ActionExternalConnectionOption, 0)
	for rows.Next() {
		var option models.ActionExternalConnectionOption
		var externalID sql.NullString
		if err = rows.Scan(&option.ConnectionID, &option.ConnectionName, &option.PortalDomain, &externalID); err != nil {
			return item, err
		}
		if externalID.Valid && strings.TrimSpace(externalID.String) != "" {
			value := externalID.String
			option.ExternalAssigneeID = &value
			option.Available = true
		} else {
			option.UnavailableReason = "assignee_not_mapped"
		}
		item.Options = append(item.Options, option)
	}
	if err = rows.Err(); err != nil {
		return item, err
	}
	return item, nil
}

func (s *Service) CreateActionSync(ctx context.Context, actionID, connectionID, actor uuid.UUID, requestKey string) (models.ActionExternalSync, bool, error) {
	if len(strings.TrimSpace(requestKey)) < 8 {
		return models.ActionExternalSync{}, false, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return models.ActionExternalSync{}, false, err
	}
	defer func() { _ = tx.Rollback() }()
	var title, description string
	var dueAt time.Time
	var assignee, creator, company, targetDepartment uuid.UUID
	var actionVersion int64
	err = tx.QueryRowContext(ctx, `SELECT a.title,a.description,a.due_at,a.assignee_user_uuid,a.created_by_user_uuid,a.company_uuid,a.target_department_uuid,a.lock_version
		FROM call_actions a WHERE a.action_uuid=$1 AND a.status IN ('open','in_progress','overdue') AND (a.created_by_user_uuid=$2 OR a.assignee_user_uuid=$2) FOR UPDATE`, actionID, actor).
		Scan(&title, &description, &dueAt, &assignee, &creator, &company, &targetDepartment, &actionVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return models.ActionExternalSync{}, false, ErrNotFound
	}
	if err != nil {
		return models.ActionExternalSync{}, false, err
	}
	var externalUserID, connectionStatus string
	err = tx.QueryRowContext(ctx, `SELECT m.external_user_id,c.status FROM integration_connections c JOIN integration_external_user_mappings m ON m.connection_uuid=c.connection_uuid AND m.internal_user_uuid=$3 AND m.status='mapped'
		WHERE c.connection_uuid=$1 AND c.company_uuid=$2 AND c.provider='bitrix24' AND c.status='active'`, connectionID, company, assignee).Scan(&externalUserID, &connectionStatus)
	if errors.Is(err, sql.ErrNoRows) {
		return models.ActionExternalSync{}, false, ErrConflict
	}
	if err != nil {
		return models.ActionExternalSync{}, false, err
	}
	var existing models.ActionExternalSync
	existing, err = scanActionSync(tx.QueryRowContext(ctx, actionSyncSelect+` WHERE action_uuid=$1 AND connection_uuid=$2 AND operation='create_task' AND state NOT IN ('rejected','cancelled','unlinked')`, actionID, connectionID))
	if err == nil {
		return existing, false, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return models.ActionExternalSync{}, false, err
	}
	id := uuid.New()
	marker := "[VT:" + id.String() + "]"
	safeDescription := strings.TrimSpace(description)
	if len(safeDescription) > 1600 {
		safeDescription = safeDescription[:1600]
	}
	link := strings.TrimRight(s.config.PublicBaseURL, "/") + "/actions/" + actionID.String()
	if s.config.PublicBaseURL == "" {
		link = "/actions/" + actionID.String()
	}
	payload := map[string]any{"fields": map[string]any{"TITLE": title, "DESCRIPTION": safeDescription + "\n\nVerbaTrace: " + link + "\n" + marker, "RESPONSIBLE_ID": externalUserID, "DEADLINE": dueAt.Format(time.RFC3339)}}
	payloadJSON, _ := json.Marshal(payload)
	hash := sha256.Sum256(payloadJSON)
	now := s.now().UTC()
	item := models.ActionExternalSync{ID: id, ActionID: actionID, ConnectionID: connectionID, Provider: "bitrix24", RequesterUserID: actor, State: "pending_approval", IdempotencyMarker: marker, RequestPayload: payloadJSON, ActionLockVersion: actionVersion, LockVersion: 1, CreatedAt: now, UpdatedAt: now}
	_, err = tx.ExecContext(ctx, `INSERT INTO call_action_external_syncs(sync_uuid,action_uuid,connection_uuid,provider,requester_user_uuid,state,idempotency_marker,request_payload,request_payload_hash,action_lock_version,created_at,updated_at)
		VALUES($1,$2,$3,'bitrix24',$4,'pending_approval',$5,$6,$7,$8,$9,$9)`, id, actionID, connectionID, actor, marker, payloadJSON, hash[:], actionVersion, now)
	if err != nil {
		return models.ActionExternalSync{}, false, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT recipient FROM (
		SELECT manager_user_uuid recipient FROM companies WHERE company_uuid=$1
		UNION SELECT user_uuid FROM company_members WHERE company_uuid=$1 AND role='company_manager' AND status='active'
		UNION SELECT user_uuid FROM department_members WHERE department_uuid=$2 AND role='department_leader' AND status='active'
	) recipients WHERE recipient<>$3`, company, targetDepartment, actor)
	if err != nil {
		return models.ActionExternalSync{}, false, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var recipient uuid.UUID
		if err = rows.Scan(&recipient); err != nil {
			return models.ActionExternalSync{}, false, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO notifications(notification_uuid,user_uuid,type,title,body,entity_type,entity_uuid) VALUES($1,$2,'action_external_sync_requested','Задача ожидает отправки в Bitrix24',$3,'action_external_sync',$4)`, uuid.New(), recipient, title, id); err != nil {
			return models.ActionExternalSync{}, false, err
		}
	}
	if err = rows.Err(); err != nil {
		return models.ActionExternalSync{}, false, err
	}
	if err = tx.Commit(); err != nil {
		return models.ActionExternalSync{}, false, err
	}
	return item, true, nil
}

func (s *Service) ApproveActionSync(ctx context.Context, id, actor uuid.UUID, expectedVersion int64) (models.ActionExternalSync, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return models.ActionExternalSync{}, err
	}
	defer func() { _ = tx.Rollback() }()
	item, err := scanActionSync(tx.QueryRowContext(ctx, actionSyncSelect+` WHERE sync_uuid=$1 FOR UPDATE`, id))
	if err != nil {
		return item, err
	}
	if item.RequesterUserID == actor {
		return item, ErrForbidden
	}
	if item.State != "pending_approval" || item.LockVersion != expectedVersion {
		return item, ErrConflict
	}
	var allowed bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM call_actions a WHERE a.action_uuid=$1 AND (
		EXISTS(SELECT 1 FROM companies c WHERE c.company_uuid=a.company_uuid AND c.manager_user_uuid=$2) OR
		EXISTS(SELECT 1 FROM company_members cm WHERE cm.company_uuid=a.company_uuid AND cm.user_uuid=$2 AND cm.status='active' AND cm.role='company_manager') OR
		EXISTS(SELECT 1 FROM department_members dm WHERE dm.department_uuid=a.target_department_uuid AND dm.user_uuid=$2 AND dm.status='active' AND dm.role='department_leader')))`, item.ActionID, actor).Scan(&allowed)
	if err != nil || !allowed {
		return item, ErrForbidden
	}
	now := s.now().UTC()
	_, err = tx.ExecContext(ctx, `UPDATE call_action_external_syncs SET state='queued',approver_user_uuid=$2,approved_at=$3,available_at=$3,lock_version=lock_version+1,updated_at=$3 WHERE sync_uuid=$1`, id, actor, now)
	if err != nil {
		return item, err
	}
	if err = tx.Commit(); err != nil {
		return item, err
	}
	item.State = "queued"
	item.ApproverUserID = uuid.NullUUID{UUID: actor, Valid: true}
	item.ApprovedAt = &now
	item.LockVersion++
	item.UpdatedAt = now
	return item, nil
}

func (s *Service) RejectActionSync(ctx context.Context, id, actor uuid.UUID, expectedVersion int64, comment string) error {
	comment = strings.TrimSpace(comment)
	if len(comment) < 3 {
		return ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	item, err := scanActionSync(tx.QueryRowContext(ctx, actionSyncSelect+` WHERE sync_uuid=$1 FOR UPDATE`, id))
	if err != nil {
		return err
	}
	if item.RequesterUserID == actor {
		return ErrForbidden
	}
	if item.State != "pending_approval" || item.LockVersion != expectedVersion {
		return ErrConflict
	}
	var allowed bool
	_ = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM call_actions a WHERE a.action_uuid=$1 AND (EXISTS(SELECT 1 FROM companies c WHERE c.company_uuid=a.company_uuid AND c.manager_user_uuid=$2) OR EXISTS(SELECT 1 FROM company_members cm WHERE cm.company_uuid=a.company_uuid AND cm.user_uuid=$2 AND cm.status='active' AND cm.role='company_manager') OR EXISTS(SELECT 1 FROM department_members dm WHERE dm.department_uuid=a.target_department_uuid AND dm.user_uuid=$2 AND dm.status='active' AND dm.role='department_leader')))`, item.ActionID, actor).Scan(&allowed)
	if !allowed {
		return ErrForbidden
	}
	now := s.now().UTC()
	_, err = tx.ExecContext(ctx, `UPDATE call_action_external_syncs SET state='rejected',approver_user_uuid=$2,rejected_at=$3,last_error_code='rejected_by_approver',lock_version=lock_version+1,updated_at=$3 WHERE sync_uuid=$1`, id, actor, now)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO notifications(notification_uuid,user_uuid,type,title,body,entity_type,entity_uuid) VALUES($1,$2,'action_external_sync_decided','Отправка в Bitrix24 отклонена',$3,'action_external_sync',$4)`, uuid.New(), item.RequesterUserID, comment, id)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) GetActionSync(ctx context.Context, actionID, actor uuid.UUID) (models.ActionExternalSync, error) {
	item, err := scanActionSync(s.db.QueryRowContext(ctx, actionSyncSelect+` s JOIN call_actions a ON a.action_uuid=s.action_uuid WHERE s.action_uuid=$1 AND (a.created_by_user_uuid=$2 OR a.assignee_user_uuid=$2 OR s.approver_user_uuid=$2 OR EXISTS(SELECT 1 FROM companies c WHERE c.company_uuid=a.company_uuid AND c.manager_user_uuid=$2) OR EXISTS(SELECT 1 FROM company_members cm WHERE cm.company_uuid=a.company_uuid AND cm.user_uuid=$2 AND cm.status='active' AND cm.role='company_manager') OR EXISTS(SELECT 1 FROM department_members dm WHERE dm.department_uuid=a.target_department_uuid AND dm.user_uuid=$2 AND dm.status='active' AND dm.role='department_leader')) ORDER BY s.created_at DESC LIMIT 1`, actionID, actor))
	if err != nil {
		return item, err
	}
	if item.State == "pending_approval" && item.RequesterUserID != actor {
		var allowed bool
		allowed, err = s.canResolveActionSync(ctx, item.ActionID, actor)
		if err != nil {
			return item, err
		}
		item.CanApprove = allowed
		item.CanReject = allowed
	}
	if item.State == "needs_review" || item.ReviewState != nil {
		item.CanResolve, err = s.canResolveActionSync(ctx, item.ActionID, actor)
		if err != nil {
			return item, err
		}
	}
	return item, nil
}

func (s *Service) canResolveActionSync(ctx context.Context, actionID, actor uuid.UUID) (bool, error) {
	var allowed bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM call_actions a WHERE a.action_uuid=$1 AND (
		EXISTS(SELECT 1 FROM companies c WHERE c.company_uuid=a.company_uuid AND c.manager_user_uuid=$2) OR
		EXISTS(SELECT 1 FROM company_members cm WHERE cm.company_uuid=a.company_uuid AND cm.user_uuid=$2 AND cm.status='active' AND cm.role='company_manager') OR
		EXISTS(SELECT 1 FROM department_members dm WHERE dm.department_uuid=a.target_department_uuid AND dm.user_uuid=$2 AND dm.status='active' AND dm.role='department_leader')))`, actionID, actor).Scan(&allowed)
	return allowed, err
}

func (s *Service) GetActionSyncByID(ctx context.Context, syncID, actor uuid.UUID) (models.ActionExternalSync, error) {
	var actionID uuid.UUID
	err := s.db.QueryRowContext(ctx, `SELECT action_uuid FROM call_action_external_syncs WHERE sync_uuid=$1`, syncID).Scan(&actionID)
	if errors.Is(err, sql.ErrNoRows) {
		return models.ActionExternalSync{}, ErrNotFound
	}
	if err != nil {
		return models.ActionExternalSync{}, err
	}
	item, err := s.GetActionSync(ctx, actionID, actor)
	if err != nil {
		return item, err
	}
	if item.ID != syncID {
		item, err = scanActionSync(s.db.QueryRowContext(ctx, actionSyncSelect+` s JOIN call_actions a ON a.action_uuid=s.action_uuid WHERE s.sync_uuid=$1 AND (a.created_by_user_uuid=$2 OR a.assignee_user_uuid=$2 OR s.approver_user_uuid=$2 OR EXISTS(SELECT 1 FROM companies c WHERE c.company_uuid=a.company_uuid AND c.manager_user_uuid=$2) OR EXISTS(SELECT 1 FROM company_members cm WHERE cm.company_uuid=a.company_uuid AND cm.user_uuid=$2 AND cm.status='active' AND cm.role='company_manager') OR EXISTS(SELECT 1 FROM department_members dm WHERE dm.department_uuid=a.target_department_uuid AND dm.user_uuid=$2 AND dm.status='active' AND dm.role='department_leader'))`, syncID, actor))
		if err == nil && (item.State == "needs_review" || item.ReviewState != nil) {
			item.CanResolve, err = s.canResolveActionSync(ctx, item.ActionID, actor)
		}
	}
	return item, err
}

func (s *Service) RunActionSyncWorker(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.recoverActionSyncLeases(ctx)
				s.processOneActionSync(ctx)
				s.escalateAmbiguousActionSync(ctx)
				s.reconcileOneSyncedActionTask(ctx)
			}
		}
	}()
	return done
}

func (s *Service) processOneActionSync(ctx context.Context) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return
	}
	defer func() { _ = tx.Rollback() }()
	item, err := scanActionSync(tx.QueryRowContext(ctx, actionSyncSelect+` WHERE state='queued' AND available_at<=now() ORDER BY created_at FOR UPDATE SKIP LOCKED LIMIT 1`))
	if err != nil {
		return
	}
	lease := s.now().UTC().Add(45 * time.Second)
	_, err = tx.ExecContext(ctx, `UPDATE call_action_external_syncs SET state='sending',lease_until=$2,attempts=attempts+1,lock_version=lock_version+1,updated_at=now() WHERE sync_uuid=$1`, item.ID, lease)
	if err != nil {
		return
	}
	if tx.Commit() != nil {
		return
	}
	var actionStillCurrent bool
	if err = s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM call_actions a JOIN integration_connections c ON c.connection_uuid=$2 AND c.status='active' JOIN integration_external_user_mappings m ON m.connection_uuid=c.connection_uuid AND m.internal_user_uuid=a.assignee_user_uuid AND m.status='mapped' WHERE a.action_uuid=$1 AND a.lock_version=$3 AND a.status IN ('open','in_progress','overdue'))`, item.ActionID, item.ConnectionID, item.ActionLockVersion).Scan(&actionStillCurrent); err != nil || !actionStillCurrent {
		_, _ = s.db.ExecContext(ctx, `UPDATE call_action_external_syncs SET state='needs_review',last_error_code='action_changed_before_send',lease_until=NULL,lock_version=lock_version+1,updated_at=now() WHERE sync_uuid=$1 AND state='sending'`, item.ID)
		return
	}
	info, token, err := s.systemConnectionToken(ctx, item.ConnectionID)
	if err != nil {
		s.rescheduleActionSync(ctx, item.ID, "oauth_unavailable")
		return
	}
	var result any
	err = s.call(ctx, info.Domain, "tasks.task.add", token, mustObject(item.RequestPayload), &result)
	if err != nil {
		var netErr net.Error
		if errors.As(err, &netErr) {
			s.failActionSync(ctx, item.ID, "ambiguous_provider_outcome", true)
		} else {
			s.failActionSync(ctx, item.ID, "bitrix_task_create_failed", false)
		}
		return
	}
	taskID, taskURL := extractTaskIdentity(result)
	if taskID == "" {
		s.failActionSync(ctx, item.ID, "invalid_provider_response", true)
		return
	}
	now := s.now().UTC()
	_, _ = s.db.ExecContext(ctx, `UPDATE call_action_external_syncs SET state='synced',external_task_id=$2,external_task_url=NULLIF($3,''),synced_at=$4,lease_until=NULL,last_error_code=NULL,available_at=$4+interval '1 minute',lock_version=lock_version+1,updated_at=$4 WHERE sync_uuid=$1 AND state='sending'`, item.ID, taskID, taskURL, now)
	_, _ = s.db.ExecContext(ctx, `INSERT INTO notifications(notification_uuid,user_uuid,type,title,body,entity_type,entity_uuid) VALUES($1,$2,'action_external_sync_decided','Задача создана в Bitrix24',$3,'action_external_sync',$4)`, uuid.New(), item.RequesterUserID, "Bitrix24 task #"+taskID, item.ID)
}

func (s *Service) recoverActionSyncLeases(ctx context.Context) {
	_, _ = s.db.ExecContext(ctx, `UPDATE call_action_external_syncs SET state='reconciling',last_error_code='worker_lease_expired_after_send',lease_until=NULL,available_at=now()+interval '5 minutes',lock_version=lock_version+1,updated_at=now() WHERE state='sending' AND lease_until<now()`)
}

func (s *Service) escalateAmbiguousActionSync(ctx context.Context) {
	_, _ = s.db.ExecContext(ctx, `UPDATE call_action_external_syncs SET state='needs_review',lock_version=lock_version+1,updated_at=now() WHERE sync_uuid=(SELECT sync_uuid FROM call_action_external_syncs WHERE state='reconciling' AND available_at<=now() ORDER BY available_at,created_at LIMIT 1)`)
}

func (s *Service) rescheduleActionSync(ctx context.Context, id uuid.UUID, code string) {
	_, _ = s.db.ExecContext(ctx, `UPDATE call_action_external_syncs SET state=CASE WHEN attempts<max_attempts THEN 'queued' ELSE 'failed' END,last_error_code=$2,lease_until=NULL,available_at=now()+interval '1 minute',lock_version=lock_version+1,updated_at=now() WHERE sync_uuid=$1 AND state='sending'`, id, code)
}

func (s *Service) failActionSync(ctx context.Context, id uuid.UUID, code string, ambiguous bool) {
	state := "failed"
	if ambiguous {
		state = "reconciling"
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE call_action_external_syncs SET state=$2,last_error_code=$3,lease_until=NULL,available_at=now()+interval '5 minutes',lock_version=lock_version+1,updated_at=now() WHERE sync_uuid=$1 AND state='sending'`, id, state, code)
}

func (s *Service) systemConnectionToken(ctx context.Context, id uuid.UUID) (connectionInfo, string, error) {
	var settingsJSON string
	var ciphertext, nonce []byte
	var expires time.Time
	// A freshly authorized connection is "testing" until TestConnection performs
	// the first provider check. A previously checked connection can be "degraded"
	// and must still be testable again after its permissions are corrected.
	err := s.db.QueryRowContext(ctx, `SELECT c.settings::text,oc.access_token_ciphertext,oc.access_token_nonce,oc.expires_at FROM integration_connections c JOIN integration_oauth_credentials oc USING(connection_uuid) WHERE c.connection_uuid=$1 AND c.provider='bitrix24' AND c.status IN ('active','testing','degraded') AND oc.refresh_state='ready'`, id).Scan(&settingsJSON, &ciphertext, &nonce, &expires)
	if err != nil {
		return connectionInfo{}, "", err
	}
	if !expires.After(s.now().UTC().Add(30 * time.Second)) {
		if err = s.refreshConnectionToken(ctx, id); err != nil {
			return connectionInfo{}, "", err
		}
		return s.systemConnectionToken(ctx, id)
	}
	plain, err := s.cipher.Decrypt(append(append([]byte{}, nonce...), ciphertext...), "integration_oauth_credentials/"+id.String()+"/access")
	if err != nil {
		return connectionInfo{}, "", err
	}
	var settings map[string]any
	_ = json.Unmarshal([]byte(settingsJSON), &settings)
	return connectionInfo{Domain: stringValue(settings["portal_domain_display"]), Scopes: stringSlice(settings["oauth_scope"])}, string(plain), nil
}

const actionSyncSelect = `SELECT sync_uuid,action_uuid,connection_uuid,provider,requester_user_uuid,approver_user_uuid,state,external_task_id,external_task_url,idempotency_marker,request_payload,action_lock_version,attempts,last_error_code,lock_version,created_at,approved_at,rejected_at,synced_at,external_snapshot,review_state,review_reason,last_checked_at,reviewed_by_user_uuid,reviewed_at,updated_at FROM call_action_external_syncs`

func scanActionSync(row rowScanner) (models.ActionExternalSync, error) {
	var item models.ActionExternalSync
	err := row.Scan(&item.ID, &item.ActionID, &item.ConnectionID, &item.Provider, &item.RequesterUserID, &item.ApproverUserID, &item.State, &item.ExternalTaskID, &item.ExternalTaskURL, &item.IdempotencyMarker, &item.RequestPayload, &item.ActionLockVersion, &item.Attempts, &item.LastErrorCode, &item.LockVersion, &item.CreatedAt, &item.ApprovedAt, &item.RejectedAt, &item.SyncedAt, &item.ExternalSnapshot, &item.ReviewState, &item.ReviewReason, &item.LastCheckedAt, &item.ReviewedByUserID, &item.ReviewedAt, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return item, ErrNotFound
	}
	return item, err
}

type rowScanner interface{ Scan(...any) error }

func mustObject(raw json.RawMessage) map[string]any {
	var value map[string]any
	_ = json.Unmarshal(raw, &value)
	return value
}
func extractTaskID(value any) string {
	id, _ := extractTaskIdentity(value)
	return id
}

func extractTaskIdentity(value any) (string, string) {
	switch typed := value.(type) {
	case float64:
		return fmt.Sprintf("%.0f", typed), ""
	case string:
		return typed, ""
	case map[string]any:
		if task, ok := typed["task"].(map[string]any); ok {
			return stringValue(task["id"]), stringValue(task["link"])
		}
		if task, ok := typed["item"].(map[string]any); ok {
			return stringValue(task["id"]), stringValue(task["link"])
		}
		return stringValue(typed["id"]), stringValue(typed["link"])
	default:
		return "", ""
	}
}
