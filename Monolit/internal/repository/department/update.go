package department

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	model "verbatrace/monolit/internal/models"
	"verbatrace/monolit/internal/repository/converter"
	repoModel "verbatrace/monolit/internal/repository/models"
	"verbatrace/monolit/internal/repository/scaner"

	"github.com/google/uuid"
)

func (r *Repository) UpdateDepartment(ctx context.Context, companyID uuid.UUID, departmentID uuid.UUID, name string) (model.Department, error) {
	query := `
	UPDATE departments
	SET name = $3
	WHERE company_uuid = $1
	  AND department_uuid = $2
	  AND deleted_at IS NULL
	RETURNING department_uuid,
	          company_uuid,
	          name,
	          created_at,
	          deleted_at
	`

	row := r.db.QueryRowContext(ctx, query, companyID, departmentID, name)

	var repoDepartment repoModel.Department
	repoDepartment, err := scaner.ScanDepartment(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.Department{}, model.ErrDepartmentNotFound
		}

		return model.Department{}, fmt.Errorf("update department: %w", err)
	}

	return converter.RepoDepartmentToModel(repoDepartment)
}

func (r *Repository) ArchiveDepartment(ctx context.Context, companyID uuid.UUID, departmentID uuid.UUID) error {
	query := `
	UPDATE departments
	SET deleted_at = now()
	WHERE company_uuid = $1
	  AND department_uuid = $2
	  AND deleted_at IS NULL
	`

	result, err := r.db.ExecContext(ctx, query, companyID, departmentID)
	if err != nil {
		return fmt.Errorf("archive department: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("archive department rows affected: %w", err)
	}
	if rows == 0 {
		return model.ErrDepartmentNotFound
	}

	return nil
}
