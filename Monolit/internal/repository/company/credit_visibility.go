package company

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

func (r *Repository) GetCompanyCreditUsageVisibility(ctx context.Context, companyID uuid.UUID) (bool, error) {
	var visible bool
	if err := r.db.QueryRowContext(ctx, `
		SELECT credit_usage_visible_to_members
		FROM companies
		WHERE company_uuid = $1 AND deleted_at IS NULL
	`, companyID).Scan(&visible); err != nil {
		return false, fmt.Errorf("get company credit usage visibility: %w", err)
	}
	return visible, nil
}

func (r *Repository) UpdateCompanyCreditUsageVisibility(ctx context.Context, companyID uuid.UUID, visible bool) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE companies
		SET credit_usage_visible_to_members = $2
		WHERE company_uuid = $1 AND deleted_at IS NULL
	`, companyID, visible)
	if err != nil {
		return fmt.Errorf("update company credit usage visibility: %w", err)
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read company credit usage visibility update: %w", err)
	}
	if updated == 0 {
		return fmt.Errorf("update company credit usage visibility: company not found")
	}
	return nil
}
