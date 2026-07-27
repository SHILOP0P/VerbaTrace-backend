package call

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	model "calllens/monolit/internal/models"
	"calllens/monolit/internal/repository/converter"
	repoModel "calllens/monolit/internal/repository/models"
	"calllens/monolit/internal/repository/scaner"

	"github.com/google/uuid"
)

func (r *Repository) GetByUUIDForProcessing(ctx context.Context, callUUID uuid.UUID) (model.Call, error) {
	query := `
	SELECT call_uuid,
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
	FROM calls
	WHERE call_uuid = $1
	`

	row := r.db.QueryRowContext(ctx, query, callUUID)

	var repoCall repoModel.Call
	repoCall, err := scaner.ScanCall(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.Call{}, model.ErrCallNotFound
		}

		return model.Call{}, fmt.Errorf("get call by uuid for processing: %w", err)
	}

	return converter.RepoCallToModel(repoCall)
}
