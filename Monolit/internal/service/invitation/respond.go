package invitation

import (
	"context"
	"errors"
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

// companyInvitationsWindow is how far back the owner and the deputy can look at
// what their company sent out.
const companyInvitationsWindow = 90 * 24 * time.Hour

func (s *Service) AcceptInvitation(ctx context.Context, input models.AcceptInvitationInput) (models.MembershipInvitation, error) {
	if input.InvitationUUID == uuid.Nil || input.RequestUser == uuid.Nil {
		return models.MembershipInvitation{}, models.ErrInvalidInvitationInput
	}

	invitation, err := s.invitationRepository.GetInvitationByUUID(ctx, input.InvitationUUID)
	if err != nil {
		return models.MembershipInvitation{}, err
	}
	if invitation.InvitedUserUUID != input.RequestUser {
		return models.MembershipInvitation{}, models.ErrForbidden
	}
	if invitation.Status != models.InvitationStatusPending {
		return models.MembershipInvitation{}, models.ErrInvitationNotPending
	}
	if !invitation.Visible() {
		return models.MembershipInvitation{}, models.ErrInvitationApprovalRequired
	}
	if !invitation.ExpiresAt.After(s.now()) {
		_, err = s.invitationRepository.AcceptInvitation(ctx, models.AcceptInvitationCommand{InvitationUUID: input.InvitationUUID, Now: s.now()})
		if errors.Is(err, models.ErrInvitationExpired) {
			return models.MembershipInvitation{}, err
		}
		return models.MembershipInvitation{}, models.ErrInvitationExpired
	}

	if !invitation.DepartmentUUID.Valid {
		active, err := s.isActiveCompanyMember(ctx, invitation.CompanyUUID, invitation.InvitedUserUUID)
		if err != nil {
			return models.MembershipInvitation{}, err
		}
		if !active && s.billingLimiter != nil {
			if err := s.billingLimiter.CanAddCompanyMember(ctx, invitation.CompanyUUID); err != nil {
				return models.MembershipInvitation{}, err
			}
		}
	}

	accepted, err := s.invitationRepository.AcceptInvitation(ctx, models.AcceptInvitationCommand{
		InvitationUUID:  input.InvitationUUID,
		ConfirmTransfer: input.ConfirmTransfer,
		Now:             s.now(),
	})
	if err != nil {
		// The user must see which company they are about to leave before the
		// move happens, so the conflict carries that company with it.
		if errors.Is(err, models.ErrCompanyMembershipConflict) {
			return models.MembershipInvitation{}, s.describeMembershipConflict(ctx, invitation.InvitedUserUUID)
		}
		return models.MembershipInvitation{}, err
	}

	return accepted, nil
}

func (s *Service) describeMembershipConflict(ctx context.Context, userID uuid.UUID) error {
	company, err := s.companyRepository.ActiveEmployerCompany(ctx, userID)
	if err != nil {
		return models.ErrCompanyMembershipConflict
	}

	return &models.CompanyMembershipConflict{
		CurrentCompanyUUID: company.ID,
		CurrentCompanyName: company.Name,
	}
}

func (s *Service) DeclineInvitation(ctx context.Context, input models.DeclineInvitationInput) (models.MembershipInvitation, error) {
	if input.InvitationUUID == uuid.Nil || input.RequestUser == uuid.Nil {
		return models.MembershipInvitation{}, models.ErrInvalidInvitationInput
	}

	invitation, err := s.invitationRepository.GetInvitationByUUID(ctx, input.InvitationUUID)
	if err != nil {
		return models.MembershipInvitation{}, err
	}
	if invitation.InvitedUserUUID != input.RequestUser {
		return models.MembershipInvitation{}, models.ErrForbidden
	}
	if invitation.Status != models.InvitationStatusPending {
		return models.MembershipInvitation{}, models.ErrInvitationNotPending
	}
	if !invitation.Visible() {
		return models.MembershipInvitation{}, models.ErrInvitationApprovalRequired
	}

	return s.invitationRepository.DeclineInvitation(ctx, input.InvitationUUID, s.now())
}

func (s *Service) CancelInvitation(ctx context.Context, input models.CancelInvitationInput) (models.MembershipInvitation, error) {
	if input.CompanyUUID == uuid.Nil || input.InvitationUUID == uuid.Nil || input.RequestUser == uuid.Nil {
		return models.MembershipInvitation{}, models.ErrInvalidInvitationInput
	}

	invitation, err := s.invitationRepository.GetInvitationByUUID(ctx, input.InvitationUUID)
	if err != nil {
		return models.MembershipInvitation{}, err
	}
	if invitation.CompanyUUID != input.CompanyUUID {
		return models.MembershipInvitation{}, models.ErrInvitationNotFound
	}
	if input.DepartmentUUID.Valid && (!invitation.DepartmentUUID.Valid || invitation.DepartmentUUID.UUID != input.DepartmentUUID.UUID) {
		return models.MembershipInvitation{}, models.ErrInvitationNotFound
	}
	if invitation.Status != models.InvitationStatusPending {
		return models.MembershipInvitation{}, models.ErrInvitationNotPending
	}

	if invitation.DepartmentUUID.Valid {
		if err := s.requireDepartmentCancelPermission(ctx, invitation, input.RequestUser); err != nil {
			return models.MembershipInvitation{}, err
		}
	} else if err := s.requireCompanyManager(ctx, input.CompanyUUID, input.RequestUser); err != nil {
		return models.MembershipInvitation{}, err
	}

	return s.invitationRepository.CancelInvitation(ctx, input.InvitationUUID, s.now())
}

// DecideInvitationApproval releases a leader's invitation for a person the
// company excluded, or refuses it.
func (s *Service) DecideInvitationApproval(ctx context.Context, input models.DecideInvitationApprovalInput) (models.MembershipInvitation, error) {
	if input.CompanyUUID == uuid.Nil || input.InvitationUUID == uuid.Nil || input.RequestUser == uuid.Nil {
		return models.MembershipInvitation{}, models.ErrInvalidInvitationInput
	}

	invitation, err := s.invitationRepository.GetInvitationByUUID(ctx, input.InvitationUUID)
	if err != nil {
		return models.MembershipInvitation{}, err
	}
	if invitation.CompanyUUID != input.CompanyUUID {
		return models.MembershipInvitation{}, models.ErrInvitationNotFound
	}
	if invitation.ApprovalStatus != models.InvitationApprovalPending {
		return models.MembershipInvitation{}, models.ErrInvitationNotPending
	}
	if err := s.requireCompanyManager(ctx, input.CompanyUUID, input.RequestUser); err != nil {
		return models.MembershipInvitation{}, err
	}

	decided, err := s.invitationRepository.DecideInvitationApproval(ctx, input.InvitationUUID, input.RequestUser, input.Approve, s.now())
	if err != nil {
		return models.MembershipInvitation{}, err
	}

	s.notify(ctx, decided.InvitedByUserUUID, models.NotificationTypeInvitationApprovalDecided, "Решение по приглашению", approvalDecisionBody(input.Approve), decided.ID)
	if input.Approve {
		s.notifyInvitationCreated(ctx, decided)
	}

	return decided, nil
}

func approvalDecisionBody(approved bool) string {
	if approved {
		return "Приглашение одобрено и отправлено сотруднику"
	}
	return "Приглашение отклонено"
}

// ListCompanyInvitations shows the owner and the deputy what the company sent
// recently, so repeated invitations stay visible.
func (s *Service) ListCompanyInvitations(ctx context.Context, input models.ListCompanyInvitationsInput) ([]models.MembershipInvitation, error) {
	if input.CompanyUUID == uuid.Nil || input.RequestUser == uuid.Nil {
		return nil, models.ErrInvalidInvitationInput
	}
	if !validInvitationStatus(input.Status) {
		return nil, models.ErrInvalidInvitationInput
	}
	if err := s.requireCompanyManager(ctx, input.CompanyUUID, input.RequestUser); err != nil {
		return nil, err
	}

	if input.Since == nil {
		since := s.now().Add(-companyInvitationsWindow)
		input.Since = &since
	}

	return s.invitationRepository.ListCompanyInvitations(ctx, input)
}

// ExpirePendingInvitations is the worker entry point: an invitation nobody
// answered must stop looking actionable.
func (s *Service) ExpirePendingInvitations(ctx context.Context) (int64, error) {
	return s.invitationRepository.ExpireInvitations(ctx, s.now())
}

func (s *Service) requireDepartmentCancelPermission(ctx context.Context, invitation models.MembershipInvitation, requestUser uuid.UUID) error {
	manager, err := s.companyRepository.GetCompanyMember(ctx, invitation.CompanyUUID, requestUser)
	if err == nil && manager.Role.ManagesCompany() {
		return nil
	}
	if err != nil && !errors.Is(err, models.ErrCompanyNotFound) {
		return err
	}

	member, err := s.departmentRepository.GetDepartmentMember(ctx, invitation.CompanyUUID, invitation.DepartmentUUID.UUID, requestUser)
	if err != nil {
		return err
	}
	if member.Role != models.DepartmentMemberRoleLeader || invitation.InvitedByUserUUID != requestUser {
		return models.ErrForbidden
	}

	return nil
}
