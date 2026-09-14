package processing_job

import (
	"context"

	"github.com/google/uuid"
)

// HeartbeatByEntity keeps a long staged analysis owned by its current worker.
// Provider calls are bounded to five minutes, and every completed step refreshes
// the lock before the stale-job threshold can elapse.
func (r *Repository) HeartbeatByEntity(ctx context.Context, entityID uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE processing_jobs
		SET locked_at=now(), updated_at=now()
		WHERE entity_uuid=$1 AND status='running'
	`, entityID)
	return err
}
