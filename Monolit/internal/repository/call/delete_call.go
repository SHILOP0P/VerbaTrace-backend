package call

import (
	"context"
	"fmt"
	"time"

	"verbatrace/monolit/internal/models"
	"verbatrace/monolit/internal/repository/converter"
	repoModel "verbatrace/monolit/internal/repository/models"

	"github.com/google/uuid"
)

// SoftDeleteCall moves a call to the bin. Files and rows stay untouched for the
// grace period so an accidental deletion can be undone, but the queue stops
// working on the call right away.
func (r *Repository) SoftDeleteCall(ctx context.Context, id uuid.UUID, userID uuid.UUID, now time.Time, purgeAfter time.Time) (models.Call, error) {
	// processing_jobs intentionally has no foreign key to calls because the
	// queue supports more than one entity type. Remove jobs belonging to the
	// call in the same statement, otherwise a deleted call can keep consuming a
	// single-worker queue until all of its retries are exhausted.
	query := fmt.Sprintf(`
	WITH deleted_call AS (
		UPDATE calls c
		SET deleted_at = $3,
		    deleted_by_user_uuid = $2,
		    purge_after = $4
		WHERE c.call_uuid = $1
		  AND %s
		RETURNING c.call_uuid
	), deleted_jobs AS (
		DELETE FROM processing_jobs p
		USING deleted_call d
		WHERE p.entity_uuid = d.call_uuid
	)
	SELECT `+callColumns+`
	FROM calls c
	JOIN deleted_call d ON d.call_uuid = c.call_uuid
	`, deletableByUserCondition("c", "$2"))

	row := r.db.QueryRowContext(ctx, query, id, userID, now, purgeAfter)

	return scanCallRow(row)
}

// RestoreCall takes a call back out of the bin. Only the people who could
// delete it may bring it back.
func (r *Repository) RestoreCall(ctx context.Context, id uuid.UUID, userID uuid.UUID) (models.Call, error) {
	query := fmt.Sprintf(`
	WITH restored AS (
		UPDATE calls c
		SET deleted_at = NULL,
		    deleted_by_user_uuid = NULL,
		    purge_after = NULL
		WHERE c.call_uuid = $1
		  AND %s
		RETURNING c.call_uuid
	)
	SELECT `+callColumns+`
	FROM calls c
	JOIN restored ON restored.call_uuid = c.call_uuid
	`, restorableByUserCondition("c", "$2"))

	row := r.db.QueryRowContext(ctx, query, id, userID)

	return scanCallRow(row)
}

// ListDeletedCalls shows the bin to the people who may restore from it: the
// uploader of a personal call, the leader of the department, the deputy and the
// owner of the company.
func (r *Repository) ListDeletedCalls(ctx context.Context, input models.ListDeletedCallsInput) (models.ListDeletedCallsResult, error) {
	limit := input.Limit
	if limit <= 0 {
		limit = 20
	}

	query := fmt.Sprintf(`
	SELECT `+callColumns+`,
	       c.deleted_at,
	       c.purge_after,
	       c.deleted_by_user_uuid,
	       COUNT(*) OVER() AS total
	FROM calls c
	WHERE %s
	ORDER BY c.deleted_at DESC, c.call_uuid DESC
	LIMIT $2 OFFSET $3
	`, deletedVisibleToUserCondition("c", "$1"))

	rows, err := r.db.QueryContext(ctx, query, input.UserID, limit, input.Offset)
	if err != nil {
		return models.ListDeletedCallsResult{}, fmt.Errorf("list deleted calls: %w", err)
	}
	defer func() { _ = rows.Close() }()

	result := models.ListDeletedCallsResult{Items: []models.DeletedCall{}, Limit: limit, Offset: input.Offset}
	for rows.Next() {
		var repoCall repoModel.Call
		var deleted models.DeletedCall
		if err := rows.Scan(
			&repoCall.ID,
			&repoCall.Title,
			&repoCall.Status,
			&repoCall.AudioPath,
			&repoCall.ASRCachePath,
			&repoCall.OriginalFilename,
			&repoCall.MimeType,
			&repoCall.SizeBytes,
			&repoCall.DurationSeconds,
			&repoCall.UploadedByUserUUID,
			&repoCall.CompanyUUID,
			&repoCall.DepartmentUUID,
			&repoCall.VisibilityScope,
			&repoCall.SkipCustomInstructions,
			&repoCall.TranscriptionOnly,
			&repoCall.IsTest,
			&repoCall.CreatedAt,
			&deleted.DeletedAt,
			&deleted.PurgeAfter,
			&deleted.DeletedByUserUUID,
			&result.Total,
		); err != nil {
			return models.ListDeletedCallsResult{}, fmt.Errorf("scan deleted call: %w", err)
		}
		call, err := converter.RepoCallToModel(repoCall)
		if err != nil {
			return models.ListDeletedCallsResult{}, err
		}
		deleted.Call = call
		result.Items = append(result.Items, deleted)
	}
	if err := rows.Err(); err != nil {
		return models.ListDeletedCallsResult{}, fmt.Errorf("list deleted calls: %w", err)
	}

	return result, nil
}
