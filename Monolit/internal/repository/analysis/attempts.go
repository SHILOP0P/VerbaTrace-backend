package analysis

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func (r *Repository) CreateAttempt(ctx context.Context, callID, userID uuid.UUID) (models.CallAnalysisAttempt, error) {
	var revision int
	err := r.db.QueryRowContext(ctx, `SELECT COALESCE(s.active_revision, (SELECT max(r.revision) FROM call_transcription_revisions r WHERE r.transcription_uuid=t.transcription_uuid), 1) FROM call_transcriptions t LEFT JOIN call_transcription_revision_state s ON s.transcription_uuid=t.transcription_uuid WHERE t.call_uuid=$1`, callID).Scan(&revision)
	if err != nil {
		return models.CallAnalysisAttempt{}, err
	}
	id, now := uuid.New(), time.Now().UTC()
	_, err = r.db.ExecContext(ctx, `INSERT INTO call_analysis_attempts (analysis_attempt_uuid,call_uuid,transcription_revision,requested_by_user_uuid,status,created_at,updated_at) VALUES ($1,$2,$3,$4,'pending',$5,$5)`, id, callID, revision, userID, now)
	if err != nil {
		return models.CallAnalysisAttempt{}, fmt.Errorf("create analysis attempt: %w", err)
	}
	return models.CallAnalysisAttempt{ID: id, CallUUID: callID, TranscriptionRevision: revision, Status: "pending", CreatedAt: now, UpdatedAt: now}, nil
}

func (r *Repository) ActiveAttempt(ctx context.Context, callID uuid.UUID) (models.CallAnalysisAttempt, error) {
	var a models.CallAnalysisAttempt
	var message sql.NullString
	err := r.db.QueryRowContext(ctx, `SELECT analysis_attempt_uuid,call_uuid,transcription_revision,status,error_message,created_at,updated_at FROM call_analysis_attempts WHERE call_uuid=$1 AND status IN ('pending','processing') ORDER BY created_at DESC LIMIT 1`, callID).Scan(&a.ID, &a.CallUUID, &a.TranscriptionRevision, &a.Status, &message, &a.CreatedAt, &a.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return a, models.ErrAnalysisNotFound
	}
	if err != nil {
		return a, err
	}
	if message.Valid {
		a.ErrorMessage = &message.String
	}
	return a, nil
}

func (r *Repository) MarkAttempt(ctx context.Context, id uuid.UUID, status string, cause error) error {
	var message any
	if cause != nil {
		message = cause.Error()
	}
	_, err := r.db.ExecContext(ctx, `UPDATE call_analysis_attempts SET status=$2,error_message=$3,updated_at=now() WHERE analysis_attempt_uuid=$1`, id, status, message)
	return err
}

func (r *Repository) CurrentTranscriptionRevision(ctx context.Context, callID uuid.UUID) (int, error) {
	var revision int
	err := r.db.QueryRowContext(ctx, `SELECT COALESCE(s.active_revision, (SELECT max(r.revision) FROM call_transcription_revisions r WHERE r.transcription_uuid=t.transcription_uuid), 1) FROM call_transcriptions t LEFT JOIN call_transcription_revision_state s ON s.transcription_uuid=t.transcription_uuid WHERE t.call_uuid=$1`, callID).Scan(&revision)
	return revision, err
}
