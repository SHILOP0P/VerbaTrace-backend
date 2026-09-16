package invitation

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

func (r *Repository) DeclineInvitation(ctx context.Context, id uuid.UUID, now time.Time) (model.MembershipInvitation, error) {
	return r.updatePendingStatus(ctx, id, model.InvitationStatusDeclined, now)
}

func (r *Repository) CancelInvitation(ctx context.Context, id uuid.UUID, now time.Time) (model.MembershipInvitation, error) {
	return r.updatePendingStatus(ctx, id, model.InvitationStatusCanceled, now)
}

// DecideInvitationApproval releases or rejects an invitation a department leader
// created for a person the company had excluded.
func (r *Repository) DecideInvitationApproval(ctx context.Context, id uuid.UUID, approvedBy uuid.UUID, approve bool, now time.Time) (model.MembershipInvitation, error) {
	approvalStatus := model.InvitationApprovalRejected
	invitationStatus := string(model.InvitationStatusCanceled)
	if approve {
		approvalStatus = model.InvitationApprovalApproved
		invitationStatus = string(model.InvitationStatusPending)
	}

	query := `
	UPDATE membership_invitations
	SET approval_status = $2,
	    approval_decided_by_user_uuid = $3,
	    approval_decided_at = $4,
	    status = $5,
	    updated_at = $4
	WHERE invitation_uuid = $1
	  AND status = 'pending'
	  AND approval_status = 'pending'
	RETURNING ` + invitationColumns

	row := r.db.QueryRowContext(ctx, query, id, string(approvalStatus), approvedBy, now, invitationStatus)
	repoInvitation, err := scaner.ScanInvitation(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.MembershipInvitation{}, model.ErrInvitationNotPending
		}
		return model.MembershipInvitation{}, fmt.Errorf("decide invitation approval: %w", err)
	}

	return converter.RepoInvitationToModel(repoInvitation)
}

// ExpireInvitations marks the invitations nobody answered in time. Without it a
// stale invitation keeps looking actionable in every list.
func (r *Repository) ExpireInvitations(ctx context.Context, now time.Time) (int64, error) {
	result, err := r.db.ExecContext(ctx, `
		UPDATE membership_invitations
		SET status = 'expired', updated_at = $1
		WHERE status = 'pending' AND expires_at <= $1
	`, now)
	if err != nil {
		return 0, fmt.Errorf("expire invitations: %w", err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("count expired invitations: %w", err)
	}

	return affected, nil
}

// CancelCompanyInvitations drops every pending invitation of a company, which is
// what archiving or freezing a company must do.
func (r *Repository) CancelCompanyInvitations(ctx context.Context, companyID uuid.UUID, now time.Time) error {
	if _, err := r.db.ExecContext(ctx, `
		UPDATE membership_invitations
		SET status = 'canceled', updated_at = $2
		WHERE company_uuid = $1 AND status = 'pending'
	`, companyID, now); err != nil {
		return fmt.Errorf("cancel company invitations: %w", err)
	}

	return nil
}

// CancelDepartmentInvitations drops pending invitations into a department that
// no longer exists.
func (r *Repository) CancelDepartmentInvitations(ctx context.Context, departmentID uuid.UUID, now time.Time) error {
	if _, err := r.db.ExecContext(ctx, `
		UPDATE membership_invitations
		SET status = 'canceled', updated_at = $2
		WHERE department_uuid = $1 AND status = 'pending'
	`, departmentID, now); err != nil {
		return fmt.Errorf("cancel department invitations: %w", err)
	}

	return nil
}

func (r *Repository) updatePendingStatus(ctx context.Context, id uuid.UUID, status model.InvitationStatus, now time.Time) (model.MembershipInvitation, error) {
	query := `
	UPDATE membership_invitations
	SET status = $2,
	    responded_at = $3,
	    updated_at = $3
	WHERE invitation_uuid = $1
	  AND status = 'pending'
	RETURNING ` + invitationColumns

	row := r.db.QueryRowContext(ctx, query, id, string(status), now)
	repoInvitation, err := scaner.ScanInvitation(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			_, getErr := r.GetInvitationByUUID(ctx, id)
			if errors.Is(getErr, model.ErrInvitationNotFound) {
				return model.MembershipInvitation{}, model.ErrInvitationNotFound
			}
			if getErr != nil {
				return model.MembershipInvitation{}, getErr
			}
			return model.MembershipInvitation{}, model.ErrInvitationNotPending
		}

		return model.MembershipInvitation{}, fmt.Errorf("update invitation status: %w", err)
	}

	return converter.RepoInvitationToModel(repoInvitation)
}
