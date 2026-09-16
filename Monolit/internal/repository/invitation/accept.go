package invitation

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	model "verbatrace/monolit/internal/models"
	companyRepo "verbatrace/monolit/internal/repository/company"
	"verbatrace/monolit/internal/repository/converter"
	repoModel "verbatrace/monolit/internal/repository/models"
	"verbatrace/monolit/internal/repository/scaner"

	"github.com/google/uuid"
)

// AcceptInvitation turns a pending invitation into a membership. A user works in
// one company at a time, so joining a new one means leaving the previous one in
// the same transaction, and only after the user confirmed the move.
func (r *Repository) AcceptInvitation(ctx context.Context, command model.AcceptInvitationCommand) (model.MembershipInvitation, error) {
	now := command.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return model.MembershipInvitation{}, fmt.Errorf("begin accept invitation transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	invitation, err := getInvitationForUpdate(ctx, tx, command.InvitationUUID)
	if err != nil {
		return model.MembershipInvitation{}, err
	}

	if invitation.Status != string(model.InvitationStatusPending) {
		return model.MembershipInvitation{}, model.ErrInvitationNotPending
	}

	if invitation.ApprovalStatus == string(model.InvitationApprovalPending) {
		return model.MembershipInvitation{}, model.ErrInvitationApprovalRequired
	}
	if invitation.ApprovalStatus == string(model.InvitationApprovalRejected) {
		return model.MembershipInvitation{}, model.ErrInvitationNotPending
	}

	if !invitation.ExpiresAt.After(now) {
		if err := markExpired(ctx, tx, command.InvitationUUID, now); err != nil {
			return model.MembershipInvitation{}, err
		}
		if err := tx.Commit(); err != nil {
			return model.MembershipInvitation{}, fmt.Errorf("commit expired invitation: %w", err)
		}
		return model.MembershipInvitation{}, model.ErrInvitationExpired
	}

	// The company or department could have been archived while the invitation
	// was waiting, and joining a dead scope leaves the user with nothing.
	if err := ensureScopeAlive(ctx, tx, invitation); err != nil {
		return model.MembershipInvitation{}, err
	}

	previousCompany, err := activeEmployerCompanyForUpdate(ctx, tx, invitation.InvitedUserUUID)
	if err != nil {
		return model.MembershipInvitation{}, err
	}
	if previousCompany != uuid.Nil && previousCompany != invitation.CompanyUUID {
		if !command.ConfirmTransfer {
			return model.MembershipInvitation{}, model.ErrCompanyMembershipConflict
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE company_members
			SET status = 'left'
			WHERE company_uuid = $1 AND user_uuid = $2 AND status = 'active'
		`, previousCompany, invitation.InvitedUserUUID); err != nil {
			return model.MembershipInvitation{}, fmt.Errorf("leave previous company: %w", err)
		}
		if err := companyRepo.CleanupCompanyAccessTx(ctx, tx, previousCompany, invitation.InvitedUserUUID, now); err != nil {
			return model.MembershipInvitation{}, err
		}
	}

	if err := upsertCompanyMember(ctx, tx, invitation); err != nil {
		return model.MembershipInvitation{}, companyRepo.MembershipConflictError(err)
	}

	if invitation.DepartmentUUID.Valid {
		if err := upsertDepartmentMember(ctx, tx, invitation); err != nil {
			return model.MembershipInvitation{}, err
		}
	}

	accepted, err := setAccepted(ctx, tx, command.InvitationUUID, now)
	if err != nil {
		return model.MembershipInvitation{}, err
	}

	if err := tx.Commit(); err != nil {
		return model.MembershipInvitation{}, fmt.Errorf("commit accept invitation: %w", err)
	}

	return converter.RepoInvitationToModel(accepted)
}

func getInvitationForUpdate(ctx context.Context, tx *sql.Tx, id uuid.UUID) (repoModel.MembershipInvitation, error) {
	query := `
	SELECT ` + invitationColumns + `
	FROM membership_invitations
	WHERE invitation_uuid = $1
	FOR UPDATE
	`

	row := tx.QueryRowContext(ctx, query, id)
	invitation, err := scaner.ScanInvitation(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return repoModel.MembershipInvitation{}, model.ErrInvitationNotFound
		}
		return repoModel.MembershipInvitation{}, fmt.Errorf("get invitation for update: %w", err)
	}

	return invitation, nil
}

func ensureScopeAlive(ctx context.Context, tx *sql.Tx, invitation repoModel.MembershipInvitation) error {
	var alive bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM companies WHERE company_uuid = $1 AND deleted_at IS NULL)
	`, invitation.CompanyUUID).Scan(&alive); err != nil {
		return fmt.Errorf("check company for invitation: %w", err)
	}
	if !alive {
		return model.ErrCompanyNotFound
	}

	if !invitation.DepartmentUUID.Valid {
		return nil
	}

	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM departments
			WHERE department_uuid = $1 AND company_uuid = $2 AND deleted_at IS NULL
		)
	`, invitation.DepartmentUUID.UUID, invitation.CompanyUUID).Scan(&alive); err != nil {
		return fmt.Errorf("check department for invitation: %w", err)
	}
	if !alive {
		return model.ErrDepartmentNotFound
	}

	return nil
}

func activeEmployerCompanyForUpdate(ctx context.Context, tx *sql.Tx, userID uuid.UUID) (uuid.UUID, error) {
	var companyID uuid.UUID
	err := tx.QueryRowContext(ctx, `
		SELECT company_uuid
		FROM company_members
		WHERE user_uuid = $1 AND status = 'active' AND role = 'employee'
		FOR UPDATE
	`, userID).Scan(&companyID)
	if errors.Is(err, sql.ErrNoRows) {
		return uuid.Nil, nil
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("lock current company membership: %w", err)
	}

	return companyID, nil
}

func markExpired(ctx context.Context, tx *sql.Tx, id uuid.UUID, now time.Time) error {
	query := `
	UPDATE membership_invitations
	SET status = 'expired',
	    updated_at = $2
	WHERE invitation_uuid = $1
	`

	if _, err := tx.ExecContext(ctx, query, id, now); err != nil {
		return fmt.Errorf("mark invitation expired: %w", err)
	}

	return nil
}

func upsertCompanyMember(ctx context.Context, tx *sql.Tx, invitation repoModel.MembershipInvitation) error {
	query := `
	INSERT INTO company_members (
		company_uuid,
		user_uuid,
		role,
		status,
		created_at
	)
	VALUES ($1, $2, $3, 'active', now())
	ON CONFLICT (company_uuid, user_uuid)
	DO UPDATE SET role = CASE
	                       WHEN company_members.role = 'company_manager' THEN company_members.role
	                       ELSE EXCLUDED.role
	                     END,
	              status = EXCLUDED.status
	`

	if _, err := tx.ExecContext(ctx, query, invitation.CompanyUUID, invitation.InvitedUserUUID, invitation.CompanyRole); err != nil {
		return fmt.Errorf("upsert company member: %w", err)
	}

	return nil
}

func upsertDepartmentMember(ctx context.Context, tx *sql.Tx, invitation repoModel.MembershipInvitation) error {
	if _, err := tx.ExecContext(ctx, `
		UPDATE department_members dm
		SET status = 'left'
		FROM departments d
		WHERE d.department_uuid = dm.department_uuid
		  AND d.company_uuid = $1
		  AND dm.user_uuid = $2
		  AND dm.department_uuid <> $3
		  AND dm.status = 'active'
	`, invitation.CompanyUUID, invitation.InvitedUserUUID, invitation.DepartmentUUID.UUID); err != nil {
		return fmt.Errorf("leave previous department memberships: %w", err)
	}

	query := `
	INSERT INTO department_members (
		department_uuid,
		user_uuid,
		role,
		status,
		created_at
	)
	VALUES ($1, $2, $3, 'active', now())
	ON CONFLICT (department_uuid, user_uuid)
	DO UPDATE SET role = EXCLUDED.role,
	              status = EXCLUDED.status
	`

	if _, err := tx.ExecContext(ctx, query, invitation.DepartmentUUID.UUID, invitation.InvitedUserUUID, invitation.DepartmentRole.String); err != nil {
		return fmt.Errorf("upsert department member: %w", err)
	}

	return nil
}

func setAccepted(ctx context.Context, tx *sql.Tx, id uuid.UUID, now time.Time) (repoModel.MembershipInvitation, error) {
	query := `
	UPDATE membership_invitations
	SET status = 'accepted',
	    responded_at = $2,
	    updated_at = $2
	WHERE invitation_uuid = $1
	RETURNING ` + invitationColumns

	row := tx.QueryRowContext(ctx, query, id, now)
	invitation, err := scaner.ScanInvitation(row)
	if err != nil {
		return repoModel.MembershipInvitation{}, fmt.Errorf("set invitation accepted: %w", err)
	}

	return invitation, nil
}
