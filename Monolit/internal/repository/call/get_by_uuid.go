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

// GetByUUID returns a call the user may see.
func (r *Repository) GetByUUID(ctx context.Context, callUUID uuid.UUID, userID uuid.UUID) (model.Call, error) {
	return r.getWhere(ctx, callUUID, userID, visibleToUserCondition("c", "$2"))
}

// GetEditableByUUID returns a call the user may change. Every operation that
// changes a call or its transcript checks it with this rather than with
// visibility: an employee marked in a call reads it and nothing more.
func (r *Repository) GetEditableByUUID(ctx context.Context, callUUID uuid.UUID, userID uuid.UUID) (model.Call, error) {
	call, err := r.getWhere(ctx, callUUID, userID, editableByUserCondition("c", "$2"))
	if errors.Is(err, model.ErrCallNotFound) {
		// A call the user sees but may not change is refused, not hidden.
		if _, visibleErr := r.GetByUUID(ctx, callUUID, userID); visibleErr == nil {
			return model.Call{}, fmt.Errorf("editing call refused: %w", model.ErrForbidden)
		}
	}
	return call, err
}

// GetAccess says how the user reaches a visible call and whether they may
// change it.
func (r *Repository) GetAccess(ctx context.Context, callUUID uuid.UUID, userID uuid.UUID) (model.CallAccess, error) {
	var access model.CallAccess
	var uploader bool
	// Whom a company call counts for is set by management alone, not by the
	// uploader: otherwise one could move a weak call onto a colleague.
	err := r.db.QueryRowContext(ctx, fmt.Sprintf(`
	SELECT c.uploaded_by_user_uuid IS NOT DISTINCT FROM $2, %s, c.company_uuid IS NOT NULL AND %s
	FROM calls c
	WHERE c.call_uuid = $1 AND %s`, editableByUserCondition("c", "$2"), scopeManagementCondition("c", "$2"), visibleToUserCondition("c", "$2")),
		callUUID, userID).Scan(&uploader, &access.CanEdit, &access.CanManageSubjects)
	if errors.Is(err, sql.ErrNoRows) {
		return model.CallAccess{}, fmt.Errorf("reading call access failed: %w", model.ErrCallNotFound)
	}
	if err != nil {
		return model.CallAccess{}, fmt.Errorf("reading call access failed: %w", err)
	}
	switch {
	case uploader:
		access.Via = model.CallAccessViaUploader
	case access.CanEdit:
		access.Via = model.CallAccessViaManagement
	default:
		access.Via = model.CallAccessViaSubject
	}
	return access, nil
}

func (r *Repository) getWhere(ctx context.Context, callUUID uuid.UUID, userID uuid.UUID, condition string) (model.Call, error) {
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
	`, condition)

	row := r.db.QueryRowContext(ctx, getQuery, callUUID, userID)

	var repoCall repoModel.Call
	repoCall, err := scaner.ScanCall(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.Call{}, fmt.Errorf("selecting call failed: %w", model.ErrCallNotFound)
		}
		return model.Call{}, fmt.Errorf("selecting call failed: %w", err)
	}

	return converter.RepoCallToModel(repoCall)
}
