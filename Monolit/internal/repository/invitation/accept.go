package invitation

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	model "verbatrace/monolit/internal/models"
	"verbatrace/monolit/internal/repository/converter"
	repoModel "verbatrace/monolit/internal/repository/models"
	"verbatrace/monolit/internal/repository/scaner"

	"github.com/google/uuid"
)

func (r *Repository) AcceptInvitation(ctx context.Context, id uuid.UUID, now time.Time) (model.MembershipInvitation, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return model.MembershipInvitation{}, fmt.Errorf("begin accept invitation transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	invitation, err := getInvitationForUpdate(ctx, tx, id)
	if err != nil {
		return model.MembershipInvitation{}, err
	}

	if invitation.Status != string(model.InvitationStatusPending) {
		return model.MembershipInvitation{}, model.ErrInvitationNotPending
	}

	if !invitation.ExpiresAt.After(now) {
		if err := markExpired(ctx, tx, id, now); err != nil {
			return model.MembershipInvitation{}, err
		}
		if err := tx.Commit(); err != nil {
			return model.MembershipInvitation{}, fmt.Errorf("commit expired invitation: %w", err)
		}
		return model.MembershipInvitation{}, model.ErrInvitationExpired
	}

	if invitation.DepartmentUUID.Valid {
		if err := ensureActiveCompanyMember(ctx, tx, invitation.CompanyUUID, invitation.InvitedUserUUID); err != nil {
			return model.MembershipInvitation{}, err
		}

		if err := upsertDepartmentMember(ctx, tx, invitation); err != nil {
			return model.MembershipInvitation{}, err
		}
	} else {
		if err := upsertCompanyMember(ctx, tx, invitation); err != nil {
			return model.MembershipInvitation{}, err
		}
	}

	accepted, err := setAccepted(ctx, tx, id, now)
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
	DO UPDATE SET role = EXCLUDED.role,
	              status = EXCLUDED.status
	`

	if _, err := tx.ExecContext(ctx, query, invitation.CompanyUUID, invitation.InvitedUserUUID, invitation.CompanyRole); err != nil {
		return fmt.Errorf("upsert company member: %w", err)
	}

	return nil
}

func ensureActiveCompanyMember(ctx context.Context, tx *sql.Tx, companyID uuid.UUID, userID uuid.UUID) error {
	query := `
	SELECT 1
	FROM company_members
	WHERE company_uuid = $1
	  AND user_uuid = $2
	  AND status = 'active'
	`

	var exists int
	err := tx.QueryRowContext(ctx, query, companyID, userID).Scan(&exists)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.ErrForbidden
		}
		return fmt.Errorf("check active company member: %w", err)
	}

	return nil
}

func upsertDepartmentMember(ctx context.Context, tx *sql.Tx, invitation repoModel.MembershipInvitation) error {
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
