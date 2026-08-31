package bitrix24

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"strings"

	"github.com/google/uuid"
)

func (s *Service) AcceptEvent(ctx context.Context, values url.Values) (bool, error) {
	if s.config.EventToken == "" {
		return false, ErrUnavailable
	}
	provided := strings.TrimSpace(values.Get("auth[application_token]"))
	expectedHash := sha256.Sum256([]byte(s.config.EventToken))
	providedHash := sha256.Sum256([]byte(provided))
	if provided == "" || subtle.ConstantTimeCompare(providedHash[:], expectedHash[:]) != 1 {
		return false, ErrForbidden
	}
	memberID := strings.TrimSpace(values.Get("auth[member_id]"))
	eventType := strings.TrimSpace(values.Get("event"))
	if memberID == "" || eventType == "" {
		return false, ErrInvalid
	}
	var connectionID uuid.UUID
	err := s.db.QueryRowContext(ctx, `SELECT connection_uuid FROM integration_connections WHERE provider='bitrix24' AND status NOT IN ('revoked','disabled') AND settings->>'portal_member_id'=$1`, memberID).Scan(&connectionID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrNotFound
	}
	if err != nil {
		return false, err
	}
	redacted := map[string]any{"event": eventType, "member_id": memberID, "data": redactEventData(values)}
	payload, _ := json.Marshal(redacted)
	hash := sha256.Sum256(payload)
	externalID := "bitrix-event-" + hex.EncodeToString(hash[:])
	result, err := s.db.ExecContext(ctx, `INSERT INTO ingest_events(event_uuid,connection_uuid,external_event_id,event_type,schema_version,payload_redacted,payload_sha256,accepted,received_at) VALUES($1,$2,$3,$4,1,$5,$6,true,$7) ON CONFLICT(connection_uuid,external_event_id) DO NOTHING`, uuid.New(), connectionID, externalID, eventType, payload, hash[:], s.now().UTC())
	if err != nil {
		return false, err
	}
	affected, _ := result.RowsAffected()
	if affected == 1 {
		_, _ = s.db.ExecContext(ctx, `UPDATE integration_connections SET last_event_at=now(),updated_at=now() WHERE connection_uuid=$1`, connectionID)
	}
	return affected == 1, nil
}

func redactEventData(values url.Values) map[string]any {
	result := map[string]any{}
	for key, list := range values {
		if strings.HasPrefix(strings.ToLower(key), "auth[") {
			continue
		}
		if len(list) == 1 {
			result[key] = truncateEventValue(list[0])
		} else {
			safe := make([]string, 0, len(list))
			for _, value := range list {
				safe = append(safe, truncateEventValue(value))
			}
			result[key] = safe
		}
	}
	return result
}
func truncateEventValue(value string) string {
	if len(value) > 1000 {
		return value[:1000]
	}
	return value
}
