package bitrix24

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

type Ingestor interface {
	AcceptURLIngest(context.Context, models.IntegrationPrincipal, models.IngestCallInput, string) (models.IngestItem, bool, error)
}

func (s *Service) RunReconciler(ctx context.Context, ingestor Ingestor) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.reconcileActiveConnections(ctx, ingestor)
				s.processRecoverableCandidate(ctx, ingestor)
			}
		}
	}()
	return done
}

func (s *Service) reconcileActiveConnections(ctx context.Context, ingestor Ingestor) {
	rows, err := s.db.QueryContext(ctx, `SELECT connection_uuid FROM integration_connections
		WHERE provider='bitrix24' AND status IN ('active','degraded')
		AND COALESCE(settings->'capabilities'->>'calls_readable','false')='true'
		ORDER BY COALESCE(last_success_at,'epoch') LIMIT 20`)
	if err != nil {
		return
	}
	defer func() { _ = rows.Close() }()
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	for _, id := range ids {
		if ctx.Err() != nil {
			return
		}
		_ = s.ReconcileConnection(ctx, id, ingestor)
	}
}

type statisticRecord struct {
	ID             string `json:"ID"`
	CallID         string `json:"CALL_ID"`
	ExternalCallID string `json:"EXTERNAL_CALL_ID"`
	PortalUserID   string `json:"PORTAL_USER_ID"`
	Duration       any    `json:"CALL_DURATION"`
	StartedAt      string `json:"CALL_START_DATE"`
	RecordingURL   string `json:"CALL_RECORD_URL"`
	CallType       any    `json:"CALL_TYPE"`
	CRMActivityID  string `json:"CRM_ACTIVITY_ID"`
}

func (s *Service) ReconcileConnection(ctx context.Context, connectionID uuid.UUID, ingestor Ingestor) error {
	if ingestor == nil {
		return ErrUnavailable
	}
	info, token, err := s.systemConnectionToken(ctx, connectionID)
	if err != nil {
		return err
	}
	var cursorAt sql.NullTime
	var cursorID sql.NullString
	err = s.db.QueryRowContext(ctx, `SELECT cursor_occurred_at,cursor_external_id FROM integration_sync_checkpoints WHERE connection_uuid=$1`, connectionID).Scan(&cursorAt, &cursorID)
	if errors.Is(err, sql.ErrNoRows) {
		cursorAt = sql.NullTime{Time: s.now().UTC().Add(-10 * time.Minute), Valid: true}
		cursorID = sql.NullString{String: "", Valid: true}
	} else if err != nil {
		return err
	}
	from := cursorAt.Time.Add(-5 * time.Minute)
	to := s.now().UTC()
	records, err := s.listStatisticRecords(ctx, info.Domain, token, map[string]any{
		">=CALL_START_DATE": from.Format(time.RFC3339),
		"<CALL_START_DATE":  to.Format(time.RFC3339),
	})
	if err != nil {
		_, _ = s.db.ExecContext(ctx, `UPDATE integration_connections SET status='degraded',last_error_code='bitrix_reconciliation_failed',last_health_at=now(),updated_at=now() WHERE connection_uuid=$1`, connectionID)
		return err
	}
	principal, err := s.connectorPrincipal(ctx, connectionID)
	if err != nil {
		return err
	}
	lastAt, lastID := cursorAt.Time, cursorID.String
	for _, record := range records {
		started, parseErr := time.Parse(time.RFC3339, record.StartedAt)
		if parseErr != nil {
			continue
		}
		started = started.UTC()
		externalID := strings.TrimSpace(record.ID)
		if externalID == "" {
			externalID = strings.TrimSpace(record.CallID)
		}
		if externalID == "" {
			continue
		}
		duration := intValue(record.Duration)
		payload, _ := json.Marshal(map[string]any{"id": externalID, "portal_user_id": record.PortalUserID, "call_type": stringValue(record.CallType), "duration_seconds": duration, "occurred_at": started, "crm_activity_id": record.CRMActivityID, "has_recording": record.RecordingURL != ""})
		hash := sha256.Sum256(payload)
		status := "waiting_for_recording"
		if record.RecordingURL != "" {
			status = "queued"
		}
		candidateID := uuid.New()
		deadline := s.now().UTC().Add(30 * time.Minute)
		_, err = s.db.ExecContext(ctx, `INSERT INTO bitrix_call_candidates(candidate_uuid,connection_uuid,external_call_id,occurred_at,external_user_id,call_direction,duration_seconds,status,payload_redacted,payload_hash,recording_deadline_at)
			VALUES($1,$2,$3,$4,NULLIF($5,''),NULLIF($6,''),$7,$8,$9,$10,$11) ON CONFLICT(connection_uuid,external_call_id) DO UPDATE SET last_seen_at=now(),payload_redacted=EXCLUDED.payload_redacted,payload_hash=EXCLUDED.payload_hash,duration_seconds=EXCLUDED.duration_seconds,status=CASE WHEN bitrix_call_candidates.status='waiting_for_recording' AND EXCLUDED.status='queued' THEN 'queued' ELSE bitrix_call_candidates.status END,lock_version=bitrix_call_candidates.lock_version+1`, candidateID, connectionID, externalID, started, record.PortalUserID, stringValue(record.CallType), duration, status, payload, hash[:], deadline)
		if err != nil {
			return err
		}
		if record.RecordingURL != "" {
			_ = s.ingestStatisticRecord(ctx, ingestor, principal, connectionID, info.Domain, token, record, started, externalID)
		}
		if started.After(lastAt) || started.Equal(lastAt) && externalID > lastID {
			lastAt, lastID = started, externalID
		}
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO integration_sync_checkpoints(connection_uuid,cursor_occurred_at,cursor_external_id,last_reconciliation_at,updated_at) VALUES($1,$2,$3,now(),now()) ON CONFLICT(connection_uuid) DO UPDATE SET cursor_occurred_at=EXCLUDED.cursor_occurred_at,cursor_external_id=EXCLUDED.cursor_external_id,last_reconciliation_at=now(),lock_version=integration_sync_checkpoints.lock_version+1,updated_at=now()`, connectionID, lastAt, lastID)
	return err
}

func (s *Service) processRecoverableCandidate(ctx context.Context, ingestor Ingestor) {
	var connectionID uuid.UUID
	var externalID string
	var deadline time.Time
	err := s.db.QueryRowContext(ctx, `WITH next_candidate AS (
		SELECT cand.candidate_uuid
		FROM bitrix_call_candidates cand JOIN integration_connections c USING(connection_uuid)
		WHERE cand.status IN ('waiting_for_recording','failed') AND cand.attempts<cand.max_attempts
		AND cand.last_seen_at<now()-interval '30 seconds'
		AND (cand.lease_until IS NULL OR cand.lease_until<now())
		AND c.status IN ('active','degraded')
		AND COALESCE(c.settings->'capabilities'->>'calls_readable','false')='true'
		ORDER BY cand.last_seen_at,cand.candidate_uuid
		FOR UPDATE OF cand SKIP LOCKED LIMIT 1
	)
	UPDATE bitrix_call_candidates cand
	SET lease_until=now()+interval '2 minutes',lock_version=lock_version+1
	FROM next_candidate selected
	WHERE cand.candidate_uuid=selected.candidate_uuid
	RETURNING cand.connection_uuid,cand.external_call_id,cand.recording_deadline_at`).Scan(&connectionID, &externalID, &deadline)
	if err != nil {
		return
	}
	if !s.now().UTC().Before(deadline) {
		_, _ = s.db.ExecContext(ctx, `UPDATE bitrix_call_candidates SET status=CASE WHEN status='waiting_for_recording' THEN 'skipped_no_media' ELSE 'failed' END,error_code='recording_deadline_elapsed',attempts=max_attempts,last_seen_at=now(),lease_until=NULL,lock_version=lock_version+1 WHERE connection_uuid=$1 AND external_call_id=$2 AND status IN ('waiting_for_recording','failed')`, connectionID, externalID)
		return
	}
	info, token, err := s.systemConnectionToken(ctx, connectionID)
	if err != nil {
		s.releaseCandidateLease(ctx, connectionID, externalID, "credential_unavailable")
		return
	}
	var records []statisticRecord
	err = s.call(ctx, info.Domain, "voximplant.statistic.get", token, map[string]any{"FILTER": map[string]any{"ID": externalID}, "start": 0}, &records)
	if err != nil || len(records) == 0 || strings.TrimSpace(records[0].RecordingURL) == "" {
		_, _ = s.db.ExecContext(ctx, `UPDATE bitrix_call_candidates SET attempts=attempts+1,last_seen_at=now(),error_code=$3,lease_until=NULL,lock_version=lock_version+1 WHERE connection_uuid=$1 AND external_call_id=$2 AND status IN ('waiting_for_recording','failed')`, connectionID, externalID, candidateRetryCode(err))
		return
	}
	record := records[0]
	started, err := time.Parse(time.RFC3339, record.StartedAt)
	if err != nil {
		_, _ = s.db.ExecContext(ctx, `UPDATE bitrix_call_candidates SET attempts=attempts+1,last_seen_at=now(),error_code='invalid_occurred_at',lease_until=NULL,lock_version=lock_version+1 WHERE connection_uuid=$1 AND external_call_id=$2`, connectionID, externalID)
		return
	}
	principal, err := s.connectorPrincipal(ctx, connectionID)
	if err != nil {
		s.releaseCandidateLease(ctx, connectionID, externalID, "principal_unavailable")
		return
	}
	_ = s.ingestStatisticRecord(ctx, ingestor, principal, connectionID, info.Domain, token, record, started.UTC(), externalID)
}

func (s *Service) listStatisticRecords(ctx context.Context, domain, token string, filter map[string]any) ([]statisticRecord, error) {
	const maxPages = 200
	start := 0
	records := make([]statisticRecord, 0, 50)
	for page := 0; page < maxPages; page++ {
		var chunk []statisticRecord
		meta, err := s.callPage(ctx, domain, "voximplant.statistic.get", token, map[string]any{
			"FILTER": filter,
			"SORT":   "CALL_START_DATE",
			"ORDER":  "ASC",
			"start":  start,
		}, &chunk)
		if err != nil {
			return nil, err
		}
		records = append(records, chunk...)
		if meta.Next == nil {
			return records, nil
		}
		if *meta.Next <= start {
			return nil, ErrUnavailable
		}
		start = *meta.Next
	}
	return nil, ErrUnavailable
}

func (s *Service) ingestStatisticRecord(ctx context.Context, ingestor Ingestor, principal models.IntegrationPrincipal, connectionID uuid.UUID, portalDomain, token string, record statisticRecord, started time.Time, externalID string) error {
	recordingURL, err := withAuth(record.RecordingURL, portalDomain, token)
	if err != nil {
		return err
	}
	participants := []map[string]string{}
	if record.PortalUserID != "" {
		participants = append(participants, map[string]string{"external_user_id": record.PortalUserID, "role": "employee"})
	}
	input := models.IngestCallInput{SchemaVersion: 2, ExternalEventID: "bitrix-stat-" + externalID, ExternalCallID: externalID, Title: "Bitrix24 звонок #" + externalID, RecordingURL: recordingURL, OriginalFilename: "bitrix24-" + externalID, OccurredAt: &started, Participants: participants, Metadata: map[string]any{"source_provider": "bitrix24", "crm_activity_id": record.CRMActivityID, "call_direction": stringValue(record.CallType)}, InstructionMode: "scope_and_folder"}
	item, _, ingestErr := ingestor.AcceptURLIngest(ctx, principal, input, "bitrix24:"+connectionID.String()+":"+externalID)
	if ingestErr == nil {
		_, _ = s.db.ExecContext(ctx, `UPDATE bitrix_call_candidates SET status='imported',ingest_item_uuid=$3,error_code=NULL,attempts=attempts+1,last_seen_at=now(),lease_until=NULL,lock_version=lock_version+1 WHERE connection_uuid=$1 AND external_call_id=$2`, connectionID, externalID, item.ID)
		return nil
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE bitrix_call_candidates SET status='failed',error_code='ingest_rejected',attempts=attempts+1,last_seen_at=now(),lease_until=NULL,lock_version=lock_version+1 WHERE connection_uuid=$1 AND external_call_id=$2`, connectionID, externalID)
	return ingestErr
}

func (s *Service) releaseCandidateLease(ctx context.Context, connectionID uuid.UUID, externalID, code string) {
	_, _ = s.db.ExecContext(ctx, `UPDATE bitrix_call_candidates SET error_code=$3,lease_until=NULL,last_seen_at=now(),lock_version=lock_version+1 WHERE connection_uuid=$1 AND external_call_id=$2 AND status IN ('waiting_for_recording','failed')`, connectionID, externalID, code)
}

func candidateRetryCode(err error) string {
	if err != nil {
		return "provider_retry_failed"
	}
	return "recording_not_ready"
}

func (s *Service) connectorPrincipal(ctx context.Context, id uuid.UUID) (models.IntegrationPrincipal, error) {
	var p models.IntegrationPrincipal
	err := s.db.QueryRowContext(ctx, `SELECT c.application_uuid,a.billing_account_uuid,a.environment
		FROM integration_connections c JOIN developer_applications a USING(application_uuid)
		WHERE c.connection_uuid=$1 AND c.provider='bitrix24' AND c.status IN ('active','degraded')
		AND COALESCE(c.settings->'capabilities'->>'calls_readable','false')='true' AND a.status='active'`, id).Scan(&p.ApplicationUUID, &p.BillingAccountUUID, &p.Environment)
	if err != nil {
		return p, err
	}
	p.ConnectionUUID = id
	p.Scopes = []string{"calls:write"}
	return p, nil
}
func withAuth(raw, portalDomain, token string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.Port() != "" || parsed.User != nil {
		return "", ErrInvalid
	}
	host := strings.ToLower(parsed.Hostname())
	if strings.HasSuffix(host, ".voximplant.com") {
		return parsed.String(), nil
	}
	if !strings.EqualFold(host, portalDomain) {
		return "", ErrInvalid
	}
	query := parsed.Query()
	query.Set("auth", token)
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}
func intValue(value any) int {
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case string:
		parsed, _ := strconv.Atoi(typed)
		return max(0, parsed)
	case json.Number:
		parsed, _ := strconv.Atoi(typed.String())
		return max(0, parsed)
	default:
		return 0
	}
}
