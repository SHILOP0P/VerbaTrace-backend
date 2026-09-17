package admin

import (
	"context"
	"database/sql"
	"fmt"

	"verbatrace/monolit/internal/models"
)

// ListRestorableCompanies is the superadmin's rescue queue: companies that are
// soft-deleted, still inside their purge window and have not used their one
// restore yet.
//
// Every other company query filters `deleted_at IS NULL`, which is why this one
// exists. A soft-deleted company was invisible in the panel, so the company card
// that carries the restore control could not be opened for the only companies
// that need it.
func (r *Repository) ListRestorableCompanies(ctx context.Context) ([]models.AdminRestorableCompany, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT company_uuid, name, tag, manager_user_uuid, soft_deleted_at, purge_after
		FROM companies
		WHERE lifecycle_state = 'soft_deleted'
		  AND restore_used = false
		  AND soft_deleted_at IS NOT NULL
		ORDER BY purge_after NULLS LAST, soft_deleted_at
		LIMIT 100
	`)
	if err != nil {
		return nil, fmt.Errorf("list restorable companies: %w", err)
	}
	defer func() { _ = rows.Close() }()

	companies := []models.AdminRestorableCompany{}
	for rows.Next() {
		var company models.AdminRestorableCompany
		var purgeAfter sql.NullTime
		if err = rows.Scan(&company.ID, &company.Name, &company.Tag, &company.ManagerUserUUID, &company.SoftDeletedAt, &purgeAfter); err != nil {
			return nil, fmt.Errorf("scan restorable company: %w", err)
		}
		if purgeAfter.Valid {
			deadline := purgeAfter.Time
			company.PurgeAfter = &deadline
		}
		companies = append(companies, company)
	}

	return companies, rows.Err()
}
