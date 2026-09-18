package call

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"verbatrace/monolit/internal/models"
	"verbatrace/monolit/internal/repository/converter"
	repoModel "verbatrace/monolit/internal/repository/models"
	"verbatrace/monolit/internal/repository/scaner"

	"github.com/google/uuid"
)

func (r *Repository) UpdateCallTitle(ctx context.Context, id uuid.UUID, userID uuid.UUID, title string) (models.Call, error) {
	var repoCall repoModel.Call

	queryUpdate := fmt.Sprintf(`
	UPDATE calls c
	SET title = $3
	WHERE c.call_uuid = $1
	  AND %s
	RETURNING c.call_uuid,
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
	          transcription_only,
	          EXISTS (SELECT 1 FROM ingest_items i JOIN developer_applications a USING(application_uuid) WHERE i.ingest_item_uuid=c.ingest_item_uuid AND a.environment='sandbox') AS is_test,
	          created_at
	`, editableByUserCondition("c", "$2"))

	row := r.db.QueryRowContext(ctx, queryUpdate, id, userID, title)

	repoCall, err := scaner.ScanCall(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// A call the user sees but may not change is refused, not hidden.
			if _, visibleErr := r.GetByUUID(ctx, id, userID); visibleErr == nil {
				return models.Call{}, models.ErrForbidden
			}
			return models.Call{}, models.ErrCallNotFound
		}
		return models.Call{}, fmt.Errorf("update call title failed: %w", err)
	}

	return converter.RepoCallToModel(repoCall)
}
