package integration

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func (r *Repository) ClaimIngest(ctx context.Context, worker string, lease time.Duration) (models.ClaimedIngestItem, error) {
	if lease <= 0 {
		lease = 10 * time.Minute
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return models.ClaimedIngestItem{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var id uuid.UUID
	err = tx.QueryRowContext(ctx, `SELECT i.ingest_item_uuid FROM ingest_items i JOIN integration_connections c USING(connection_uuid) WHERE i.attempts<i.max_attempts AND i.available_at<=now() AND (i.status='received' OR (i.status='processing' AND i.lease_expires_at<now()) OR (i.status='blocked' AND c.status='active')) AND (c.status='active' OR (c.status='disabled' AND c.disable_policy='continue' AND i.created_at<c.disabled_at)) ORDER BY i.available_at,i.created_at,i.ingest_item_uuid FOR UPDATE SKIP LOCKED LIMIT 1`).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return models.ClaimedIngestItem{}, models.ErrIngestNotFound
	}
	if err != nil {
		return models.ClaimedIngestItem{}, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE ingest_items SET status='processing',stage='fetching',attempts=attempts+1,locked_by=$2,locked_at=now(),lease_expires_at=now()+make_interval(secs=>$3),lock_version=lock_version+1,updated_at=now(),error_code=NULL,error_message_safe=NULL WHERE ingest_item_uuid=$1`, id, worker, int64(lease/time.Second))
	if err != nil {
		return models.ClaimedIngestItem{}, err
	}
	item, err := r.claimedByID(ctx, tx, id)
	if err != nil {
		return item, err
	}
	if err = tx.Commit(); err != nil {
		return item, err
	}
	return item, nil
}

func (r *Repository) claimedByID(ctx context.Context, tx *sql.Tx, id uuid.UUID) (models.ClaimedIngestItem, error) {
	var c models.ClaimedIngestItem
	var original, errorCode, errorMessage sql.NullString
	var occurred, completed, cancelled sql.NullTime
	var metadata []byte
	err := tx.QueryRowContext(ctx, `SELECT i.ingest_item_uuid,i.application_uuid,i.connection_uuid,i.event_uuid,i.billing_account_uuid,i.external_call_id,i.idempotency_key,i.source_kind,i.title,i.original_filename,i.occurred_at,i.metadata_redacted,i.status,i.stage,i.attempts,i.max_attempts,i.available_at,i.call_uuid,i.error_code,i.error_message_safe,i.created_at,i.updated_at,i.completed_at,i.cancelled_at,COALESCE(i.uploader_user_uuid,c.created_by_user_uuid),i.destination_company_uuid,i.destination_department_uuid,i.destination_folder_uuid,c.status,c.disable_policy,i.recording_locator_ciphertext,i.recording_locator_key_version,COALESCE((i.instruction_snapshot->>'inherit_scope_instructions')::boolean,false) FROM ingest_items i JOIN integration_connections c USING(connection_uuid) WHERE i.ingest_item_uuid=$1`, id).Scan(&c.ID, &c.ApplicationID, &c.ConnectionID, &c.EventID, &c.BillingAccountID, &c.ExternalCallID, &c.IdempotencyKey, &c.SourceKind, &c.Title, &original, &occurred, &metadata, &c.Status, &c.Stage, &c.Attempts, &c.MaxAttempts, &c.AvailableAt, &c.CallID, &errorCode, &errorMessage, &c.CreatedAt, &c.UpdatedAt, &completed, &cancelled, &c.CreatedByUserID, &c.CompanyID, &c.DepartmentID, &c.FolderID, &c.ConnectionStatus, &c.DisablePolicy, &c.LocatorCiphertext, &c.LocatorKeyVersion, &c.InheritScopeInstructions)
	if err != nil {
		return c, err
	}
	c.OriginalFilename = stringPtr(original)
	c.OccurredAt = timePtr(occurred)
	c.Metadata = metadata
	c.ErrorCode = stringPtr(errorCode)
	c.ErrorMessage = stringPtr(errorMessage)
	c.CompletedAt = timePtr(completed)
	c.CancelledAt = timePtr(cancelled)
	return c, nil
}

func (r *Repository) CompleteIngest(ctx context.Context, itemID, callID uuid.UUID, size int64, duration int, hash []byte) error {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var app, connection uuid.UUID
	var occurredAt sql.NullTime
	err = tx.QueryRowContext(ctx, `SELECT application_uuid,connection_uuid,occurred_at FROM ingest_items WHERE ingest_item_uuid=$1 AND status='processing' FOR UPDATE`, itemID).Scan(&app, &connection, &occurredAt)
	if err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `UPDATE ingest_items SET status='completed',stage='completed',call_uuid=$2,media_size_bytes=$3,media_duration_seconds=$4,media_sha256=$5,recording_locator_ciphertext=NULL,recording_locator_key_version=NULL,locator_expires_at=NULL,locked_at=NULL,locked_by=NULL,lease_expires_at=NULL,completed_at=now(),updated_at=now(),lock_version=lock_version+1 WHERE ingest_item_uuid=$1 AND call_uuid IS NULL`, itemID, callID, size, duration, hash)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return models.ErrIntegrationConflict
	}
	_, err = tx.ExecContext(ctx, `UPDATE calls SET integration_connection_uuid=$2,ingest_item_uuid=$1,occurred_at=$4 WHERE call_uuid=$3 AND ingest_item_uuid IS NULL`, itemID, connection, callID, timePtr(occurredAt))
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO integration_call_participants(call_uuid,connection_uuid,external_user_id,internal_user_uuid,role,display_snapshot)
		SELECT $2,$3,p->>'external_user_id',m.internal_user_uuid,COALESCE(NULLIF(p->>'role',''),'participant'),COALESCE(m.external_display_snapshot,'')
		FROM ingest_items i JOIN ingest_events e ON e.event_uuid=i.event_uuid
		CROSS JOIN LATERAL jsonb_array_elements(COALESCE(e.payload_redacted->'participants','[]'::jsonb)) p
		LEFT JOIN integration_external_user_mappings m ON m.connection_uuid=i.connection_uuid AND m.external_user_id=p->>'external_user_id' AND m.status='mapped'
		WHERE i.ingest_item_uuid=$1 AND NULLIF(p->>'external_user_id','') IS NOT NULL
		ON CONFLICT(call_uuid,connection_uuid,external_user_id) DO NOTHING`, itemID, callID, connection)
	if err != nil {
		return err
	}
	if err = audit(ctx, tx, app, uuid.NullUUID{UUID: connection, Valid: true}, "system", uuid.NullUUID{}, "ingest.completed", "ingest_item", itemID, map[string]any{"call_uuid": callID}); err != nil {
		return err
	}
	if err = outbox(ctx, tx, app, connection, "ingest.completed", itemID, map[string]any{"ingest_item_uuid": itemID, "call_uuid": callID, "status": "completed"}); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE integration_connections SET last_success_at=now(),last_error_code=NULL,updated_at=now() WHERE connection_uuid=$1`, connection)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Repository) FailIngest(ctx context.Context, itemID uuid.UUID, code, message string, retry bool, delay time.Duration) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var app, connection uuid.UUID
	var attempts, max int
	err = tx.QueryRowContext(ctx, `SELECT application_uuid,connection_uuid,attempts,max_attempts FROM ingest_items WHERE ingest_item_uuid=$1 FOR UPDATE`, itemID).Scan(&app, &connection, &attempts, &max)
	if err != nil {
		return err
	}
	status := "failed"
	if retry && attempts < max {
		status = "received"
	}
	available := time.Now().UTC().Add(delay)
	_, err = tx.ExecContext(ctx, `UPDATE ingest_items SET status=$2,available_at=$3,error_code=$4,error_message_safe=$5,locked_at=NULL,locked_by=NULL,lease_expires_at=NULL,updated_at=now(),lock_version=lock_version+1 WHERE ingest_item_uuid=$1`, itemID, status, available, code, message)
	if err != nil {
		return err
	}
	event := "ingest.retry_scheduled"
	if status == "failed" {
		event = "ingest.failed"
	}
	if err = audit(ctx, tx, app, uuid.NullUUID{UUID: connection, Valid: true}, "system", uuid.NullUUID{}, event, "ingest_item", itemID, map[string]any{"error_code": code, "attempt": attempts}); err != nil {
		return err
	}
	if status == "failed" {
		if err = outbox(ctx, tx, app, connection, "ingest.failed", itemID, map[string]any{"ingest_item_uuid": itemID, "status": "failed", "error_code": code}); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE integration_connections SET last_error_code=$2,updated_at=now() WHERE connection_uuid=$1`, connection, code)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Repository) HeartbeatIngest(ctx context.Context, itemID uuid.UUID, worker string, lease time.Duration) error {
	res, err := r.db.ExecContext(ctx, `UPDATE ingest_items SET lease_expires_at=now()+make_interval(secs=>$3),updated_at=now() WHERE ingest_item_uuid=$1 AND locked_by=$2 AND status='processing'`, itemID, worker, int64(lease/time.Second))
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return models.ErrIntegrationConflict
	}
	return nil
}

func (r *Repository) ClearExpiredIngestLocators(ctx context.Context) (int64, error) {
	result, err := r.db.ExecContext(ctx, `UPDATE ingest_items SET recording_locator_ciphertext=NULL,recording_locator_key_version=NULL,locator_expires_at=NULL,updated_at=now() WHERE status IN ('failed','cancelled','completed') AND locator_expires_at IS NOT NULL AND locator_expires_at<now()`)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
