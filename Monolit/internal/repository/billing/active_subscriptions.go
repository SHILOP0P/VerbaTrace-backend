package billing

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func (r *Repository) GetActivePersonalSubscription(ctx context.Context, userID uuid.UUID) (models.Subscription, error) {
	query := activeSubscriptionQuery("s.type = 'personal' AND s.user_uuid = $1")
	return r.getSubscription(ctx, query, userID)
}

// GetActiveBusinessSubscription answers with the subscription that covers this
// company. The plan belongs to the owner and covers several companies, so a
// frozen company has no subscription even while its owner keeps paying.
func (r *Repository) GetActiveBusinessSubscription(ctx context.Context, companyID uuid.UUID) (models.Subscription, error) {
	query := activeSubscriptionQuery(`s.type = 'business'
	  AND s.user_uuid IN (
	      SELECT manager_user_uuid
	      FROM companies
	      WHERE company_uuid = $1
	        AND deleted_at IS NULL
	        AND lifecycle_state = 'active'
	  )`)
	return r.getSubscription(ctx, query, companyID)
}

// GetPlanForCompany answers which plan a company is on, whether or not it is
// frozen. Reading in a frozen company works exactly as it does in an active one,
// so anything that only asks "what is this company allowed to see" — reports,
// analytics — has to look the plan up without the lifecycle condition.
func (r *Repository) GetPlanForCompany(ctx context.Context, companyID uuid.UUID) (models.Subscription, error) {
	query := activeSubscriptionQuery(`s.type = 'business'
	  AND s.user_uuid IN (
	      SELECT manager_user_uuid
	      FROM companies
	      WHERE company_uuid = $1
	        AND deleted_at IS NULL
	  )`)
	return r.getSubscription(ctx, query, companyID)
}

func (r *Repository) GetBestActiveBusinessSubscriptionForManager(ctx context.Context, managerID uuid.UUID) (models.Subscription, error) {
	query := activeSubscriptionQuery("s.type = 'business' AND s.user_uuid = $1")
	return r.getSubscription(ctx, query, managerID)
}

func (r *Repository) getSubscription(ctx context.Context, query string, args ...any) (models.Subscription, error) {
	subscription, err := scanSubscription(r.db.QueryRowContext(ctx, query, args...))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.Subscription{}, models.ErrSubscriptionNotFound
		}
		return models.Subscription{}, fmt.Errorf("get subscription: %w", err)
	}

	return subscription, nil
}

func activeSubscriptionQuery(where string) string {
	return `
	SELECT ` + subscriptionColumns("s", "p") + `
	FROM subscriptions s
	JOIN plans p ON p.plan_uuid = s.plan_uuid
	WHERE s.status = 'active'
	  AND s.starts_at <= now()
	  AND (s.ends_at IS NULL OR s.ends_at > now())
	  AND ` + where + `
	`
}
