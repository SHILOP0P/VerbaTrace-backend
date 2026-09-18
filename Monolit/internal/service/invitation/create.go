package invitation

import (
	"context"
	"errors"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

func (s *Service) CreateCompanyInvitation(ctx context.Context, input models.CreateCompanyInvitationInput) (models.MembershipInvitation, error) {
	role := input.Role
	if role == "" {
		role = models.CompanyMemberRoleEmployee
	}
	if input.CompanyUUID == uuid.Nil || input.RequestUser == uuid.Nil {
		return models.MembershipInvitation{}, models.ErrInvalidInvitationInput
	}
	if role != models.CompanyMemberRoleEmployee && role != models.CompanyMemberRoleDeputy {
		return models.MembershipInvitation{}, models.ErrInvalidInvitationInput
	}

	targetUserID, err := s.resolveTargetUser(ctx, input.RequestUser, input.UserUUID, input.Username)
	if err != nil {
		return models.MembershipInvitation{}, err
	}

	// Offering the deputy seat is the owner's decision alone, exactly like
	// promoting a member to it. A deputy must not be able to seat another deputy.
	if role == models.CompanyMemberRoleDeputy {
		if err := s.requireCompanyOwner(ctx, input.CompanyUUID, input.RequestUser); err != nil {
			return models.MembershipInvitation{}, err
		}
	} else if err := s.requireCompanyManager(ctx, input.CompanyUUID, input.RequestUser); err != nil {
		return models.MembershipInvitation{}, err
	}

	if err := s.requireActiveCompanySubscription(ctx, input.CompanyUUID); err != nil {
		return models.MembershipInvitation{}, err
	}

	if err := s.ensureInvitationsAllowed(ctx, targetUserID); err != nil {
		return models.MembershipInvitation{}, err
	}

	active, err := s.isActiveCompanyMember(ctx, input.CompanyUUID, targetUserID)
	if err != nil {
		return models.MembershipInvitation{}, err
	}
	if active {
		return models.MembershipInvitation{}, models.ErrInvalidInvitationInput
	}

	// The deputy seat holds one person, and finding that out only when the
	// invitation is accepted wastes everybody's time.
	if role == models.CompanyMemberRoleDeputy {
		taken, err := s.companyHasDeputy(ctx, input.CompanyUUID)
		if err != nil {
			return models.MembershipInvitation{}, err
		}
		if taken {
			return models.MembershipInvitation{}, models.ErrCompanyDeputyAlreadyAssigned
		}
	}

	// Working somewhere else is no longer a conflict: the invitation adds a
	// membership and leaves every other one alone.
	//
	// The member limit is checked when the invitation is accepted: a pending
	// invitation must not hold a seat.
	return s.createInvitation(ctx, models.MembershipInvitation{
		CompanyUUID:       input.CompanyUUID,
		InvitedUserUUID:   targetUserID,
		InvitedByUserUUID: input.RequestUser,
		CompanyRole:       role,
	})
}

func (s *Service) CreateDepartmentInvitation(ctx context.Context, input models.CreateDepartmentInvitationInput) (models.MembershipInvitation, error) {
	if input.CompanyUUID == uuid.Nil || input.DepartmentUUID == uuid.Nil || input.RequestUser == uuid.Nil {
		return models.MembershipInvitation{}, models.ErrInvalidInvitationInput
	}
	if input.Role != models.DepartmentMemberRoleEmployee && input.Role != models.DepartmentMemberRoleLeader {
		return models.MembershipInvitation{}, models.ErrInvalidInvitationInput
	}

	targetUserID, err := s.resolveTargetUser(ctx, input.RequestUser, input.UserUUID, input.Username)
	if err != nil {
		return models.MembershipInvitation{}, err
	}

	managesCompany, err := s.requireDepartmentInvitePermission(ctx, input, targetUserID)
	if err != nil {
		return models.MembershipInvitation{}, err
	}

	if err := s.requireActiveCompanySubscription(ctx, input.CompanyUUID); err != nil {
		return models.MembershipInvitation{}, err
	}

	if err := s.ensureInvitationsAllowed(ctx, targetUserID); err != nil {
		return models.MembershipInvitation{}, err
	}

	activeCompanyMember, err := s.isActiveCompanyMember(ctx, input.CompanyUUID, targetUserID)
	if err != nil {
		return models.MembershipInvitation{}, err
	}
	if !activeCompanyMember && s.billingLimiter != nil {
		if err := s.billingLimiter.CanAddCompanyMember(ctx, input.CompanyUUID); err != nil {
			return models.MembershipInvitation{}, err
		}
	}

	activeDepartmentMember, err := s.isActiveDepartmentMember(ctx, input.CompanyUUID, input.DepartmentUUID, targetUserID)
	if err != nil {
		return models.MembershipInvitation{}, err
	}
	if activeDepartmentMember {
		return models.MembershipInvitation{}, models.ErrInvalidInvitationInput
	}

	// Several companies at once is fine; several departments inside one company
	// is not. A colleague who already sits in another department of this company
	// is moved, not invited, so the answer names the transfer instead of
	// creating an invitation that could never be honoured.
	if activeCompanyMember {
		departments, err := s.departmentRepository.ListUserDepartments(ctx, input.CompanyUUID, targetUserID)
		if err != nil {
			return models.MembershipInvitation{}, err
		}
		if len(departments) > 0 {
			return models.MembershipInvitation{}, &models.DepartmentTransferRequired{UserUUID: targetUserID}
		}
	}

	// A leader cannot quietly undo an exclusion decided by the owner or deputy.
	approval := models.InvitationApprovalNotRequired
	if !managesCompany {
		restricted, err := s.companyRepository.HasActiveMembershipRestriction(ctx, input.CompanyUUID, targetUserID, s.now())
		if err != nil {
			return models.MembershipInvitation{}, err
		}
		if restricted {
			approval = models.InvitationApprovalPending
		}
	}

	role := input.Role
	return s.createInvitation(ctx, models.MembershipInvitation{
		CompanyUUID:       input.CompanyUUID,
		DepartmentUUID:    uuid.NullUUID{UUID: input.DepartmentUUID, Valid: true},
		InvitedUserUUID:   targetUserID,
		InvitedByUserUUID: input.RequestUser,
		CompanyRole:       models.CompanyMemberRoleEmployee,
		DepartmentRole:    &role,
		ApprovalStatus:    approval,
	})
}

func (s *Service) createInvitation(ctx context.Context, draft models.MembershipInvitation) (models.MembershipInvitation, error) {
	now := s.now()
	draft.ID = uuid.New()
	draft.Status = models.InvitationStatusPending
	draft.ExpiresAt = now.Add(defaultInvitationTTL)
	draft.CreatedAt = now
	draft.UpdatedAt = now
	if draft.ApprovalStatus == "" {
		draft.ApprovalStatus = models.InvitationApprovalNotRequired
	}

	invitation, err := s.invitationRepository.CreateInvitation(ctx, draft)
	if err != nil {
		return models.MembershipInvitation{}, err
	}

	if invitation.ApprovalStatus == models.InvitationApprovalPending {
		s.notifyApprover(ctx, invitation)
		return invitation, nil
	}

	s.notifyInvitationCreated(ctx, invitation)
	return invitation, nil
}

// ensureInvitationsAllowed respects the user's "do not disturb" switch.
func (s *Service) ensureInvitationsAllowed(ctx context.Context, userID uuid.UUID) error {
	if s.preferencesReader == nil {
		return nil
	}

	preferences, err := s.preferencesReader.Get(ctx, userID)
	if err != nil {
		return err
	}
	if preferences.InvitationsMuted {
		return models.ErrInvitationsMuted
	}

	return nil
}

// companyHasDeputy answers whether the deputy seat of this company is taken.
func (s *Service) companyHasDeputy(ctx context.Context, companyID uuid.UUID) (bool, error) {
	overview, err := s.companyRepository.GetCompanyMembersOverview(ctx, companyID)
	if err != nil {
		return false, err
	}

	return overview.Deputy != nil, nil
}

func (s *Service) notifyInvitationCreated(ctx context.Context, invitation models.MembershipInvitation) {
	s.notify(ctx, invitation.InvitedUserUUID, models.NotificationTypeInvitation, "Новое приглашение", "Вам отправили приглашение в VerbaTrace", invitation.ID)
	if s.mailer != nil {
		s.mailer.InvitationCreated(ctx, invitation.ID)
	}
}

func (s *Service) notifyApprover(ctx context.Context, invitation models.MembershipInvitation) {
	approver, err := s.approverForCompany(ctx, invitation.CompanyUUID)
	if err != nil {
		s.log.Warn(ctx, "failed to resolve invitation approver", zap.Error(err), zap.String("invitation_uuid", invitation.ID.String()))
		return
	}

	s.notify(ctx, approver, models.NotificationTypeInvitationApprovalRequested, "Приглашение ждёт одобрения", "Лидер отдела приглашает исключённого сотрудника", invitation.ID)
}

// requireDepartmentInvitePermission reports whether the actor runs the whole
// company and rejects what a department leader must never do.
func (s *Service) requireDepartmentInvitePermission(ctx context.Context, input models.CreateDepartmentInvitationInput, targetUserID uuid.UUID) (bool, error) {
	manager, err := s.companyRepository.GetCompanyMember(ctx, input.CompanyUUID, input.RequestUser)
	if err == nil && manager.Role.ManagesCompany() {
		return true, nil
	}
	if err != nil && !errors.Is(err, models.ErrCompanyNotFound) {
		return false, err
	}

	member, err := s.departmentRepository.GetDepartmentMember(ctx, input.CompanyUUID, input.DepartmentUUID, input.RequestUser)
	if err != nil {
		return false, err
	}
	if member.Role != models.DepartmentMemberRoleLeader {
		return false, models.ErrForbidden
	}
	if input.Role != models.DepartmentMemberRoleEmployee {
		return false, models.ErrForbidden
	}

	// A leader never pulls another department's leader into their own. Taking a
	// plain colleague from another department is refused too, but by the shared
	// transfer check in the caller, which answers the same way for everybody.
	departments, err := s.departmentRepository.ListUserDepartments(ctx, input.CompanyUUID, targetUserID)
	if err != nil {
		return false, err
	}
	for _, department := range departments {
		if department.Role == models.DepartmentMemberRoleLeader {
			return false, models.ErrForbidden
		}
	}

	return false, nil
}
