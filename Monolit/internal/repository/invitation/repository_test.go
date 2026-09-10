//go:build integration

package invitation

import (
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func (s *RepositorySuite) createUser(email string) models.CurrentUser {
	userID := uuid.New()
	user := models.CurrentUser{
		ID:           userID,
		Email:        email,
		PasswordHash: "hash",
		FullName:     "Dmitry",
		FullSurname:  "Mukhachev",
		Username:     "@user_" + userID.String()[:6],
		Role:         models.UserRoleUser,
		CreatedAt:    time.Now().UTC().Truncate(time.Microsecond),
	}

	created, err := s.userRepository.CreateUser(s.ctx, user)
	s.Require().NoError(err)

	return created
}

func (s *RepositorySuite) createCompanyWithManager() (models.Company, models.CurrentUser) {
	manager := s.createUser(uuid.NewString() + "@example.com")
	company := models.Company{
		ID:              uuid.New(),
		Name:            "VerbaTrace",
		ManagerUserUUID: manager.ID,
		MemberLimit:     5,
		CreatedAt:       time.Now().UTC().Truncate(time.Microsecond),
	}
	member := models.CompanyMember{
		CompanyUUID: company.ID,
		UserUUID:    manager.ID,
		Role:        models.CompanyMemberRoleManager,
		Status:      models.MembershipStatusActive,
		CreatedAt:   time.Now().UTC().Truncate(time.Microsecond),
	}

	created, err := s.companyRepository.CreateCompany(s.ctx, company, member)
	s.Require().NoError(err)

	return created, manager
}

func (s *RepositorySuite) createDepartment(companyID uuid.UUID) models.Department {
	department := models.Department{
		ID:          uuid.New(),
		CompanyUUID: companyID,
		Name:        "Sales",
		CreatedAt:   time.Now().UTC().Truncate(time.Microsecond),
	}
	created, err := s.departmentRepository.CreateDepartment(s.ctx, department)
	s.Require().NoError(err)

	return created
}

func (s *RepositorySuite) addCompanyMember(companyID uuid.UUID, userID uuid.UUID, status models.MembershipStatus) {
	_, err := s.companyRepository.AddCompanyMember(s.ctx, models.CompanyMember{
		CompanyUUID: companyID,
		UserUUID:    userID,
		Role:        models.CompanyMemberRoleEmployee,
		Status:      status,
		CreatedAt:   time.Now().UTC().Truncate(time.Microsecond),
	})
	s.Require().NoError(err)
}

func testInvitation(companyID uuid.UUID, invitedUserID uuid.UUID, invitedByUserID uuid.UUID) models.MembershipInvitation {
	now := time.Now().UTC().Truncate(time.Microsecond)
	return models.MembershipInvitation{
		ID:                uuid.New(),
		CompanyUUID:       companyID,
		InvitedUserUUID:   invitedUserID,
		InvitedByUserUUID: invitedByUserID,
		CompanyRole:       models.CompanyMemberRoleEmployee,
		Status:            models.InvitationStatusPending,
		ExpiresAt:         now.Add(time.Hour),
		CreatedAt:         now,
		UpdatedAt:         now,
	}
}

func (s *RepositorySuite) TestCreateListGetInvitation() {
	company, manager := s.createCompanyWithManager()
	invited := s.createUser(uuid.NewString() + "@example.com")
	invitation := testInvitation(company.ID, invited.ID, manager.ID)

	created, err := s.repository.CreateInvitation(s.ctx, invitation)
	s.Require().NoError(err)
	s.Require().Equal(invitation.ID, created.ID)

	got, err := s.repository.GetInvitationByUUID(s.ctx, invitation.ID)
	s.Require().NoError(err)
	s.Require().Equal(invited.ID, got.InvitedUserUUID)

	list, err := s.repository.ListUserInvitations(s.ctx, models.ListUserInvitationsInput{
		UserUUID: invited.ID,
		Status:   models.InvitationStatusPending,
	})
	s.Require().NoError(err)
	s.Require().Len(list, 1)
	s.Require().Equal(invitation.ID, list[0].ID)
}

func (s *RepositorySuite) TestUniquePendingCompanyInvitation() {
	company, manager := s.createCompanyWithManager()
	invited := s.createUser(uuid.NewString() + "@example.com")
	invitation := testInvitation(company.ID, invited.ID, manager.ID)

	_, err := s.repository.CreateInvitation(s.ctx, invitation)
	s.Require().NoError(err)

	duplicate := testInvitation(company.ID, invited.ID, manager.ID)
	_, err = s.repository.CreateInvitation(s.ctx, duplicate)
	s.Require().ErrorIs(err, models.ErrInvitationAlreadyExists)
}

func (s *RepositorySuite) TestUniquePendingDepartmentInvitation() {
	company, manager := s.createCompanyWithManager()
	department := s.createDepartment(company.ID)
	invited := s.createUser(uuid.NewString() + "@example.com")
	role := models.DepartmentMemberRoleEmployee
	invitation := testInvitation(company.ID, invited.ID, manager.ID)
	invitation.DepartmentUUID = uuid.NullUUID{UUID: department.ID, Valid: true}
	invitation.DepartmentRole = &role

	_, err := s.repository.CreateInvitation(s.ctx, invitation)
	s.Require().NoError(err)

	duplicate := testInvitation(company.ID, invited.ID, manager.ID)
	duplicate.DepartmentUUID = uuid.NullUUID{UUID: department.ID, Valid: true}
	duplicate.DepartmentRole = &role
	_, err = s.repository.CreateInvitation(s.ctx, duplicate)
	s.Require().ErrorIs(err, models.ErrInvitationAlreadyExists)
}

func (s *RepositorySuite) TestAcceptCompanyInvitationCreatesAndReactivatesMember() {
	company, manager := s.createCompanyWithManager()
	invited := s.createUser(uuid.NewString() + "@example.com")
	s.addCompanyMember(company.ID, invited.ID, models.MembershipStatusLeft)

	invitation := testInvitation(company.ID, invited.ID, manager.ID)
	created, err := s.repository.CreateInvitation(s.ctx, invitation)
	s.Require().NoError(err)

	accepted, err := s.repository.AcceptInvitation(s.ctx, created.ID, time.Now().UTC())
	s.Require().NoError(err)
	s.Require().Equal(models.InvitationStatusAccepted, accepted.Status)

	member, err := s.companyRepository.GetCompanyMember(s.ctx, company.ID, invited.ID)
	s.Require().NoError(err)
	s.Require().Equal(models.MembershipStatusActive, member.Status)
}

func (s *RepositorySuite) TestAcceptDepartmentInvitationCreatesDepartmentMemberForCompanyMember() {
	company, manager := s.createCompanyWithManager()
	previousDepartment := s.createDepartment(company.ID)
	department := s.createDepartment(company.ID)
	invited := s.createUser(uuid.NewString() + "@example.com")
	s.addCompanyMember(company.ID, invited.ID, models.MembershipStatusActive)
	_, err := s.departmentRepository.AddDepartmentMember(s.ctx, company.ID, models.DepartmentMember{
		DepartmentUUID: previousDepartment.ID,
		UserUUID:       invited.ID,
		Role:           models.DepartmentMemberRoleEmployee,
		Status:         models.MembershipStatusActive,
		CreatedAt:      time.Now().UTC().Truncate(time.Microsecond),
	})
	s.Require().NoError(err)
	role := models.DepartmentMemberRoleEmployee
	invitation := testInvitation(company.ID, invited.ID, manager.ID)
	invitation.DepartmentUUID = uuid.NullUUID{UUID: department.ID, Valid: true}
	invitation.DepartmentRole = &role
	created, err := s.repository.CreateInvitation(s.ctx, invitation)
	s.Require().NoError(err)

	accepted, err := s.repository.AcceptInvitation(s.ctx, created.ID, time.Now().UTC())
	s.Require().NoError(err)
	s.Require().Equal(models.InvitationStatusAccepted, accepted.Status)

	member, err := s.departmentRepository.GetDepartmentMember(s.ctx, company.ID, department.ID, invited.ID)
	s.Require().NoError(err)
	s.Require().Equal(models.DepartmentMemberRoleEmployee, member.Role)

	var previousStatus models.MembershipStatus
	err = s.db.QueryRowContext(s.ctx, `SELECT status FROM department_members WHERE department_uuid=$1 AND user_uuid=$2`, previousDepartment.ID, invited.ID).Scan(&previousStatus)
	s.Require().NoError(err)
	s.Require().Equal(models.MembershipStatusLeft, previousStatus)
}

func (s *RepositorySuite) TestAcceptExpiredInvitationMarksExpired() {
	company, manager := s.createCompanyWithManager()
	invited := s.createUser(uuid.NewString() + "@example.com")
	invitation := testInvitation(company.ID, invited.ID, manager.ID)
	invitation.ExpiresAt = time.Now().UTC().Add(-time.Hour)
	created, err := s.repository.CreateInvitation(s.ctx, invitation)
	s.Require().NoError(err)

	_, err = s.repository.AcceptInvitation(s.ctx, created.ID, time.Now().UTC())
	s.Require().ErrorIs(err, models.ErrInvitationExpired)

	got, err := s.repository.GetInvitationByUUID(s.ctx, created.ID)
	s.Require().NoError(err)
	s.Require().Equal(models.InvitationStatusExpired, got.Status)
}

func (s *RepositorySuite) TestListCompanyDeclineAndCancelInvitations() {
	company, manager := s.createCompanyWithManager()
	firstUser := s.createUser(uuid.NewString() + "@example.com")
	secondUser := s.createUser(uuid.NewString() + "@example.com")
	first, err := s.repository.CreateInvitation(s.ctx, testInvitation(company.ID, firstUser.ID, manager.ID))
	s.Require().NoError(err)
	second, err := s.repository.CreateInvitation(s.ctx, testInvitation(company.ID, secondUser.ID, manager.ID))
	s.Require().NoError(err)

	list, err := s.repository.ListCompanyInvitations(s.ctx, company.ID, "")
	s.Require().NoError(err)
	s.Require().Len(list, 2)

	now := time.Now().UTC()
	declined, err := s.repository.DeclineInvitation(s.ctx, first.ID, now)
	s.Require().NoError(err)
	s.Require().Equal(models.InvitationStatusDeclined, declined.Status)
	s.Require().NotNil(declined.RespondedAt)

	canceled, err := s.repository.CancelInvitation(s.ctx, second.ID, now)
	s.Require().NoError(err)
	s.Require().Equal(models.InvitationStatusCanceled, canceled.Status)

	declinedList, err := s.repository.ListCompanyInvitations(
		s.ctx, company.ID, models.InvitationStatusDeclined,
	)
	s.Require().NoError(err)
	s.Require().Len(declinedList, 1)
	s.Require().Equal(first.ID, declinedList[0].ID)

	_, err = s.repository.DeclineInvitation(s.ctx, first.ID, now)
	s.Require().ErrorIs(err, models.ErrInvitationNotPending)
	_, err = s.repository.CancelInvitation(s.ctx, uuid.New(), now)
	s.Require().ErrorIs(err, models.ErrInvitationNotFound)
}

func (s *RepositorySuite) TestGetAndAcceptMissingInvitation() {
	_, err := s.repository.GetInvitationByUUID(s.ctx, uuid.New())
	s.Require().ErrorIs(err, models.ErrInvitationNotFound)

	_, err = s.repository.AcceptInvitation(s.ctx, uuid.New(), time.Now().UTC())
	s.Require().ErrorIs(err, models.ErrInvitationNotFound)
}

func (s *RepositorySuite) TestAcceptRejectsNonPendingAndAddsDepartmentUserToCompany() {
	company, manager := s.createCompanyWithManager()
	department := s.createDepartment(company.ID)
	invited := s.createUser(uuid.NewString() + "@example.com")

	declinedInvitation, err := s.repository.CreateInvitation(
		s.ctx, testInvitation(company.ID, invited.ID, manager.ID),
	)
	s.Require().NoError(err)
	_, err = s.repository.DeclineInvitation(s.ctx, declinedInvitation.ID, time.Now().UTC())
	s.Require().NoError(err)
	_, err = s.repository.AcceptInvitation(s.ctx, declinedInvitation.ID, time.Now().UTC())
	s.Require().ErrorIs(err, models.ErrInvitationNotPending)

	role := models.DepartmentMemberRoleEmployee
	departmentInvitation := testInvitation(company.ID, invited.ID, manager.ID)
	departmentInvitation.DepartmentUUID = uuid.NullUUID{UUID: department.ID, Valid: true}
	departmentInvitation.DepartmentRole = &role
	created, err := s.repository.CreateInvitation(s.ctx, departmentInvitation)
	s.Require().NoError(err)
	accepted, err := s.repository.AcceptInvitation(s.ctx, created.ID, time.Now().UTC())
	s.Require().NoError(err)
	s.Require().Equal(models.InvitationStatusAccepted, accepted.Status)
	companyMember, err := s.companyRepository.GetCompanyMember(s.ctx, company.ID, invited.ID)
	s.Require().NoError(err)
	s.Require().Equal(models.CompanyMemberRoleEmployee, companyMember.Role)
	departmentMember, err := s.departmentRepository.GetDepartmentMember(s.ctx, company.ID, department.ID, invited.ID)
	s.Require().NoError(err)
	s.Require().Equal(models.DepartmentMemberRoleEmployee, departmentMember.Role)
}

func (s *RepositorySuite) TestDepartmentInvitationPreservesCompanyManagerRole() {
	company, manager := s.createCompanyWithManager()
	department := s.createDepartment(company.ID)
	role := models.DepartmentMemberRoleEmployee
	invitation := testInvitation(company.ID, manager.ID, manager.ID)
	invitation.DepartmentUUID = uuid.NullUUID{UUID: department.ID, Valid: true}
	invitation.DepartmentRole = &role
	invitation.CompanyRole = models.CompanyMemberRoleEmployee
	created, err := s.repository.CreateInvitation(s.ctx, invitation)
	s.Require().NoError(err)
	_, err = s.repository.AcceptInvitation(s.ctx, created.ID, time.Now().UTC())
	s.Require().NoError(err)
	var actual string
	s.Require().NoError(s.db.QueryRowContext(s.ctx, `SELECT role FROM company_members WHERE company_uuid=$1 AND user_uuid=$2`, company.ID, manager.ID).Scan(&actual))
	s.Require().Equal(string(models.CompanyMemberRoleManager), actual)
}
