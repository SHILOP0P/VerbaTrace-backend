//go:build integration

package company

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

func testCompany(managerID uuid.UUID) models.Company {
	return models.Company{
		ID:              uuid.New(),
		Name:            "VerbaTrace",
		ManagerUserUUID: managerID,
		MemberLimit:     5,
		CreatedAt:       time.Now().UTC().Truncate(time.Microsecond),
	}
}

func testCompanyMember(companyID uuid.UUID, userID uuid.UUID, role models.CompanyMemberRole) models.CompanyMember {
	return models.CompanyMember{
		CompanyUUID: companyID,
		UserUUID:    userID,
		Role:        role,
		Status:      models.MembershipStatusActive,
		CreatedAt:   time.Now().UTC().Truncate(time.Microsecond),
	}
}

func (s *RepositorySuite) createCompanyWithManager() (models.Company, models.CurrentUser) {
	manager := s.createUser(uuid.NewString() + "@example.com")
	company := testCompany(manager.ID)
	member := testCompanyMember(company.ID, manager.ID, models.CompanyMemberRoleManager)

	created, err := s.repository.CreateCompany(s.ctx, company, member)
	s.Require().NoError(err)

	return created, manager
}

func (s *RepositorySuite) TestCreateCompanyCreatesManagerMember() {
	company, manager := s.createCompanyWithManager()

	s.Require().Equal("VerbaTrace", company.Name)
	s.Require().Equal(manager.ID, company.ManagerUserUUID)
	s.Require().Equal("@"+company.ID.String(), company.Tag)

	gotCompany, err := s.repository.GetManagedCompanyByUserUUID(s.ctx, manager.ID)
	s.Require().NoError(err)
	s.Require().Equal(company.ID, gotCompany.ID)

	member, err := s.repository.GetCompanyMember(s.ctx, company.ID, manager.ID)
	s.Require().NoError(err)
	s.Require().Equal(models.CompanyMemberRoleManager, member.Role)
	s.Require().Equal(models.MembershipStatusActive, member.Status)
}

func (s *RepositorySuite) TestUpdateCompanyTag() {
	company, _ := s.createCompanyWithManager()
	updated, err := s.repository.UpdateCompanyTag(s.ctx, company.ID, "@verbatrace_team")
	s.Require().NoError(err)
	s.Require().Equal("@verbatrace_team", updated.Tag)
}

func (s *RepositorySuite) TestCreateCompanyAllowsSecondManagedCompanyForSameUser() {
	_, manager := s.createCompanyWithManager()

	anotherCompany := testCompany(manager.ID)
	anotherMember := testCompanyMember(anotherCompany.ID, manager.ID, models.CompanyMemberRoleManager)

	created, err := s.repository.CreateCompany(s.ctx, anotherCompany, anotherMember)

	s.Require().NoError(err)
	s.Require().Equal(anotherCompany.ID, created.ID)
}

func (s *RepositorySuite) TestGetManagedCompanyByUserUUIDNotFound() {
	_, err := s.repository.GetManagedCompanyByUserUUID(s.ctx, uuid.New())

	s.Require().ErrorIs(err, models.ErrCompanyNotFound)
}

func (s *RepositorySuite) TestAddCompanyMemberAndGetCompanyByUUID() {
	company, manager := s.createCompanyWithManager()
	employee := s.createUser(uuid.NewString() + "@example.com")
	member := testCompanyMember(company.ID, employee.ID, models.CompanyMemberRoleEmployee)

	createdMember, err := s.repository.AddCompanyMember(s.ctx, member)
	s.Require().NoError(err)
	s.Require().Equal(employee.ID, createdMember.UserUUID)
	s.Require().Equal(models.CompanyMemberRoleEmployee, createdMember.Role)

	gotMember, err := s.repository.GetCompanyMember(s.ctx, company.ID, employee.ID)
	s.Require().NoError(err)
	s.Require().Equal(createdMember, gotMember)

	managerCompany, err := s.repository.GetCompanyByUUID(s.ctx, company.ID, manager.ID)
	s.Require().NoError(err)
	s.Require().Equal(company.ID, managerCompany.ID)

	employeeCompany, err := s.repository.GetCompanyByUUID(s.ctx, company.ID, employee.ID)
	s.Require().NoError(err)
	s.Require().Equal(company.ID, employeeCompany.ID)
}

func (s *RepositorySuite) TestUpdateCompanyMemberJobTitle() {
	company, _ := s.createCompanyWithManager()
	employee := s.createUser(uuid.NewString() + "@example.com")
	_, err := s.repository.AddCompanyMember(
		s.ctx,
		testCompanyMember(company.ID, employee.ID, models.CompanyMemberRoleEmployee),
	)
	s.Require().NoError(err)

	title := "Backend developer"
	updated, err := s.repository.UpdateCompanyMemberJobTitle(s.ctx, company.ID, employee.ID, &title)
	s.Require().NoError(err)
	s.Require().Equal(employee.ID, updated.UserUUID)
	s.Require().Equal(&title, updated.JobTitle)

	cleared, err := s.repository.UpdateCompanyMemberJobTitle(s.ctx, company.ID, employee.ID, nil)
	s.Require().NoError(err)
	s.Require().Nil(cleared.JobTitle)
}

func (s *RepositorySuite) TestAddCompanyMemberUpsertsExistingMember() {
	company, _ := s.createCompanyWithManager()
	employee := s.createUser(uuid.NewString() + "@example.com")
	member := testCompanyMember(company.ID, employee.ID, models.CompanyMemberRoleEmployee)

	_, err := s.repository.AddCompanyMember(s.ctx, member)
	s.Require().NoError(err)

	member.Status = models.MembershipStatusSuspended
	upserted, err := s.repository.AddCompanyMember(s.ctx, member)
	s.Require().NoError(err)
	s.Require().Equal(models.MembershipStatusSuspended, upserted.Status)

	_, err = s.repository.GetCompanyMember(s.ctx, company.ID, employee.ID)
	s.Require().ErrorIs(err, models.ErrCompanyNotFound)
}

func (s *RepositorySuite) TestGetCompanyMemberNotFoundForMissingOrInactiveMember() {
	company, _ := s.createCompanyWithManager()
	employee := s.createUser(uuid.NewString() + "@example.com")

	_, err := s.repository.GetCompanyMember(s.ctx, company.ID, employee.ID)
	s.Require().ErrorIs(err, models.ErrCompanyNotFound)

	member := testCompanyMember(company.ID, employee.ID, models.CompanyMemberRoleEmployee)
	member.Status = models.MembershipStatusSuspended
	_, err = s.repository.AddCompanyMember(s.ctx, member)
	s.Require().NoError(err)

	_, err = s.repository.GetCompanyMember(s.ctx, company.ID, employee.ID)
	s.Require().ErrorIs(err, models.ErrCompanyNotFound)
}

func (s *RepositorySuite) TestListUserCompaniesReturnsOnlyActiveMemberships() {
	company, _ := s.createCompanyWithManager()
	activeUser := s.createUser(uuid.NewString() + "@example.com")
	suspendedUser := s.createUser(uuid.NewString() + "@example.com")

	_, err := s.repository.AddCompanyMember(s.ctx, testCompanyMember(company.ID, activeUser.ID, models.CompanyMemberRoleEmployee))
	s.Require().NoError(err)

	suspendedMember := testCompanyMember(company.ID, suspendedUser.ID, models.CompanyMemberRoleEmployee)
	suspendedMember.Status = models.MembershipStatusSuspended
	_, err = s.repository.AddCompanyMember(s.ctx, suspendedMember)
	s.Require().NoError(err)

	activeCompanies, err := s.repository.ListUserCompanies(s.ctx, activeUser.ID)
	s.Require().NoError(err)
	s.Require().Len(activeCompanies, 1)
	s.Require().Equal(company.ID, activeCompanies[0].ID)

	suspendedCompanies, err := s.repository.ListUserCompanies(s.ctx, suspendedUser.ID)
	s.Require().NoError(err)
	s.Require().Empty(suspendedCompanies)
}

func (s *RepositorySuite) TestGetCompanyByUUIDRejectsInactiveOrMissingMember() {
	company, _ := s.createCompanyWithManager()
	outsider := s.createUser(uuid.NewString() + "@example.com")

	_, err := s.repository.GetCompanyByUUID(s.ctx, company.ID, outsider.ID)
	s.Require().ErrorIs(err, models.ErrCompanyNotFound)

	member := testCompanyMember(company.ID, outsider.ID, models.CompanyMemberRoleEmployee)
	member.Status = models.MembershipStatusLeft
	_, err = s.repository.AddCompanyMember(s.ctx, member)
	s.Require().NoError(err)

	_, err = s.repository.GetCompanyByUUID(s.ctx, company.ID, outsider.ID)
	s.Require().ErrorIs(err, models.ErrCompanyNotFound)
}

func (s *RepositorySuite) TestUpdateCompanyMemberRole() {
	company, _ := s.createCompanyWithManager()
	employee := s.createUser(uuid.NewString() + "@example.com")
	_, err := s.repository.AddCompanyMember(s.ctx, testCompanyMember(company.ID, employee.ID, models.CompanyMemberRoleEmployee))
	s.Require().NoError(err)

	updated, err := s.repository.UpdateCompanyMemberRole(s.ctx, company.ID, employee.ID, models.CompanyMemberRoleEmployee)
	s.Require().NoError(err)
	s.Require().Equal(models.CompanyMemberRoleEmployee, updated.Role)
}

func (s *RepositorySuite) TestUpdateCompanyMemberRoleDoesNotUpdateManagerOrInactiveMember() {
	company, manager := s.createCompanyWithManager()

	_, err := s.repository.UpdateCompanyMemberRole(s.ctx, company.ID, manager.ID, models.CompanyMemberRoleEmployee)
	s.Require().ErrorIs(err, models.ErrCompanyNotFound)

	employee := s.createUser(uuid.NewString() + "@example.com")
	member := testCompanyMember(company.ID, employee.ID, models.CompanyMemberRoleEmployee)
	member.Status = models.MembershipStatusSuspended
	_, err = s.repository.AddCompanyMember(s.ctx, member)
	s.Require().NoError(err)

	_, err = s.repository.UpdateCompanyMemberRole(s.ctx, company.ID, employee.ID, models.CompanyMemberRoleEmployee)
	s.Require().ErrorIs(err, models.ErrCompanyNotFound)
}

func (s *RepositorySuite) TestUpdateCompanyMemberStatus() {
	company, _ := s.createCompanyWithManager()
	employee := s.createUser(uuid.NewString() + "@example.com")
	_, err := s.repository.AddCompanyMember(s.ctx, testCompanyMember(company.ID, employee.ID, models.CompanyMemberRoleEmployee))
	s.Require().NoError(err)

	updated, err := s.repository.UpdateCompanyMemberStatus(s.ctx, company.ID, employee.ID, models.MembershipStatusSuspended)
	s.Require().NoError(err)
	s.Require().Equal(models.MembershipStatusSuspended, updated.Status)

	_, err = s.repository.GetCompanyMember(s.ctx, company.ID, employee.ID)
	s.Require().ErrorIs(err, models.ErrCompanyNotFound)
}

func (s *RepositorySuite) TestUpdateCompanyMemberStatusDoesNotUpdateManager() {
	company, manager := s.createCompanyWithManager()

	_, err := s.repository.UpdateCompanyMemberStatus(s.ctx, company.ID, manager.ID, models.MembershipStatusSuspended)

	s.Require().ErrorIs(err, models.ErrCompanyNotFound)
}

func (s *RepositorySuite) TestGetCompanyMembersOverview() {
	company, manager := s.createCompanyWithManager()
	employee := s.createUser(uuid.NewString() + "@example.com")
	departmentEmployee := s.createUser(uuid.NewString() + "@example.com")
	suspendedEmployee := s.createUser(uuid.NewString() + "@example.com")

	_, err := s.repository.AddCompanyMember(s.ctx, testCompanyMember(company.ID, employee.ID, models.CompanyMemberRoleEmployee))
	s.Require().NoError(err)
	_, err = s.repository.AddCompanyMember(s.ctx, testCompanyMember(company.ID, departmentEmployee.ID, models.CompanyMemberRoleEmployee))
	s.Require().NoError(err)
	suspendedMember := testCompanyMember(company.ID, suspendedEmployee.ID, models.CompanyMemberRoleEmployee)
	suspendedMember.Status = models.MembershipStatusSuspended
	_, err = s.repository.AddCompanyMember(s.ctx, suspendedMember)
	s.Require().NoError(err)

	departmentID := uuid.New()
	_, err = s.db.ExecContext(
		s.ctx,
		`INSERT INTO departments (department_uuid, company_uuid, name, created_at) VALUES ($1, $2, $3, $4)`,
		departmentID,
		company.ID,
		"Sales",
		time.Now().UTC().Truncate(time.Microsecond),
	)
	s.Require().NoError(err)

	_, err = s.db.ExecContext(
		s.ctx,
		`INSERT INTO department_members (department_uuid, user_uuid, role, status, created_at) VALUES ($1, $2, $3, $4, $5)`,
		departmentID,
		departmentEmployee.ID,
		string(models.DepartmentMemberRoleEmployee),
		string(models.MembershipStatusActive),
		time.Now().UTC().Truncate(time.Microsecond),
	)
	s.Require().NoError(err)
	_, err = s.db.ExecContext(
		s.ctx,
		`INSERT INTO department_members (department_uuid, user_uuid, role, status, created_at) VALUES ($1, $2, $3, $4, $5)`,
		departmentID,
		suspendedEmployee.ID,
		string(models.DepartmentMemberRoleEmployee),
		string(models.MembershipStatusSuspended),
		time.Now().UTC().Truncate(time.Microsecond),
	)
	s.Require().NoError(err)

	overview, err := s.repository.GetCompanyMembersOverview(s.ctx, company.ID)
	s.Require().NoError(err)
	s.Require().Equal(company.ID, overview.CompanyUUID)
	s.Require().NotNil(overview.Manager)
	s.Require().Equal(manager.ID, overview.Manager.UserUUID)
	s.Require().Equal(manager.Username, overview.Manager.Username)
	s.Require().Equal(manager.FullName, overview.Manager.FullName)
	s.Require().Equal(manager.FullSurname, overview.Manager.FullSurname)
	s.Require().Len(overview.CompanyEmployees, 2)
	var employeeOverview *models.CompanyMember
	for i := range overview.CompanyEmployees {
		if overview.CompanyEmployees[i].UserUUID == employee.ID {
			employeeOverview = &overview.CompanyEmployees[i]
			break
		}
	}
	s.Require().NotNil(employeeOverview)
	s.Require().Equal(employee.Username, employeeOverview.Username)
	s.Require().Equal(employee.FullName, employeeOverview.FullName)
	s.Require().Equal(employee.FullSurname, employeeOverview.FullSurname)
	s.Require().Len(overview.Departments, 1)
	s.Require().Equal(departmentID, overview.Departments[0].Department.ID)
	s.Require().Len(overview.Departments[0].Members, 1)
	s.Require().Equal(departmentEmployee.ID, overview.Departments[0].Members[0].UserUUID)
	s.Require().Equal(departmentEmployee.Username, overview.Departments[0].Members[0].Username)
	s.Require().Equal(departmentEmployee.FullName, overview.Departments[0].Members[0].FullName)
	s.Require().Equal(departmentEmployee.FullSurname, overview.Departments[0].Members[0].FullSurname)
}
