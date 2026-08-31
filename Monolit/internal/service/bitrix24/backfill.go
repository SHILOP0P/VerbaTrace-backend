package bitrix24

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

const maxBackfillRange = 366 * 24 * time.Hour

func (s *Service) PreviewBackfill(ctx context.Context, connectionID, actor uuid.UUID, from, to time.Time) (models.BitrixBackfillPreview, error) {
	from, to, err := normalizeBackfillRange(from, to)
	if err != nil {
		return models.BitrixBackfillPreview{}, err
	}
	var allowed bool
	if err = s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM integration_connections c WHERE c.connection_uuid=$1 AND c.provider='bitrix24' AND c.status IN ('active','degraded') AND COALESCE(c.settings->'capabilities'->>'calls_readable','false')='true' AND `+managerAccessSQL+`)`, connectionID, actor).Scan(&allowed); err != nil {
		return models.BitrixBackfillPreview{}, err
	}
	if !allowed {
		return models.BitrixBackfillPreview{}, ErrForbidden
	}
	info, token, err := s.connectionToken(ctx, connectionID, actor)
	if err != nil {
		return models.BitrixBackfillPreview{}, err
	}
	var firstPage []statisticRecord
	meta, err := s.callPage(ctx, info.Domain, "voximplant.statistic.get", token, map[string]any{
		"FILTER": map[string]any{">=CALL_START_DATE": from.Format(time.RFC3339), "<CALL_START_DATE": to.Format(time.RFC3339)},
		"SORT":   "CALL_START_DATE", "ORDER": "ASC", "start": 0,
	}, &firstPage)
	if err != nil {
		return models.BitrixBackfillPreview{}, err
	}
	estimated := len(firstPage)
	if meta.Total != nil && *meta.Total >= 0 {
		estimated = *meta.Total
	}
	return models.BitrixBackfillPreview{ConnectionID: connectionID, RangeFrom: from, RangeTo: to, EstimatedCalls: estimated}, nil
}

func (s *Service) CreateBackfill(ctx context.Context, connectionID, actor uuid.UUID, from, to time.Time, idempotencyKey string) (models.BitrixBackfill, bool, error) {
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if idempotencyKey == "" || len(idempotencyKey) > 200 {
		return models.BitrixBackfill{}, false, ErrInvalid
	}
	preview, err := s.PreviewBackfill(ctx, connectionID, actor, from, to)
	if err != nil {
		return models.BitrixBackfill{}, false, err
	}
	var allowed bool
	if err = s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM integration_connections c WHERE c.connection_uuid=$1 AND c.provider='bitrix24' AND c.status IN ('active','degraded') AND COALESCE(c.settings->'capabilities'->>'calls_readable','false')='true' AND `+managerAccessSQL+`)`, connectionID, actor).Scan(&allowed); err != nil {
		return models.BitrixBackfill{}, false, err
	}
	if !allowed {
		return models.BitrixBackfill{}, false, ErrForbidden
	}
	id := uuid.NewSHA1(uuid.NameSpaceOID, []byte("bitrix-backfill:"+connectionID.String()+":"+actor.String()+":"+idempotencyKey))
	result, err := s.db.ExecContext(ctx, `INSERT INTO integration_backfills(backfill_uuid,connection_uuid,requested_by_user_uuid,range_from,range_to,estimated_calls) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(backfill_uuid) DO NOTHING`, id, connectionID, actor, preview.RangeFrom, preview.RangeTo, preview.EstimatedCalls)
	if err != nil {
		return models.BitrixBackfill{}, false, err
	}
	created := false
	if affected, _ := result.RowsAffected(); affected == 1 {
		created = true
	}
	item, err := s.GetBackfill(ctx, connectionID, id, actor)
	return item, created, err
}

func (s *Service) GetBackfill(ctx context.Context, connectionID, backfillID, actor uuid.UUID) (models.BitrixBackfill, error) {
	var item models.BitrixBackfill
	var estimated sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT b.backfill_uuid,b.connection_uuid,b.requested_by_user_uuid,b.range_from,b.range_to,b.status,b.estimated_calls,
		COUNT(bc.candidate_uuid),
		COUNT(bc.candidate_uuid) FILTER (WHERE cand.status='imported'),
		COUNT(bc.candidate_uuid) FILTER (WHERE cand.status IN ('waiting_for_recording','queued')),
		COUNT(bc.candidate_uuid) FILTER (WHERE cand.status='skipped_no_media'),
		COUNT(bc.candidate_uuid) FILTER (WHERE cand.status='failed'),
		b.lock_version,b.created_at,b.updated_at,b.finished_at
	FROM integration_backfills b
	JOIN integration_connections c ON c.connection_uuid=b.connection_uuid
	LEFT JOIN integration_backfill_candidates bc ON bc.backfill_uuid=b.backfill_uuid
	LEFT JOIN bitrix_call_candidates cand ON cand.candidate_uuid=bc.candidate_uuid
	WHERE b.connection_uuid=$1 AND b.backfill_uuid=$3 AND `+managerOrLeaderAccessSQL+`
	GROUP BY b.backfill_uuid`, connectionID, actor, backfillID).Scan(
		&item.ID, &item.ConnectionID, &item.RequestedByID, &item.RangeFrom, &item.RangeTo, &item.Status, &estimated,
		&item.DiscoveredCalls, &item.ImportedCalls, &item.PendingCalls, &item.SkippedCalls, &item.ErrorCalls,
		&item.LockVersion, &item.CreatedAt, &item.UpdatedAt, &item.FinishedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return item, ErrNotFound
	}
	if err != nil {
		return item, err
	}
	if estimated.Valid {
		value := int(estimated.Int64)
		item.EstimatedCalls = &value
	}
	return item, nil
}

func (s *Service) ListBackfills(ctx context.Context, connectionID, actor uuid.UUID) ([]models.BitrixBackfill, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT b.backfill_uuid FROM integration_backfills b JOIN integration_connections c ON c.connection_uuid=b.connection_uuid WHERE b.connection_uuid=$1 AND `+managerOrLeaderAccessSQL+` ORDER BY b.created_at DESC LIMIT 20`, connectionID, actor)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	ids := make([]uuid.UUID, 0, 20)
	for rows.Next() {
		var id uuid.UUID
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	items := make([]models.BitrixBackfill, 0, len(ids))
	for _, id := range ids {
		item, getErr := s.GetBackfill(ctx, connectionID, id, actor)
		if getErr != nil {
			return nil, getErr
		}
		items = append(items, item)
	}
	return items, nil
}

func (s *Service) RunBackfillWorker(ctx context.Context, ingestor Ingestor) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.processBackfill(ctx, ingestor)
			}
		}
	}()
	return done
}

func (s *Service) processBackfill(ctx context.Context, ingestor Ingestor) {
	if ingestor == nil {
		return
	}
	var id, connectionID uuid.UUID
	var from, to time.Time
	err := s.db.QueryRowContext(ctx, `WITH next AS (
		SELECT b.backfill_uuid FROM integration_backfills b JOIN integration_connections c USING(connection_uuid)
		WHERE b.status IN ('pending','running') AND b.available_at<=now() AND (b.lease_until IS NULL OR b.lease_until<now())
		AND c.provider='bitrix24' AND c.status IN ('active','degraded')
		AND COALESCE(c.settings->'capabilities'->>'calls_readable','false')='true'
		ORDER BY b.created_at FOR UPDATE OF b SKIP LOCKED LIMIT 1
	) UPDATE integration_backfills b SET status='running',lease_until=now()+interval '10 minutes',lock_version=lock_version+1,updated_at=now()
	FROM next WHERE b.backfill_uuid=next.backfill_uuid RETURNING b.backfill_uuid,b.connection_uuid,b.range_from,b.range_to`).Scan(&id, &connectionID, &from, &to)
	if err != nil {
		return
	}
	info, token, err := s.systemConnectionToken(ctx, connectionID)
	if err != nil {
		s.failBackfill(ctx, id)
		return
	}
	principal, err := s.connectorPrincipal(ctx, connectionID)
	if err != nil {
		s.failBackfill(ctx, id)
		return
	}
	records, err := s.listStatisticRecords(ctx, info.Domain, token, map[string]any{
		">=CALL_START_DATE": from.UTC().Format(time.RFC3339),
		"<CALL_START_DATE":  to.UTC().Format(time.RFC3339),
	})
	if err != nil {
		s.failBackfill(ctx, id)
		return
	}
	for _, record := range records {
		if ctx.Err() != nil {
			return
		}
		s.persistBackfillRecord(ctx, ingestor, principal, id, connectionID, info.Domain, token, record)
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE integration_backfills SET status='completed',lease_until=NULL,finished_at=now(),lock_version=lock_version+1,updated_at=now() WHERE backfill_uuid=$1 AND status='running'`, id)
}

func (s *Service) persistBackfillRecord(ctx context.Context, ingestor Ingestor, principal models.IntegrationPrincipal, backfillID, connectionID uuid.UUID, domain, token string, record statisticRecord) {
	started, err := time.Parse(time.RFC3339, record.StartedAt)
	if err != nil {
		return
	}
	externalID := strings.TrimSpace(record.ID)
	if externalID == "" {
		externalID = strings.TrimSpace(record.CallID)
	}
	if externalID == "" {
		return
	}
	started = started.UTC()
	duration := intValue(record.Duration)
	payload, _ := json.Marshal(map[string]any{"id": externalID, "portal_user_id": record.PortalUserID, "call_type": stringValue(record.CallType), "duration_seconds": duration, "occurred_at": started, "crm_activity_id": record.CRMActivityID, "has_recording": record.RecordingURL != ""})
	hash := sha256.Sum256(payload)
	status := "waiting_for_recording"
	if strings.TrimSpace(record.RecordingURL) != "" {
		status = "queued"
	}
	candidateID := uuid.New()
	err = s.db.QueryRowContext(ctx, `INSERT INTO bitrix_call_candidates(candidate_uuid,connection_uuid,external_call_id,occurred_at,external_user_id,call_direction,duration_seconds,status,payload_redacted,payload_hash,recording_deadline_at)
		VALUES($1,$2,$3,$4,NULLIF($5,''),NULLIF($6,''),$7,$8,$9,$10,$11)
		ON CONFLICT(connection_uuid,external_call_id) DO UPDATE SET last_seen_at=now(),payload_redacted=EXCLUDED.payload_redacted,payload_hash=EXCLUDED.payload_hash,duration_seconds=EXCLUDED.duration_seconds,lock_version=bitrix_call_candidates.lock_version+1
		RETURNING candidate_uuid`, candidateID, connectionID, externalID, started, record.PortalUserID, stringValue(record.CallType), duration, status, payload, hash[:], s.now().UTC().Add(30*time.Minute)).Scan(&candidateID)
	if err != nil {
		return
	}
	_, _ = s.db.ExecContext(ctx, `INSERT INTO integration_backfill_candidates(backfill_uuid,candidate_uuid) VALUES($1,$2) ON CONFLICT DO NOTHING`, backfillID, candidateID)
	if strings.TrimSpace(record.RecordingURL) != "" {
		_ = s.ingestStatisticRecord(ctx, ingestor, principal, connectionID, domain, token, record, started, externalID)
	}
}

func (s *Service) failBackfill(ctx context.Context, id uuid.UUID) {
	_, _ = s.db.ExecContext(ctx, `UPDATE integration_backfills SET
		status=CASE WHEN error_calls+1>=5 THEN 'failed' ELSE 'pending' END,
		error_calls=error_calls+1,
		available_at=now()+make_interval(mins=>LEAST(30,(error_calls+1)*(error_calls+1))),
		lease_until=NULL,
		finished_at=CASE WHEN error_calls+1>=5 THEN now() ELSE NULL END,
		lock_version=lock_version+1,updated_at=now()
		WHERE backfill_uuid=$1 AND status='running'`, id)
}

func normalizeBackfillRange(from, to time.Time) (time.Time, time.Time, error) {
	from, to = from.UTC(), to.UTC()
	if from.IsZero() || to.IsZero() || !from.Before(to) || to.Sub(from) > maxBackfillRange {
		return time.Time{}, time.Time{}, ErrInvalid
	}
	return from, to, nil
}
