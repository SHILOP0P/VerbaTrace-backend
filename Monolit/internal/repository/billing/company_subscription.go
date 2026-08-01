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

func (r *Repository) ActivateCompanySubscription(ctx context.Context, input models.ActivateCompanySubscriptionInput, startsAt time.Time) (models.Subscription, error) {
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
	      AND type = 'business'
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
	    SELECT $1, plan_uuid, type, NULL, $3, 'active', $4, NULL
	    FROM selected_plan
	    ON CONFLICT (company_uuid) WHERE status = 'active' AND company_uuid IS NOT NULL
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
		input.CompanyUUID,
		startsAt,
	))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.Subscription{}, models.ErrPlanNotFound
		}
		return models.Subscription{}, fmt.Errorf("activate company subscription: %w", err)
	}

	return subscription, nil
}

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
	      AND company_uuid = $1
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
