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

func (r *Repository) AddDepartmentMember(ctx context.Context, companyID uuid.UUID, member model.DepartmentMember) (model.DepartmentMember, error) {
	repoMember, err := converter.ModelDepartmentMemberToRepoDepartmentMember(member)
	if err != nil {
		return model.DepartmentMember{}, fmt.Errorf("convert department member: %w", err)
	}

	query := `
	INSERT INTO department_members (
		department_uuid,
		user_uuid,
		role,
		status,
		created_at
	)
	SELECT $1, $2, $3, $4, $5
	WHERE EXISTS (
		SELECT 1
		FROM departments d
		WHERE d.department_uuid = $1
		  AND d.company_uuid = $6
		  AND d.deleted_at IS NULL
	)
	ON CONFLICT (department_uuid, user_uuid)
	DO UPDATE SET role = EXCLUDED.role,
	              status = EXCLUDED.status
	RETURNING department_uuid,
	          user_uuid,
	          role,
	          status,
	          created_at
	`

	row := r.db.QueryRowContext(
		ctx,
		query,
		repoMember.DepartmentUUID,
		repoMember.UserUUID,
		repoMember.Role,
		repoMember.Status,
		repoMember.CreatedAt,
		companyID,
	)

	var createdMember repoModel.DepartmentMember
	createdMember, err = scaner.ScanDepartmentMember(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.DepartmentMember{}, model.ErrDepartmentNotFound
		}

		return model.DepartmentMember{}, fmt.Errorf("add department member: %w", err)
	}

	return converter.RepoDepartmentMemberToModel(createdMember)
}
