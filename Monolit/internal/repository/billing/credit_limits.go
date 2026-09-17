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

// SetCompanyCreditLimit records the cap the owner puts on one of their
// companies. A nil limit removes the cap.
func (r *Repository) SetCompanyCreditLimit(ctx context.Context, input models.SetCreditLimitInput) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO company_credit_limits(company_uuid,limit_credits,updated_by_user_uuid,updated_at)
		VALUES($1,$2,$3,now())
		ON CONFLICT (company_uuid) DO UPDATE
		SET limit_credits=EXCLUDED.limit_credits,
		    updated_by_user_uuid=EXCLUDED.updated_by_user_uuid,
		    updated_at=now()`, input.CompanyUUID, input.LimitCredits, input.UserUUID)
	if err != nil {
		return fmt.Errorf("set company credit limit: %w", err)
	}
	return nil
}

func (r *Repository) SetDepartmentCreditLimit(ctx context.Context, input models.SetCreditLimitInput) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO department_credit_limits(department_uuid,company_uuid,limit_credits,updated_by_user_uuid,updated_at)
		VALUES($1,$2,$3,$4,now())
		ON CONFLICT (department_uuid) DO UPDATE
		SET limit_credits=EXCLUDED.limit_credits,
		    updated_by_user_uuid=EXCLUDED.updated_by_user_uuid,
		    updated_at=now()`, input.DepartmentUUID.UUID, input.CompanyUUID, input.LimitCredits, input.UserUUID)
	if err != nil {
		return fmt.Errorf("set department credit limit: %w", err)
	}
	return nil
}

// creditSpendingExpression counts what a subject has taken out of its cap. An
// operation that is still running has not been charged yet but the credits are
// already committed, so the reservation counts until it settles. Leaving it out
// is what let concurrent calls walk straight past the limit together: each one
// saw only what had already been billed.
const creditSpendingExpression = `COALESCE((
	SELECT sum(CASE WHEN status='settled' THEN settled_credits ELSE reserved_credits END)
	FROM usage_operations
	WHERE %s=$1 AND environment='production'
	  AND status IN ('reserved','provider_running','settled','reconciling')
	  AND started_at >= $2 AND started_at < $3
),0)`

// checkCreditLimits refuses the reservation when the department or the company
// cannot afford it this period. A missing limit means no cap of its own, zero
// forbids spending entirely.
//
// The cap is hard: what the operation may cost at most is added to what has
// already been committed before the two are compared. Checking only what was
// spent before let a single expensive call overshoot the cap by any amount,
// because nothing ever refused the operation that did the overshooting.
func checkCreditLimits(ctx context.Context, tx *sql.Tx, input models.ReserveCreditsInput, periodStart, periodEnd time.Time) error {
	if input.DepartmentUUID.Valid {
		exceeded, err := limitExceeded(ctx, tx, `
			SELECT (SELECT limit_credits FROM department_credit_limits WHERE department_uuid=$1),
			       `+fmt.Sprintf(creditSpendingExpression, "department_uuid"),
			input.DepartmentUUID.UUID, periodStart, periodEnd, input.MaximumCharge)
		if err != nil {
			return err
		}
		if exceeded {
			return models.ErrDepartmentCreditLimitExceeded
		}
	}

	exceeded, err := limitExceeded(ctx, tx, `
		SELECT (SELECT limit_credits FROM company_credit_limits WHERE company_uuid=$1),
		       `+fmt.Sprintf(creditSpendingExpression, "company_uuid"),
		input.CompanyUUID.UUID, periodStart, periodEnd, input.MaximumCharge)
	if err != nil {
		return err
	}
	if exceeded {
		return models.ErrCompanyCreditLimitExceeded
	}

	return nil
}

func limitExceeded(ctx context.Context, tx *sql.Tx, query string, subjectID uuid.UUID, start, end time.Time, maximumCharge int64) (bool, error) {
	var limit sql.NullInt64
	var used int64
	if err := tx.QueryRowContext(ctx, query, subjectID, start, end).Scan(&limit, &used); err != nil {
		return false, fmt.Errorf("read credit limit: %w", err)
	}
	if !limit.Valid {
		return false, nil
	}
	if maximumCharge < 0 {
		maximumCharge = 0
	}

	return used+maximumCharge > limit.Int64, nil
}

// CompanyCreditSpending reports what the company spent in the given period and
// what its pace projects to by the end of it. The period is decided by the
// caller, because it follows the owner's subscription rather than the calendar.
func (r *Repository) CompanyCreditSpending(ctx context.Context, companyID uuid.UUID, period models.CreditPeriod, now time.Time) (models.CreditSpending, error) {
	spending := models.CreditSpending{SubjectUUID: companyID, PeriodStart: period.Start, PeriodEnd: period.End}

	var limit sql.NullInt64
	err := r.db.QueryRowContext(ctx, `
		SELECT c.name,
		       (SELECT limit_credits FROM company_credit_limits l WHERE l.company_uuid=c.company_uuid),
		       COALESCE((
		           SELECT sum(CASE WHEN o.status='settled' THEN o.settled_credits ELSE o.reserved_credits END)
		           FROM usage_operations o
		           WHERE o.company_uuid=c.company_uuid AND o.environment='production'
		             AND o.status IN ('reserved','provider_running','settled','reconciling')
		             AND o.started_at >= $2 AND o.started_at < $3
		       ),0)
		FROM companies c
		WHERE c.company_uuid=$1`, companyID, period.Start, period.End).Scan(&spending.SubjectName, &limit, &spending.UsedCredits)
	if errors.Is(err, sql.ErrNoRows) {
		return models.CreditSpending{}, models.ErrCompanyNotFound
	}
	if err != nil {
		return models.CreditSpending{}, fmt.Errorf("company credit spending: %w", err)
	}
	if limit.Valid {
		value := limit.Int64
		spending.LimitCredits = &value
	}
	spending.ForecastCredits = forecastCredits(spending.UsedCredits, period, now)

	return spending, nil
}

// DepartmentCreditSpending reports the same numbers per department.
func (r *Repository) DepartmentCreditSpending(ctx context.Context, companyID uuid.UUID, period models.CreditPeriod, now time.Time) ([]models.CreditSpending, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT d.department_uuid,
		       d.name,
		       (SELECT limit_credits FROM department_credit_limits l WHERE l.department_uuid=d.department_uuid),
		       COALESCE((
		           SELECT sum(CASE WHEN o.status='settled' THEN o.settled_credits ELSE o.reserved_credits END)
		           FROM usage_operations o
		           WHERE o.department_uuid=d.department_uuid AND o.environment='production'
		             AND o.status IN ('reserved','provider_running','settled','reconciling')
		             AND o.started_at >= $2 AND o.started_at < $3
		       ),0)
		FROM departments d
		WHERE d.company_uuid=$1 AND d.deleted_at IS NULL
		ORDER BY d.name`, companyID, period.Start, period.End)
	if err != nil {
		return nil, fmt.Errorf("department credit spending: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items := []models.CreditSpending{}
	for rows.Next() {
		item := models.CreditSpending{PeriodStart: period.Start, PeriodEnd: period.End}
		var limit sql.NullInt64
		if err := rows.Scan(&item.SubjectUUID, &item.SubjectName, &limit, &item.UsedCredits); err != nil {
			return nil, fmt.Errorf("scan department credit spending: %w", err)
		}
		if limit.Valid {
			value := limit.Int64
			item.LimitCredits = &value
		}
		item.ForecastCredits = forecastCredits(item.UsedCredits, period, now)
		items = append(items, item)
	}

	return items, rows.Err()
}

// forecastMinimumElapsed is how much of a period has to pass before its pace
// means anything. Without it the first minutes divide by almost nothing: one
// call in the first second of a thirty-day period projects to two and a half
// billion credits, and that is the number the owner would be shown.
const forecastMinimumElapsed = 24 * time.Hour

// forecastCredits extends the pace of the period that has already passed over
// the whole period. Before anything is spent there is nothing to project.
func forecastCredits(used int64, period models.CreditPeriod, now time.Time) int64 {
	if used <= 0 {
		return 0
	}
	elapsed := now.UTC().Sub(period.Start)
	total := period.End.Sub(period.Start)
	if elapsed <= 0 || total <= 0 || elapsed >= total {
		return used
	}
	if elapsed < forecastMinimumElapsed {
		return used
	}

	return int64(float64(used) * (float64(total) / float64(elapsed)))
}
