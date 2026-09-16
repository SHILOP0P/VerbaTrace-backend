package call

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

// CanManageCall answers whether the user is responsible for the call rather
// than merely able to see it: the uploader of a personal call, the leader of
// its department, the deputy and the owner of its company. Deleting a call, a
// report or anything else attached to it reuses this one answer.
func (r *Repository) CanManageCall(ctx context.Context, callID uuid.UUID, userID uuid.UUID) (bool, error) {
	query := fmt.Sprintf(`
	SELECT EXISTS (
		SELECT 1
		FROM calls c
		WHERE c.call_uuid = $1
		  AND %s
	)`, deletableByUserCondition("c", "$2"))

	var allowed bool
	if err := r.db.QueryRowContext(ctx, query, callID, userID).Scan(&allowed); err != nil {
		return false, fmt.Errorf("check call management rights: %w", err)
	}

	return allowed, nil
}
