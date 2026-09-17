package billing

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

// CancelCompanySubscription cancels the owner's business plan, which stops every
// company they own.
func (r *Repository) CancelCompanySubscription(ctx context.Context, companyID uuid.UUID, canceledAt time.Time) (models.Subscription, error) {
	if canceledAt.IsZero() {
		canceledAt = time.Now().UTC()
	}

	query := `
	WITH canceled AS (
	    UPDATE subscriptions
	    SET status = 'canceled',
	        ends_at = CASE
	            WHEN $2 > starts_at THEN $2
	            ELSE starts_at + INTERVAL '1 second'
	        END,
	        updated_at = now()
	    WHERE type = 'business'
	      AND user_uuid IN (SELECT manager_user_uuid FROM companies WHERE company_uuid = $1 AND deleted_at IS NULL)
	      AND status = 'active'
	      AND starts_at <= $2
	      AND (ends_at IS NULL OR ends_at > $2)
	    RETURNING *
	)
	SELECT ` + subscriptionColumns("c", "p") + `
	FROM canceled c
	JOIN plans p ON p.plan_uuid = c.plan_uuid
	`

	subscription, err := scanSubscription(r.db.QueryRowContext(ctx, query, companyID, canceledAt))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.Subscription{}, models.ErrSubscriptionNotFound
		}
		return models.Subscription{}, fmt.Errorf("cancel company subscription: %w", err)
	}

	return subscription, nil
}
