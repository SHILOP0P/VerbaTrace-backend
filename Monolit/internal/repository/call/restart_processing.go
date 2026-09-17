package call

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

// RestartCallProcessing puts a cancelled call back in the queue. The recording
// never moved, so only the work starts again — and the mode may change with it,
// because somebody who stopped an analysis usually wants the transcript alone.
//
// Rights are the ones cancellation uses: whoever could stop the work may start
// it again.
func (r *Repository) RestartCallProcessing(ctx context.Context, id, userID uuid.UUID, transcriptionOnly bool, job models.ProcessingJob) (models.Call, error) {
	// Same reason as cancellation: the final SELECT reads the row the UPDATE
	// produced, because a join back to calls would still see the old status.
	query := fmt.Sprintf(`
	WITH restarted AS (
		UPDATE calls c
		SET status = 'new', transcription_only = $3
		WHERE c.call_uuid = $1
		  AND c.status = 'cancelled'
		  AND %s
		RETURNING c.*
	), queued AS (
		INSERT INTO processing_jobs (
			job_uuid, job_type, transcription_mode, entity_uuid,
			status, attempts, max_attempts, available_at, created_at, updated_at
		)
		SELECT $4, $5, $6, n.call_uuid, $7, 0, $8, $9, $9, $9
		FROM restarted n
	)
	SELECT `+callColumns+`
	FROM restarted c
	`, deletableByUserCondition("c", "$2"))

	call, err := scanCallRow(r.db.QueryRowContext(ctx, query,
		id,
		userID,
		transcriptionOnly,
		job.ID,
		string(job.Type),
		string(job.TranscriptionMode),
		string(models.ProcessingJobStatusPending),
		job.MaxAttempts,
		job.AvailableAt,
	))
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, models.ErrCallNotFound) {
		// Either the call is not the caller's to restart, or it is not waiting in
		// a cancelled state. Both answers are the same from here.
		return models.Call{}, models.ErrCallNotFound
	}

	return call, err
}

// SwitchCallToTranscriptionOnly drops the analysis half of an already
// transcribed call. Nothing is sent to a provider and nothing is charged: the
// transcript is there, so the call is simply finished.
func (r *Repository) SwitchCallToTranscriptionOnly(ctx context.Context, id, userID uuid.UUID) (models.Call, error) {
	query := fmt.Sprintf(`
	WITH switched AS (
		UPDATE calls c
		SET status = 'transcribed', transcription_only = TRUE
		WHERE c.call_uuid = $1
		  AND c.status IN ('cancelled', 'failed', 'transcribed')
		  AND EXISTS (
		      SELECT 1 FROM call_transcriptions t
		      WHERE t.call_uuid = c.call_uuid AND t.status = 'transcribed' AND t.text IS NOT NULL
		  )
		  AND %s
		RETURNING c.*
	)
	SELECT `+callColumns+`
	FROM switched c
	`, deletableByUserCondition("c", "$2"))

	call, err := scanCallRow(r.db.QueryRowContext(ctx, query, id, userID))
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, models.ErrCallNotFound) {
		return models.Call{}, models.ErrCallNotFound
	}

	return call, err
}
