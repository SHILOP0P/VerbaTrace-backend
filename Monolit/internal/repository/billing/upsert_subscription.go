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

func (r *Repository) UpsertSubscription(ctx context.Context, input models.UpsertSubscriptionInput) (models.Subscription, error) {
	if input.ID == uuid.Nil {
		id, err := uuid.NewV7()
		if err != nil {
			return models.Subscription{}, fmt.Errorf("generate subscription uuid: %w", err)
		}
		input.ID = id
	}

	if input.Status == "" {
		input.Status = models.SubscriptionStatusActive
	}

	if input.StartsAt.IsZero() {
		input.StartsAt = time.Now().UTC()
	}

	query := `
	WITH selected_plan AS (
	    SELECT plan_uuid, type
	    FROM plans
	    WHERE code = $2
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
	    SELECT $1, plan_uuid, type, $3, $4, $5, $6, $7
	    FROM selected_plan
	    ON CONFLICT (subscription_uuid)
	    DO UPDATE SET plan_uuid = EXCLUDED.plan_uuid,
	                  type = EXCLUDED.type,
	                  user_uuid = EXCLUDED.user_uuid,
	                  company_uuid = EXCLUDED.company_uuid,
	                  status = EXCLUDED.status,
	                  starts_at = EXCLUDED.starts_at,
	                  ends_at = EXCLUDED.ends_at,
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
		input.ID,
		input.PlanCode,
		nullableUUID(input.UserUUID),
		nullableUUID(input.CompanyUUID),
		input.Status,
		input.StartsAt,
		input.EndsAt,
	))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.Subscription{}, models.ErrPlanNotFound
		}
		return models.Subscription{}, fmt.Errorf("upsert subscription: %w", err)
	}

	return subscription, nil
}
