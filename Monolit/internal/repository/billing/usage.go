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

func (r *Repository) GetUsageCounter(ctx context.Context, subscriptionID uuid.UUID, periodStart time.Time) (models.UsageCounter, error) {
	query := `
	SELECT usage_counter_uuid,
	       subscription_uuid,
	       period_start,
	       period_end,
	       used_minutes,
	       created_at,
	       updated_at
	FROM usage_counters
	WHERE subscription_uuid = $1
	  AND period_start = $2
	`

	counter, err := scanUsageCounter(r.db.QueryRowContext(ctx, query, subscriptionID, monthStart(periodStart)))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.UsageCounter{}, models.ErrSubscriptionNotFound
		}
		return models.UsageCounter{}, fmt.Errorf("get usage counter: %w", err)
	}

	return counter, nil
}

func (r *Repository) AddUsageMinutes(ctx context.Context, subscriptionID uuid.UUID, periodStart time.Time, minutes int) (models.UsageCounter, error) {
	if minutes <= 0 {
		return r.ensureUsageCounter(ctx, subscriptionID, periodStart)
	}

	id, err := uuid.NewV7()
	if err != nil {
		return models.UsageCounter{}, fmt.Errorf("generate usage counter uuid: %w", err)
	}

	start := monthStart(periodStart)
	end := start.AddDate(0, 1, 0)

	query := `
	INSERT INTO usage_counters (
	    usage_counter_uuid,
	    subscription_uuid,
	    period_start,
	    period_end,
	    used_minutes
	)
	VALUES ($1, $2, $3, $4, $5)
	ON CONFLICT (subscription_uuid, period_start)
	DO UPDATE SET used_minutes = usage_counters.used_minutes + EXCLUDED.used_minutes,
	              updated_at = now()
	RETURNING usage_counter_uuid,
	          subscription_uuid,
	          period_start,
	          period_end,
	          used_minutes,
	          created_at,
	          updated_at
	`

	counter, err := scanUsageCounter(r.db.QueryRowContext(ctx, query, id, subscriptionID, start, end, minutes))
	if err != nil {
		return models.UsageCounter{}, fmt.Errorf("add usage minutes: %w", err)
	}

	return counter, nil
}

func (r *Repository) ensureUsageCounter(ctx context.Context, subscriptionID uuid.UUID, periodStart time.Time) (models.UsageCounter, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return models.UsageCounter{}, fmt.Errorf("generate usage counter uuid: %w", err)
	}

	start := monthStart(periodStart)
	end := start.AddDate(0, 1, 0)

	query := `
	INSERT INTO usage_counters (
	    usage_counter_uuid,
	    subscription_uuid,
	    period_start,
	    period_end,
	    used_minutes
	)
	VALUES ($1, $2, $3, $4, 0)
	ON CONFLICT (subscription_uuid, period_start)
	DO UPDATE SET updated_at = usage_counters.updated_at
	RETURNING usage_counter_uuid,
	          subscription_uuid,
	          period_start,
	          period_end,
	          used_minutes,
	          created_at,
	          updated_at
	`

	counter, err := scanUsageCounter(r.db.QueryRowContext(ctx, query, id, subscriptionID, start, end))
	if err != nil {
		return models.UsageCounter{}, fmt.Errorf("ensure usage counter: %w", err)
	}

	return counter, nil
}

func (r *Repository) CountUsedMinutes(ctx context.Context, subscriptionID uuid.UUID, periodStart time.Time) (int, error) {
	query := `
	SELECT COALESCE(used_minutes, 0)
	FROM usage_counters
	WHERE subscription_uuid = $1
	  AND period_start = $2
	`

	var usedMinutes int
	if err := r.db.QueryRowContext(ctx, query, subscriptionID, monthStart(periodStart)).Scan(&usedMinutes); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, nil
		}
		return 0, fmt.Errorf("count used minutes: %w", err)
	}

	return usedMinutes, nil
}

type usageCounterScanner interface {
	Scan(dest ...any) error
}

func scanUsageCounter(row usageCounterScanner) (models.UsageCounter, error) {
	var counter models.UsageCounter
	if err := row.Scan(
		&counter.ID,
		&counter.SubscriptionUUID,
		&counter.PeriodStart,
		&counter.PeriodEnd,
		&counter.UsedMinutes,
		&counter.CreatedAt,
		&counter.UpdatedAt,
	); err != nil {
		return models.UsageCounter{}, err
	}

	return counter, nil
}

func monthStart(value time.Time) time.Time {
	value = value.UTC()
	return time.Date(value.Year(), value.Month(), 1, 0, 0, 0, 0, time.UTC)
}
