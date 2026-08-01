package department

import (
	"context"
	"fmt"

	model "verbatrace/monolit/internal/models"
	"verbatrace/monolit/internal/repository/converter"
	repoModel "verbatrace/monolit/internal/repository/models"
	"verbatrace/monolit/internal/repository/scaner"
)

func (r *Repository) CreateDepartment(ctx context.Context, department model.Department) (model.Department, error) {
	repoDepartment, err := converter.ModelDepartmentToRepoDepartment(department)
	if err != nil {
		return model.Department{}, fmt.Errorf("convert department: %w", err)
	}

	query := `
	INSERT INTO departments (
		department_uuid,
		company_uuid,
		name,
		created_at
	)
	VALUES ($1, $2, $3, $4)
	RETURNING department_uuid,
	          company_uuid,
	          name,
	          created_at,
	          deleted_at
	`

	row := r.db.QueryRowContext(
		ctx,
		query,
		repoDepartment.ID,
		repoDepartment.CompanyUUID,
		repoDepartment.Name,
		repoDepartment.CreatedAt,
	)

	var createdDepartment repoModel.Department
	createdDepartment, err = scaner.ScanDepartment(row)
	if err != nil {
		return model.Department{}, fmt.Errorf("create department: %w", err)
	}

	return converter.RepoDepartmentToModel(createdDepartment)
}
