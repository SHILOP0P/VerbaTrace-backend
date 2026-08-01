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

func (r *Repository) UpdateDepartmentMemberRole(ctx context.Context, companyID uuid.UUID, departmentID uuid.UUID, userID uuid.UUID, role model.DepartmentMemberRole) (model.DepartmentMember, error) {
	query := `
	UPDATE department_members dm
	SET role = $4
	FROM departments d
	WHERE d.department_uuid = dm.department_uuid
	  AND d.company_uuid = $1
	  AND d.deleted_at IS NULL
	  AND dm.department_uuid = $2
	  AND dm.user_uuid = $3
	  AND dm.status = 'active'
	RETURNING dm.department_uuid,
	          dm.user_uuid,
	          dm.role,
	          dm.status,
	          dm.created_at
	`

	row := r.db.QueryRowContext(ctx, query, companyID, departmentID, userID, string(role))

	var repoMember repoModel.DepartmentMember
	repoMember, err := scaner.ScanDepartmentMember(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.DepartmentMember{}, model.ErrDepartmentNotFound
		}

		return model.DepartmentMember{}, fmt.Errorf("update department member role: %w", err)
	}

	return converter.RepoDepartmentMemberToModel(repoMember)
}
