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

func (r *Repository) GetDepartmentMember(ctx context.Context, companyID uuid.UUID, departmentID uuid.UUID, userID uuid.UUID) (model.DepartmentMember, error) {
	query := `
	SELECT dm.department_uuid,
	       dm.user_uuid,
	       dm.role,
	       dm.status,
	       dm.created_at
	FROM department_members dm
	JOIN departments d ON d.department_uuid = dm.department_uuid
	WHERE d.company_uuid = $1
	  AND dm.department_uuid = $2
	  AND dm.user_uuid = $3
	  AND dm.status = 'active'
	  AND d.deleted_at IS NULL
	`

	row := r.db.QueryRowContext(ctx, query, companyID, departmentID, userID)

	var repoMember repoModel.DepartmentMember
	repoMember, err := scaner.ScanDepartmentMember(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.DepartmentMember{}, model.ErrDepartmentNotFound
		}

		return model.DepartmentMember{}, fmt.Errorf("get department member: %w", err)
	}

	return converter.RepoDepartmentMemberToModel(repoMember)
}
