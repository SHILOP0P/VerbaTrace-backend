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

// checkCreditLimits refuses the reservation when the department or the company
// has already spent what it was allowed this period. A missing limit means no
// cap of its own, zero forbids spending entirely.
func checkCreditLimits(ctx context.Context, tx *sql.Tx, input models.ReserveCreditsInput, now time.Time) error {
	start, end := creditPeriod(now)

	if input.DepartmentUUID.Valid {
		exceeded, err := limitExceeded(ctx, tx, `
			SELECT (SELECT limit_credits FROM department_credit_limits WHERE department_uuid=$1),
			       COALESCE((SELECT sum(settled_credits) FROM usage_operations
			           WHERE department_uuid=$1 AND status='settled' AND environment='production'
			             AND completed_at >= $2 AND completed_at < $3),0)`, input.DepartmentUUID.UUID, start, end)
		if err != nil {
			return err
		}
		if exceeded {
			return models.ErrDepartmentCreditLimitExceeded
		}
	}

	exceeded, err := limitExceeded(ctx, tx, `
		SELECT (SELECT limit_credits FROM company_credit_limits WHERE company_uuid=$1),
		       COALESCE((SELECT sum(settled_credits) FROM usage_operations
		           WHERE company_uuid=$1 AND status='settled' AND environment='production'
		             AND completed_at >= $2 AND completed_at < $3),0)`, input.CompanyUUID.UUID, start, end)
	if err != nil {
		return err
	}
	if exceeded {
		return models.ErrCompanyCreditLimitExceeded
	}

	return nil
}

func limitExceeded(ctx context.Context, tx *sql.Tx, query string, subjectID uuid.UUID, start, end time.Time) (bool, error) {
	var limit sql.NullInt64
	var used int64
	if err := tx.QueryRowContext(ctx, query, subjectID, start, end).Scan(&limit, &used); err != nil {
		return false, fmt.Errorf("read credit limit: %w", err)
	}
	if !limit.Valid {
		return false, nil
	}

	return used >= limit.Int64, nil
}

// CompanyCreditSpending reports what the company spent in the current period
// and what its pace projects to by the end of it.
func (r *Repository) CompanyCreditSpending(ctx context.Context, companyID uuid.UUID, now time.Time) (models.CreditSpending, error) {
	start, end := creditPeriod(now)
	spending := models.CreditSpending{SubjectUUID: companyID, PeriodStart: start, PeriodEnd: end}

	var limit sql.NullInt64
	err := r.db.QueryRowContext(ctx, `
		SELECT c.name,
		       (SELECT limit_credits FROM company_credit_limits l WHERE l.company_uuid=c.company_uuid),
		       COALESCE((
		           SELECT sum(settled_credits) FROM usage_operations o
		           WHERE o.company_uuid=c.company_uuid AND o.status='settled' AND o.environment='production'
		             AND o.completed_at >= $2 AND o.completed_at < $3
		       ),0)
		FROM companies c
		WHERE c.company_uuid=$1`, companyID, start, end).Scan(&spending.SubjectName, &limit, &spending.UsedCredits)
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
	spending.ForecastCredits = forecastCredits(spending.UsedCredits, start, end, now)

	return spending, nil
}

// DepartmentCreditSpending reports the same numbers per department.
func (r *Repository) DepartmentCreditSpending(ctx context.Context, companyID uuid.UUID, now time.Time) ([]models.CreditSpending, error) {
	start, end := creditPeriod(now)
	rows, err := r.db.QueryContext(ctx, `
		SELECT d.department_uuid,
		       d.name,
		       (SELECT limit_credits FROM department_credit_limits l WHERE l.department_uuid=d.department_uuid),
		       COALESCE((
		           SELECT sum(settled_credits) FROM usage_operations o
		           WHERE o.department_uuid=d.department_uuid AND o.status='settled' AND o.environment='production'
		             AND o.completed_at >= $2 AND o.completed_at < $3
		       ),0)
		FROM departments d
		WHERE d.company_uuid=$1 AND d.deleted_at IS NULL
		ORDER BY d.name`, companyID, start, end)
	if err != nil {
		return nil, fmt.Errorf("department credit spending: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items := []models.CreditSpending{}
	for rows.Next() {
		item := models.CreditSpending{PeriodStart: start, PeriodEnd: end}
		var limit sql.NullInt64
		if err := rows.Scan(&item.SubjectUUID, &item.SubjectName, &limit, &item.UsedCredits); err != nil {
			return nil, fmt.Errorf("scan department credit spending: %w", err)
		}
		if limit.Valid {
			value := limit.Int64
			item.LimitCredits = &value
		}
		item.ForecastCredits = forecastCredits(item.UsedCredits, start, end, now)
		items = append(items, item)
	}

	return items, rows.Err()
}

// creditPeriod is the calendar month in UTC: limits and forecasts are read and
// reset on the same boundary as the subscription allowance.
func creditPeriod(now time.Time) (time.Time, time.Time) {
	utc := now.UTC()
	start := time.Date(utc.Year(), utc.Month(), 1, 0, 0, 0, 0, time.UTC)
	return start, start.AddDate(0, 1, 0)
}

// forecastCredits extends the pace of the period that has already passed over
// the whole period. Before anything is spent there is nothing to project.
func forecastCredits(used int64, start, end, now time.Time) int64 {
	if used <= 0 {
		return 0
	}
	elapsed := now.UTC().Sub(start)
	total := end.Sub(start)
	if elapsed <= 0 || total <= 0 {
		return used
	}
	if elapsed >= total {
		return used
	}

	return int64(float64(used) * (float64(total) / float64(elapsed)))
}
