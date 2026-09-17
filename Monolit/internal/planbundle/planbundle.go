// Package planbundle keeps one promise in one place: a business plan is sold
// together with a personal plan of the same tier, so whoever owns the business
// plan also holds that personal plan for as long as the business plan runs.
//
// It lives outside the repositories because two of them need it — the admin
// grant and the ownership transfer — and neither should depend on the other.
package planbundle

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

// Tx is the part of *sql.Tx this package uses.
type Tx interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// Ensure gives the owner the personal plan that comes with their business plan.
//
// Only one personal subscription may be active at a time, which the database
// enforces, so the two cannot simply sit side by side. When the person already
// holds a personal plan, the result keeps the stronger tier of the two and runs
// until the later of the two end dates: the plan they paid for is not weakened,
// and the bundled one carries on after it up to the end of the business plan.
//
// An open-ended personal plan is never shortened.
func Ensure(ctx context.Context, tx Tx, ownerUser uuid.UUID, businessPlan models.PlanCode, businessEndsAt *time.Time, now time.Time) error {
	bundled, ok := businessPlan.BundledPersonalPlan()
	if !ok {
		return nil
	}

	bundledPlanID, err := planID(ctx, tx, bundled)
	if err != nil {
		return err
	}

	var currentID, currentPlanID uuid.UUID
	var currentCode string
	var currentEnds sql.NullTime
	err = tx.QueryRowContext(ctx, `
		SELECT s.subscription_uuid, s.plan_uuid, p.code, s.ends_at
		FROM subscriptions s
		JOIN plans p ON p.plan_uuid = s.plan_uuid
		WHERE s.user_uuid = $1 AND s.type = 'personal' AND s.status = 'active'
		LIMIT 1
	`, ownerUser).Scan(&currentID, &currentPlanID, &currentCode, &currentEnds)
	if errors.Is(err, sql.ErrNoRows) {
		id, newErr := uuid.NewV7()
		if newErr != nil {
			return fmt.Errorf("new bundled personal subscription id: %w", newErr)
		}
		if _, err = tx.ExecContext(ctx, `
			INSERT INTO subscriptions (subscription_uuid, plan_uuid, type, user_uuid, company_uuid, status, starts_at, ends_at)
			VALUES ($1, $2, 'personal', $3, NULL, 'active', $4, $5)
		`, id, bundledPlanID, ownerUser, now, timeOrNil(businessEndsAt)); err != nil {
			return fmt.Errorf("grant bundled personal plan: %w", err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("read current personal plan: %w", err)
	}

	targetPlanID := currentPlanID
	if bundled.Tier() > models.PlanCode(currentCode).Tier() {
		targetPlanID = bundledPlanID
	}

	targetEnds := currentEnds
	switch {
	case !currentEnds.Valid:
		// Already open-ended, and nothing the bundle adds can beat that.
	case businessEndsAt == nil:
		targetEnds = sql.NullTime{}
	case businessEndsAt.After(currentEnds.Time):
		targetEnds = sql.NullTime{Time: *businessEndsAt, Valid: true}
	}

	if targetPlanID == currentPlanID && targetEnds == currentEnds {
		return nil
	}

	if _, err = tx.ExecContext(ctx, `
		UPDATE subscriptions
		SET plan_uuid = $2, ends_at = $3, updated_at = now()
		WHERE subscription_uuid = $1
	`, currentID, targetPlanID, timeOrNilFromNull(targetEnds)); err != nil {
		return fmt.Errorf("extend bundled personal plan: %w", err)
	}

	return nil
}

// Transfer hands the business plan itself to another person. It is the money
// half of an ownership transfer: the plan keeps its tier and the remainder of
// its period, and the new owner also picks up the personal plan that comes with
// it.
//
// It answers with the plan that moved, so the caller can record what happened,
// and with false when there was no business plan to move.
func Transfer(ctx context.Context, tx Tx, fromUser, toUser uuid.UUID, now time.Time) (models.PlanCode, bool, error) {
	var subscriptionID uuid.UUID
	var code string
	var endsAt sql.NullTime
	err := tx.QueryRowContext(ctx, `
		SELECT s.subscription_uuid, p.code, s.ends_at
		FROM subscriptions s
		JOIN plans p ON p.plan_uuid = s.plan_uuid
		WHERE s.user_uuid = $1 AND s.type = 'business' AND s.status = 'active'
		FOR UPDATE
	`, fromUser).Scan(&subscriptionID, &code, &endsAt)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("lock business plan for transfer: %w", err)
	}

	if _, err = tx.ExecContext(ctx, `
		UPDATE subscriptions SET user_uuid = $2, updated_at = now() WHERE subscription_uuid = $1
	`, subscriptionID, toUser); err != nil {
		return "", false, fmt.Errorf("move business plan: %w", err)
	}

	var ends *time.Time
	if endsAt.Valid {
		value := endsAt.Time
		ends = &value
	}
	if err = Ensure(ctx, tx, toUser, models.PlanCode(code), ends, now); err != nil {
		return "", false, err
	}

	return models.PlanCode(code), true, nil
}

// HoldsBusinessPlan reports whether the person already pays for a business plan.
// Ownership is refused in that case: one person never holds two of them, and the
// database says so too.
func HoldsBusinessPlan(ctx context.Context, tx Tx, userID uuid.UUID) (bool, error) {
	var exists bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM subscriptions
			WHERE user_uuid = $1 AND type = 'business' AND status = 'active'
		)
	`, userID).Scan(&exists); err != nil {
		return false, fmt.Errorf("check business plan: %w", err)
	}

	return exists, nil
}

func planID(ctx context.Context, tx Tx, code models.PlanCode) (uuid.UUID, error) {
	var id uuid.UUID
	err := tx.QueryRowContext(ctx, `SELECT plan_uuid FROM plans WHERE code = $1`, code).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return uuid.Nil, models.ErrPlanNotFound
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("read plan %q: %w", code, err)
	}

	return id, nil
}

func timeOrNil(value *time.Time) any {
	if value == nil {
		return nil
	}

	return *value
}

func timeOrNilFromNull(value sql.NullTime) any {
	if !value.Valid {
		return nil
	}

	return value.Time
}
