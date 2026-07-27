package call

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"calllens/monolit/internal/models"
	"calllens/monolit/internal/repository/converter"
	repoModel "calllens/monolit/internal/repository/models"
	"calllens/monolit/internal/repository/scaner"
)

func (r *Repository) TakeNextForProcessing(ctx context.Context) (models.Call, error) {
	query := `
	UPDATE calls
	SET status = $1
	WHERE call_uuid = (
		SELECT call_uuid
		FROM calls
		WHERE status = $2
		ORDER BY created_at
		FOR UPDATE SKIP LOCKED
		LIMIT 1
	)
	RETURNING call_uuid,
	          title,
	          status,
	          audio_path,
	          asr_cache_path,
	          original_filename,
	          mime_type,
	          size_bytes,
	          duration_seconds,
	          uploaded_by_user_uuid,
	          company_uuid,
	          department_uuid,
	          visibility_scope,
	          skip_custom_instructions,
	          created_at
	`

	row := r.db.QueryRowContext(
		ctx,
		query,
		string(models.CallStatusProcessing),
		string(models.CallStatusNew),
	)

	var repoCall repoModel.Call
	repoCall, err := scaner.ScanCall(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.Call{}, models.ErrNoCallsForProcessing
		}

		return models.Call{}, fmt.Errorf("take next call for processing: %w", err)
	}

	return converter.RepoCallToModel(repoCall)
}
