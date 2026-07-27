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

	"github.com/google/uuid"
)

func (r *Repository) UpdateCallStatus(ctx context.Context, id uuid.UUID, status models.CallStatus) (models.Call, error) {
	query := `
	UPDATE calls
	SET status = $2
	WHERE call_uuid = $1
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

	row := r.db.QueryRowContext(ctx, query, id, string(status))

	var repoCall repoModel.Call
	repoCall, err := scaner.ScanCall(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.Call{}, models.ErrCallNotFound
		}

		return models.Call{}, fmt.Errorf("update call status: %w", err)
	}

	return converter.RepoCallToModel(repoCall)
}
