package call

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	model "verbatrace/monolit/internal/models"
	"verbatrace/monolit/internal/repository/converter"
	repoModel "verbatrace/monolit/internal/repository/models"
	"verbatrace/monolit/internal/repository/scaner"

	"github.com/google/uuid"
)

func (r *Repository) GetByUUID(ctx context.Context, callUUID uuid.UUID, userID uuid.UUID) (model.Call, error) {
	var repoCall repoModel.Call
	getQuery := fmt.Sprintf(`
	SELECT c.call_uuid,
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
	FROM calls c
	WHERE c.call_uuid = $1
	  AND %s
	`, visibleToUserCondition("c", "$2"))

	row := r.db.QueryRowContext(ctx, getQuery, callUUID, userID)

	repoCall, err := scaner.ScanCall(row)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.Call{}, fmt.Errorf("selecting call failed: %w", model.ErrCallNotFound)
		}
		return model.Call{}, fmt.Errorf("selecting call failed: %w", err)
	}

	return converter.RepoCallToModel(repoCall)
}
