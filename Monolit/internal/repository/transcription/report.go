package transcription

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/google/uuid"

	"verbatrace/monolit/internal/models"
)

// GetReportTranscription reads one immutable revision; zero selects the active
// revision in the same statement. Legacy transcripts without history are v1.
func (r *Repository) GetReportTranscription(ctx context.Context, callID uuid.UUID, revision int) (models.Transcription, int, error) {
	var raw []byte
	var selected int
	err := r.db.QueryRowContext(ctx, `
 SELECT COALESCE(c.payload, CASE WHEN r.transcription_revision_uuid IS NULL AND chosen.revision=1
 AND NOT EXISTS(SELECT 1 FROM call_transcription_revisions rr WHERE rr.transcription_uuid=t.transcription_uuid)
 THEN jsonb_build_object('text',t.text,'segments',t.segments,'words',t.words) END), chosen.revision
 FROM call_transcriptions t
 LEFT JOIN call_transcription_revision_state s ON s.transcription_uuid=t.transcription_uuid
 CROSS JOIN LATERAL (SELECT CASE WHEN $2::int>0 THEN $2::int ELSE COALESCE(s.active_revision,
 (SELECT max(rr.revision) FROM call_transcription_revisions rr WHERE rr.transcription_uuid=t.transcription_uuid),1) END AS revision) chosen
 LEFT JOIN call_transcription_revisions r ON r.transcription_uuid=t.transcription_uuid AND r.revision=chosen.revision
 LEFT JOIN call_transcription_contents c ON c.transcription_content_uuid=r.transcription_content_uuid
 WHERE t.call_uuid=$1 AND t.status='transcribed'`, callID, revision).Scan(&raw, &selected)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && len(raw) == 0) {
		return models.Transcription{}, 0, models.ErrTranscriptionNotFound
	}
	if err != nil {
		return models.Transcription{}, 0, err
	}
	var payload struct {
		Text     string                        `json:"text"`
		Segments []models.TranscriptionSegment `json:"segments"`
		Words    []models.TranscriptionWord    `json:"words"`
	}
	if err = json.Unmarshal(raw, &payload); err != nil {
		return models.Transcription{}, 0, err
	}
	return models.Transcription{CallUUID: callID, Status: models.TranscriptionStatusTranscribed, Text: &payload.Text, Segments: payload.Segments, Words: payload.Words}, selected, nil
}

func (r *Repository) GetReportSpeakerNames(ctx context.Context, callID uuid.UUID) (map[string]string, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT speaker_key,display_name FROM call_transcription_speaker_assignments WHERE call_uuid=$1`, callID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	names := map[string]string{}
	for rows.Next() {
		var key, name string
		if err = rows.Scan(&key, &name); err != nil {
			return nil, err
		}
		names[key] = name
	}
	return names, rows.Err()
}
