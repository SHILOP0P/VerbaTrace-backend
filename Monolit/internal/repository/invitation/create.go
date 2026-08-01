package invitation

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	model "verbatrace/monolit/internal/models"
	"verbatrace/monolit/internal/repository/converter"
	repoModel "verbatrace/monolit/internal/repository/models"
	"verbatrace/monolit/internal/repository/scaner"
)

func (r *Repository) CreateInvitation(ctx context.Context, invitation model.MembershipInvitation) (model.MembershipInvitation, error) {
	repoInvitation, err := converter.ModelInvitationToRepoInvitation(invitation)
	if err != nil {
		return model.MembershipInvitation{}, fmt.Errorf("convert invitation: %w", err)
	}

	query := `
	INSERT INTO membership_invitations (
		invitation_uuid,
		company_uuid,
		department_uuid,
		invited_user_uuid,
		invited_by_user_uuid,
		company_role,
		department_role,
		status,
		expires_at,
		responded_at,
		created_at,
		updated_at
	)
	SELECT $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12
	WHERE $3::uuid IS NULL
	   OR EXISTS (
		   SELECT 1
		   FROM departments d
		   WHERE d.department_uuid = $3
		     AND d.company_uuid = $2
		     AND d.deleted_at IS NULL
	   )
	RETURNING ` + invitationColumns

	row := r.db.QueryRowContext(
		ctx,
		query,
		repoInvitation.ID,
		repoInvitation.CompanyUUID,
		repoInvitation.DepartmentUUID,
		repoInvitation.InvitedUserUUID,
		repoInvitation.InvitedByUserUUID,
		repoInvitation.CompanyRole,
		repoInvitation.DepartmentRole,
		repoInvitation.Status,
		repoInvitation.ExpiresAt,
		repoInvitation.RespondedAt,
		repoInvitation.CreatedAt,
		repoInvitation.UpdatedAt,
	)

	var created repoModel.MembershipInvitation
	created, err = scaner.ScanInvitation(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.MembershipInvitation{}, model.ErrDepartmentNotFound
		}

		err = normalizeCreateError(err)
		if errors.Is(err, model.ErrInvitationAlreadyExists) {
			return model.MembershipInvitation{}, err
		}

		return model.MembershipInvitation{}, fmt.Errorf("create invitation: %w", err)
	}

	return converter.RepoInvitationToModel(created)
}
