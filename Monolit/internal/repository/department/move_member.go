package department

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	model "verbatrace/monolit/internal/models"
	"verbatrace/monolit/internal/repository/converter"
	"verbatrace/monolit/internal/repository/scaner"

	"github.com/google/uuid"
)

// MoveMemberToDepartment puts an active company member into another department.
// The person's calls in this company move with them, otherwise the new leader
// would not see the work they are now responsible for and the old leader would
// keep access to it.
func (r *Repository) MoveMemberToDepartment(ctx context.Context, input model.MoveDepartmentMemberInput) (model.DepartmentMember, error) {
	now := input.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}

	role := input.Role
	if role == "" {
		role = model.DepartmentMemberRoleEmployee
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return model.DepartmentMember{}, fmt.Errorf("begin move department member: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var membershipExists bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM company_members
			WHERE company_uuid = $1 AND user_uuid = $2 AND status = 'active'
		)
	`, input.CompanyUUID, input.UserUUID).Scan(&membershipExists); err != nil {
		return model.DepartmentMember{}, fmt.Errorf("check company membership: %w", err)
	}
	if !membershipExists {
		return model.DepartmentMember{}, model.ErrCompanyNotFound
	}

	var targetExists bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM departments
			WHERE department_uuid = $1 AND company_uuid = $2 AND deleted_at IS NULL
		)
	`, input.ToDepartmentUUID, input.CompanyUUID).Scan(&targetExists); err != nil {
		return model.DepartmentMember{}, fmt.Errorf("check target department: %w", err)
	}
	if !targetExists {
		return model.DepartmentMember{}, model.ErrDepartmentNotFound
	}

	var previousDepartment uuid.NullUUID
	err = tx.QueryRowContext(ctx, `
		SELECT dm.department_uuid
		FROM department_members dm
		WHERE dm.company_uuid = $1 AND dm.user_uuid = $2 AND dm.status = 'active'
		FOR UPDATE
	`, input.CompanyUUID, input.UserUUID).Scan(&previousDepartment)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return model.DepartmentMember{}, fmt.Errorf("lock current department membership: %w", err)
	}

	if previousDepartment.Valid && previousDepartment.UUID == input.ToDepartmentUUID {
		if _, err := tx.ExecContext(ctx, `
			UPDATE department_members SET role = $3
			WHERE department_uuid = $1 AND user_uuid = $2 AND status = 'active'
		`, input.ToDepartmentUUID, input.UserUUID, string(role)); err != nil {
			return model.DepartmentMember{}, fmt.Errorf("update department role: %w", err)
		}
	} else {
		if previousDepartment.Valid {
			if _, err := tx.ExecContext(ctx, `
				UPDATE department_members SET status = 'left'
				WHERE department_uuid = $1 AND user_uuid = $2 AND status = 'active'
			`, previousDepartment.UUID, input.UserUUID); err != nil {
				return model.DepartmentMember{}, fmt.Errorf("leave previous department: %w", err)
			}
		}

		if _, err := tx.ExecContext(ctx, `
			INSERT INTO department_members (department_uuid, user_uuid, role, status, created_at)
			VALUES ($1, $2, $3, 'active', $4)
			ON CONFLICT (department_uuid, user_uuid)
			DO UPDATE SET role = EXCLUDED.role, status = EXCLUDED.status
		`, input.ToDepartmentUUID, input.UserUUID, string(role), now); err != nil {
			return model.DepartmentMember{}, fmt.Errorf("join department: %w", err)
		}
	}

	if previousDepartment.Valid && previousDepartment.UUID != input.ToDepartmentUUID {
		if err := moveMemberCalls(ctx, tx, input.CompanyUUID, input.UserUUID, previousDepartment.UUID, input.ToDepartmentUUID); err != nil {
			return model.DepartmentMember{}, err
		}
	}

	member, err := getDepartmentMemberTx(ctx, tx, input.CompanyUUID, input.ToDepartmentUUID, input.UserUUID)
	if err != nil {
		return model.DepartmentMember{}, err
	}

	if err := tx.Commit(); err != nil {
		return model.DepartmentMember{}, fmt.Errorf("commit move department member: %w", err)
	}

	return member, nil
}

// moveMemberCalls re-points the person's calls at the new department and drops
// folder links that no longer match the call's placement.
func moveMemberCalls(ctx context.Context, tx *sql.Tx, companyID, userID, fromDepartment, toDepartment uuid.UUID) error {
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM call_folder_assignments a
		USING calls c, call_folders f
		WHERE a.call_uuid = c.call_uuid
		  AND f.folder_uuid = a.folder_uuid
		  AND c.company_uuid = $1
		  AND c.uploaded_by_user_uuid = $2
		  AND c.department_uuid = $3
		  AND f.department_uuid = $3
	`, companyID, userID, fromDepartment); err != nil {
		return fmt.Errorf("drop folder links of moved calls: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE calls
		SET department_uuid = $4
		WHERE company_uuid = $1
		  AND uploaded_by_user_uuid = $2
		  AND department_uuid = $3
	`, companyID, userID, fromDepartment, toDepartment); err != nil {
		return fmt.Errorf("move calls to new department: %w", err)
	}

	return nil
}

func getDepartmentMemberTx(ctx context.Context, tx *sql.Tx, companyID, departmentID, userID uuid.UUID) (model.DepartmentMember, error) {
	const query = `
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
	`

	row := tx.QueryRowContext(ctx, query, companyID, departmentID, userID)
	repoMember, err := scaner.ScanDepartmentMember(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.DepartmentMember{}, model.ErrDepartmentNotFound
		}
		return model.DepartmentMember{}, fmt.Errorf("get moved department member: %w", err)
	}

	return converter.RepoDepartmentMemberToModel(repoMember)
}
