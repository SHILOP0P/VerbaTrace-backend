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

func (s *RepositorySuite) addMember(companyID uuid.UUID, userID uuid.UUID, role models.CompanyMemberRole, status models.MembershipStatus) {
	_, err := s.db.ExecContext(
		s.ctx,
		`INSERT INTO company_members (company_uuid, user_uuid, role, status)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (company_uuid, user_uuid)
		 DO UPDATE SET role = EXCLUDED.role, status = EXCLUDED.status`,
		companyID,
		userID,
		string(role),
		string(status),
	)
	s.Require().NoError(err)
}

func (s *RepositorySuite) TestCompanyMemberVisibility() {
	company, manager := s.createCompanyWithManager()
	employee := s.createUser(uuid.NewString() + "@example.com")
	s.addMember(company.ID, employee.ID, models.CompanyMemberRoleEmployee, models.MembershipStatusActive)

	gotMember, err := s.repository.GetCompanyMember(s.ctx, company.ID, employee.ID)
	s.Require().NoError(err)
	s.Require().Equal(employee.ID, gotMember.UserUUID)
	s.Require().Equal(models.CompanyMemberRoleEmployee, gotMember.Role)

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
	s.addMember(company.ID, employee.ID, models.CompanyMemberRoleEmployee, models.MembershipStatusActive)

	title := "Backend developer"
	updated, err := s.repository.UpdateCompanyMemberJobTitle(s.ctx, company.ID, employee.ID, &title)
	s.Require().NoError(err)
	s.Require().Equal(employee.ID, updated.UserUUID)
	s.Require().Equal(&title, updated.JobTitle)

	cleared, err := s.repository.UpdateCompanyMemberJobTitle(s.ctx, company.ID, employee.ID, nil)
	s.Require().NoError(err)
	s.Require().Nil(cleared.JobTitle)
}

func (s *RepositorySuite) TestGetCompanyMemberNotFoundForMissingOrInactiveMember() {
	company, _ := s.createCompanyWithManager()
	employee := s.createUser(uuid.NewString() + "@example.com")

	_, err := s.repository.GetCompanyMember(s.ctx, company.ID, employee.ID)
	s.Require().ErrorIs(err, models.ErrCompanyNotFound)

	s.addMember(company.ID, employee.ID, models.CompanyMemberRoleEmployee, models.MembershipStatusLeft)

	_, err = s.repository.GetCompanyMember(s.ctx, company.ID, employee.ID)
	s.Require().ErrorIs(err, models.ErrCompanyNotFound)
}

func (s *RepositorySuite) TestListUserCompaniesReturnsOnlyActiveMemberships() {
	company, _ := s.createCompanyWithManager()
	activeUser := s.createUser(uuid.NewString() + "@example.com")
	formerUser := s.createUser(uuid.NewString() + "@example.com")

	s.addMember(company.ID, activeUser.ID, models.CompanyMemberRoleEmployee, models.MembershipStatusActive)
	s.addMember(company.ID, formerUser.ID, models.CompanyMemberRoleEmployee, models.MembershipStatusLeft)

	activeCompanies, err := s.repository.ListUserCompanies(s.ctx, activeUser.ID)
	s.Require().NoError(err)
	s.Require().Len(activeCompanies, 1)
	s.Require().Equal(company.ID, activeCompanies[0].ID)

	formerCompanies, err := s.repository.ListUserCompanies(s.ctx, formerUser.ID)
	s.Require().NoError(err)
	s.Require().Empty(formerCompanies)
}

func (s *RepositorySuite) TestGetCompanyByUUIDRejectsInactiveOrMissingMember() {
	company, _ := s.createCompanyWithManager()
	outsider := s.createUser(uuid.NewString() + "@example.com")

	_, err := s.repository.GetCompanyByUUID(s.ctx, company.ID, outsider.ID)
	s.Require().ErrorIs(err, models.ErrCompanyNotFound)

	s.addMember(company.ID, outsider.ID, models.CompanyMemberRoleEmployee, models.MembershipStatusLeft)

	_, err = s.repository.GetCompanyByUUID(s.ctx, company.ID, outsider.ID)
	s.Require().ErrorIs(err, models.ErrCompanyNotFound)
}

// An employee belongs to one company, which the database has to guarantee even
// if some future code path forgets to check it.
func (s *RepositorySuite) TestSingleActiveEmployerIsEnforcedByDatabase() {
	first, _ := s.createCompanyWithManager()
	second, _ := s.createCompanyWithManager()
	employee := s.createUser(uuid.NewString() + "@example.com")

	s.addMember(first.ID, employee.ID, models.CompanyMemberRoleEmployee, models.MembershipStatusActive)

	_, err := s.db.ExecContext(
		s.ctx,
		`INSERT INTO company_members (company_uuid, user_uuid, role, status) VALUES ($1, $2, 'employee', 'active')`,
		second.ID,
		employee.ID,
	)
	s.Require().Error(err)

	employer, err := s.repository.ActiveEmployerCompany(s.ctx, employee.ID)
	s.Require().NoError(err)
	s.Require().Equal(first.ID, employer.ID)
}

func (s *RepositorySuite) TestAssignAndRevokeCompanyDeputy() {
	company, _ := s.createCompanyWithManager()
	employee := s.createUser(uuid.NewString() + "@example.com")
	other := s.createUser(uuid.NewString() + "@example.com")
	s.addMember(company.ID, employee.ID, models.CompanyMemberRoleEmployee, models.MembershipStatusActive)
	s.addMember(company.ID, other.ID, models.CompanyMemberRoleEmployee, models.MembershipStatusActive)

	deputy, err := s.repository.AssignCompanyDeputy(s.ctx, company.ID, employee.ID)
	s.Require().NoError(err)
	s.Require().Equal(models.CompanyMemberRoleDeputy, deputy.Role)

	_, err = s.repository.AssignCompanyDeputy(s.ctx, company.ID, other.ID)
	s.Require().ErrorIs(err, models.ErrCompanyDeputyAlreadyAssigned)

	revoked, err := s.repository.RevokeCompanyDeputy(s.ctx, company.ID)
	s.Require().NoError(err)
	s.Require().Equal(employee.ID, revoked.UserUUID)
	s.Require().Equal(models.CompanyMemberRoleEmployee, revoked.Role)

	_, err = s.repository.RevokeCompanyDeputy(s.ctx, company.ID)
	s.Require().ErrorIs(err, models.ErrCompanyDeputyNotAssigned)
}

// Leaving takes away everything the company gave the person.
func (s *RepositorySuite) TestRemoveCompanyMemberClearsAccess() {
	company, _ := s.createCompanyWithManager()
	employee := s.createUser(uuid.NewString() + "@example.com")
	s.addMember(company.ID, employee.ID, models.CompanyMemberRoleEmployee, models.MembershipStatusActive)

	departmentID := uuid.New()
	_, err := s.db.ExecContext(
		s.ctx,
		`INSERT INTO departments (department_uuid, company_uuid, name) VALUES ($1, $2, 'Sales')`,
		departmentID,
		company.ID,
	)
	s.Require().NoError(err)
	_, err = s.db.ExecContext(
		s.ctx,
		`INSERT INTO department_members (department_uuid, user_uuid, role, status) VALUES ($1, $2, 'employee', 'active')`,
		departmentID,
		employee.ID,
	)
	s.Require().NoError(err)

	removed, err := s.repository.RemoveCompanyMember(s.ctx, company.ID, employee.ID, time.Now().UTC())
	s.Require().NoError(err)
	s.Require().Equal(models.MembershipStatusLeft, removed.Status)

	var departmentStatus string
	err = s.db.QueryRowContext(
		s.ctx,
		`SELECT status FROM department_members WHERE department_uuid = $1 AND user_uuid = $2`,
		departmentID,
		employee.ID,
	).Scan(&departmentStatus)
	s.Require().NoError(err)
	s.Require().Equal("left", departmentStatus)

	count, err := s.repository.CountActiveCompanyMembersExcept(s.ctx, company.ID, company.ManagerUserUUID)
	s.Require().NoError(err)
	s.Require().Zero(count)
}

func (s *RepositorySuite) TestMembershipRestrictionLifecycle() {
	company, manager := s.createCompanyWithManager()
	employee := s.createUser(uuid.NewString() + "@example.com")
	now := time.Now().UTC()

	restricted, err := s.repository.HasActiveMembershipRestriction(s.ctx, company.ID, employee.ID, now)
	s.Require().NoError(err)
	s.Require().False(restricted)

	s.Require().NoError(s.repository.UpsertMembershipRestriction(s.ctx, models.CompanyMembershipRestriction{
		ID:                uuid.New(),
		CompanyUUID:       company.ID,
		UserUUID:          employee.ID,
		Kind:              models.CompanyRestrictionExcludedByManager,
		CreatedByUserUUID: manager.ID,
		CreatedAt:         now,
		ExpiresAt:         now.Add(time.Hour),
	}))

	restricted, err = s.repository.HasActiveMembershipRestriction(s.ctx, company.ID, employee.ID, now)
	s.Require().NoError(err)
	s.Require().True(restricted)

	deleted, err := s.repository.DeleteExpiredMembershipRestrictions(s.ctx, now.Add(2*time.Hour))
	s.Require().NoError(err)
	s.Require().Equal(int64(1), deleted)
}

func (s *RepositorySuite) TestGetCompanyMembersOverview() {
	company, manager := s.createCompanyWithManager()
	employee := s.createUser(uuid.NewString() + "@example.com")
	departmentEmployee := s.createUser(uuid.NewString() + "@example.com")
	formerEmployee := s.createUser(uuid.NewString() + "@example.com")

	s.addMember(company.ID, employee.ID, models.CompanyMemberRoleEmployee, models.MembershipStatusActive)
	s.addMember(company.ID, departmentEmployee.ID, models.CompanyMemberRoleEmployee, models.MembershipStatusActive)
	s.addMember(company.ID, formerEmployee.ID, models.CompanyMemberRoleEmployee, models.MembershipStatusLeft)

	departmentID := uuid.New()
	_, err := s.db.ExecContext(
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
		formerEmployee.ID,
		string(models.DepartmentMemberRoleEmployee),
		string(models.MembershipStatusLeft),
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
