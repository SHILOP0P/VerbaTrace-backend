package call

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

// CancelCallProcessing stops the queue from working on a call and leaves the
// call itself intact. The job rows go in the same statement, for the same reason
// deletion removes them: a cancelled call must not keep a single-worker queue
// busy until its retries run out.
//
// Only the people who could delete the call may cancel it. An employee neither
// deletes nor cancels a company call.
func (r *Repository) CancelCallProcessing(ctx context.Context, id, userID uuid.UUID) (models.Call, error) {
	// The final SELECT has to read the row the UPDATE produced. Joining back to
	// calls would see the snapshot from before the statement and report the old
	// status, so the updated row is carried through the CTE instead.
	query := fmt.Sprintf(`
	WITH cancelled AS (
		UPDATE calls c
		SET status = 'cancelled'
		WHERE c.call_uuid = $1
		  AND c.status IN ('new', 'processing', 'awaiting_credits')
		  AND %s
		RETURNING c.*
	), cancelled_jobs AS (
		DELETE FROM processing_jobs p
		USING cancelled n
		WHERE p.entity_uuid = n.call_uuid
	), cancelled_transcription AS (
		UPDATE call_transcriptions t
		SET status = 'failed', error_message = 'processing cancelled', updated_at = now()
		FROM cancelled n
		WHERE t.call_uuid = n.call_uuid AND t.status = 'processing'
	), cancelled_analysis AS (
		UPDATE call_analyses a
		SET status = 'failed', error_message = 'processing cancelled', updated_at = now()
		FROM cancelled n
		WHERE a.call_uuid = n.call_uuid AND a.status IN ('pending', 'processing')
	)
	SELECT `+callColumns+`
	FROM cancelled c
	`, deletableByUserCondition("c", "$2"))

	call, err := scanCallRow(r.db.QueryRowContext(ctx, query, id, userID))
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, models.ErrCallNotFound) {
		// Either the call is not the caller's to cancel, or the queue already
		// finished with it. Both answers are the same from here.
		return models.Call{}, models.ErrCallNotFound
	}

	return call, err
}
