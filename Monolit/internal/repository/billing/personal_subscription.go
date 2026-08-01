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

func (r *Repository) ActivatePersonalSubscription(ctx context.Context, input models.ActivatePersonalSubscriptionInput, startsAt time.Time) (models.Subscription, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return models.Subscription{}, fmt.Errorf("generate subscription uuid: %w", err)
	}

	if startsAt.IsZero() {
		startsAt = time.Now().UTC()
	}

	query := `
	WITH selected_plan AS (
	    SELECT plan_uuid, type
	    FROM plans
	    WHERE code = $2
	      AND type = 'personal'
	),
	upserted AS (
	    INSERT INTO subscriptions (
	        subscription_uuid,
	        plan_uuid,
	        type,
	        user_uuid,
	        company_uuid,
	        status,
	        starts_at,
	        ends_at
	    )
	    SELECT $1, plan_uuid, type, $3, NULL, 'active', $4, NULL
	    FROM selected_plan
	    ON CONFLICT (type, user_uuid) WHERE status = 'active' AND user_uuid IS NOT NULL
	    DO UPDATE SET plan_uuid = EXCLUDED.plan_uuid,
	                  type = EXCLUDED.type,
	                  status = 'active',
	                  starts_at = EXCLUDED.starts_at,
	                  ends_at = NULL,
	                  updated_at = now()
	    RETURNING *
	)
	SELECT ` + subscriptionColumns("u", "p") + `
	FROM upserted u
	JOIN plans p ON p.plan_uuid = u.plan_uuid
	`

	subscription, err := scanSubscription(r.db.QueryRowContext(
		ctx,
		query,
		id,
		input.PlanCode,
		input.UserUUID,
		startsAt,
	))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.Subscription{}, models.ErrPlanNotFound
		}
		return models.Subscription{}, fmt.Errorf("activate personal subscription: %w", err)
	}

	return subscription, nil
}
