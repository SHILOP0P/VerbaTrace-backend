package privacy

import (
	"context"
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func (s *Service) CreateMediaAccessSession(ctx context.Context, call models.Call, userID uuid.UUID, variant string) (uuid.UUID, time.Time, error) {
	capabilities, err := s.Capabilities(ctx, call, userID)
	if err != nil {
		return uuid.Nil, time.Time{}, err
	}
	if variant == "original" && !capabilities.CanReadOriginalMedia {
		return uuid.Nil, time.Time{}, ErrOriginalMediaForbidden
	}
	if variant == "redacted" && !capabilities.CanRequestSanitizedMedia {
		return uuid.Nil, time.Time{}, ErrOriginalMediaForbidden
	}
	if variant != "original" && variant != "redacted" {
		return uuid.Nil, time.Time{}, ErrMediaVariantNotFound
	}
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.Nil, time.Time{}, err
	}
	expires := s.now().UTC().Add(15 * time.Minute)
	_, err = s.db.ExecContext(ctx, `INSERT INTO media_access_sessions(media_access_session_uuid,call_uuid,actor_user_uuid,variant,expires_at) VALUES($1,$2,$3,$4,$5)`, id, call.ID, userID, variant, expires)
	return id, expires, err
}

func (s *Service) ValidateMediaAccessSession(ctx context.Context, id, callID, userID uuid.UUID, variant string) error {
	if id == uuid.Nil {
		return nil
	}
	var exists bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM media_access_sessions WHERE media_access_session_uuid=$1 AND call_uuid=$2 AND actor_user_uuid=$3 AND variant=$4 AND expires_at>now())`, id, callID, userID, variant).Scan(&exists)
	if err != nil {
		return err
	}
	if !exists {
		return ErrOriginalMediaForbidden
	}
	return nil
}

func (s *Service) AuditMediaOpen(ctx context.Context, call models.Call, userID, sessionID uuid.UUID, variant string) error {
	dedup := ""
	if sessionID != uuid.Nil {
		dedup = call.ID.String() + ":" + userID.String() + ":" + sessionID.String()
	}
	id, err := uuid.NewV7()
	if err != nil {
		return err
	}
	eventType := "sanitized_media_opened"
	if variant == "original" {
		eventType = "original_media_opened"
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO privacy_audit_events(privacy_audit_uuid,scope_type,scope_uuid,call_uuid,actor_user_uuid,actor_type,event_type,entity_type,entity_uuid,metadata_redacted,deduplication_key)
		VALUES($1,$2,$3,$4,$5,'user',$6,'call',$4,'{}'::jsonb,NULLIF($7,'')) ON CONFLICT(event_type,deduplication_key) WHERE deduplication_key IS NOT NULL DO NOTHING`, id, scopeForCall(call), scopeIDForCall(call), call.ID, userID, eventType, dedup)
	return err
}

func (s *Service) ListAudit(ctx context.Context, callID uuid.UUID, limit int) ([]models.PrivacyAuditEvent, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT privacy_audit_uuid,call_uuid,actor_user_uuid,event_type,entity_type,entity_uuid,metadata_redacted,created_at FROM privacy_audit_events WHERE call_uuid=$1 ORDER BY created_at DESC,privacy_audit_uuid DESC LIMIT $2`, callID, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	result := make([]models.PrivacyAuditEvent, 0)
	for rows.Next() {
		var item models.PrivacyAuditEvent
		if err = rows.Scan(&item.ID, &item.CallID, &item.ActorID, &item.EventType, &item.EntityType, &item.EntityID, &item.Metadata, &item.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Service) CleanupExpiredMediaSessions(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM media_access_sessions WHERE expires_at<now()-interval '1 hour'`)
	return err
}
