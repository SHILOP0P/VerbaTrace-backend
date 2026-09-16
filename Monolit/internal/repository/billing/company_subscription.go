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

// ActivateCompanySubscription grants a business plan for a company, which means
// granting it to the owner of that company: one plan covers every company they
// own, up to the number the plan allows.
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
	owner AS (
	    SELECT manager_user_uuid
	    FROM companies
	    WHERE company_uuid = $3
	      AND deleted_at IS NULL
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
	    SELECT $1, selected_plan.plan_uuid, selected_plan.type, owner.manager_user_uuid, NULL, 'active', $4, NULL
	    FROM selected_plan, owner
	    ON CONFLICT (type, user_uuid) WHERE status = 'active' AND user_uuid IS NOT NULL
	    DO UPDATE SET plan_uuid = EXCLUDED.plan_uuid,
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
