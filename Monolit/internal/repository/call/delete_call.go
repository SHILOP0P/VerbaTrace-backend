package call

import (
	"context"
	"fmt"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func (r *Repository) DeleteCall(ctx context.Context, id uuid.UUID, userID uuid.UUID) error {
	// processing_jobs intentionally has no foreign key to calls because the
	// queue supports more than one entity type. Remove jobs belonging to the
	// call in the same statement, otherwise a deleted call can keep consuming a
	// single-worker queue until all of its retries are exhausted.
	queryDel := fmt.Sprintf(`
	WITH deleted_call AS (
		DELETE FROM calls c
		WHERE c.call_uuid = $1
		  AND %s
		RETURNING c.call_uuid
	), deleted_jobs AS (
		DELETE FROM processing_jobs p
		USING deleted_call c
		WHERE p.entity_uuid = c.call_uuid
	)
	SELECT EXISTS (SELECT 1 FROM deleted_call)
	`, visibleToUserCondition("c", "$2"))

	var deleted bool
	err := r.db.QueryRowContext(ctx, queryDel, id, userID).Scan(&deleted)
	if err != nil {
		return fmt.Errorf("delete call: %w", err)
	}
	if !deleted {
		return models.ErrCallNotFound
	}

	return nil
}
