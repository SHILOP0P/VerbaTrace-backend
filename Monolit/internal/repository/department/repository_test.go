//go:build integration

package department

import (
	"time"

	"calllens/monolit/internal/models"

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
		Name:            "CallLens",
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

func testDepartment(companyID uuid.UUID) models.Department {
	return models.Department{
		ID:          uuid.New(),
		CompanyUUID: companyID,
		Name:        "Sales",
		CreatedAt:   time.Now().UTC().Truncate(time.Microsecond),
	}
}

func testDepartmentMember(departmentID uuid.UUID, userID uuid.UUID, role models.DepartmentMemberRole) models.DepartmentMember {
	return models.DepartmentMember{
		DepartmentUUID: departmentID,
		UserUUID:       userID,
		Role:           role,
		Status:         models.MembershipStatusActive,
		CreatedAt:      time.Now().UTC().Truncate(time.Microsecond),
	}
}

func (s *RepositorySuite) createDepartmentWithCompany() (models.Department, models.Company, models.CurrentUser) {
	company, manager := s.createCompanyWithManager()
	department, err := s.repository.CreateDepartment(s.ctx, testDepartment(company.ID))
	s.Require().NoError(err)

	return department, company, manager
}

func (s *RepositorySuite) addCompanyEmployee(companyID uuid.UUID) models.CurrentUser {
	user := s.createUser(uuid.NewString() + "@example.com")
	_, err := s.companyRepository.AddCompanyMember(s.ctx, models.CompanyMember{
		CompanyUUID: companyID,
		UserUUID:    user.ID,
		Role:        models.CompanyMemberRoleEmployee,
		Status:      models.MembershipStatusActive,
		CreatedAt:   time.Now().UTC().Truncate(time.Microsecond),
	})
	s.Require().NoError(err)

	return user
}

func (s *RepositorySuite) TestCreateDepartment() {
	company, _ := s.createCompanyWithManager()

	department, err := s.repository.CreateDepartment(s.ctx, testDepartment(company.ID))

	s.Require().NoError(err)
	s.Require().Equal(company.ID, department.CompanyUUID)
	s.Require().Equal("Sales", department.Name)
}

func (s *RepositorySuite) TestCreateDepartmentRejectsMissingCompany() {
	_, err := s.repository.CreateDepartment(s.ctx, testDepartment(uuid.New()))

	s.Require().Error(err)
}

func (s *RepositorySuite) TestAddDepartmentMemberAndGetMember() {
	department, company, _ := s.createDepartmentWithCompany()
	employee := s.addCompanyEmployee(company.ID)
	member := testDepartmentMember(department.ID, employee.ID, models.DepartmentMemberRoleEmployee)

	created, err := s.repository.AddDepartmentMember(s.ctx, company.ID, member)
	s.Require().NoError(err)
	s.Require().Equal(employee.ID, created.UserUUID)
	s.Require().Equal(models.DepartmentMemberRoleEmployee, created.Role)

	got, err := s.repository.GetDepartmentMember(s.ctx, company.ID, department.ID, employee.ID)
	s.Require().NoError(err)
	s.Require().Equal(created, got)
}

func (s *RepositorySuite) TestAddDepartmentMemberRejectsWrongCompanyOrMissingDepartment() {
	department, _, _ := s.createDepartmentWithCompany()
	employee := s.createUser(uuid.NewString() + "@example.com")

	_, err := s.repository.AddDepartmentMember(s.ctx, uuid.New(), testDepartmentMember(department.ID, employee.ID, models.DepartmentMemberRoleEmployee))
	s.Require().ErrorIs(err, models.ErrDepartmentNotFound)

	_, err = s.repository.AddDepartmentMember(s.ctx, uuid.New(), testDepartmentMember(uuid.New(), employee.ID, models.DepartmentMemberRoleEmployee))
	s.Require().ErrorIs(err, models.ErrDepartmentNotFound)
}

func (s *RepositorySuite) TestAddDepartmentMemberUpsertsExistingMember() {
	department, company, _ := s.createDepartmentWithCompany()
	employee := s.addCompanyEmployee(company.ID)
	member := testDepartmentMember(department.ID, employee.ID, models.DepartmentMemberRoleEmployee)

	_, err := s.repository.AddDepartmentMember(s.ctx, company.ID, member)
	s.Require().NoError(err)

	member.Role = models.DepartmentMemberRoleLeader
	updated, err := s.repository.AddDepartmentMember(s.ctx, company.ID, member)
	s.Require().NoError(err)
	s.Require().Equal(models.DepartmentMemberRoleLeader, updated.Role)
}

func (s *RepositorySuite) TestGetDepartmentMemberReturnsNotFoundForInactiveOrWrongCompany() {
	department, company, _ := s.createDepartmentWithCompany()
	employee := s.addCompanyEmployee(company.ID)
	member := testDepartmentMember(department.ID, employee.ID, models.DepartmentMemberRoleEmployee)
	member.Status = models.MembershipStatusSuspended
	_, err := s.repository.AddDepartmentMember(s.ctx, company.ID, member)
	s.Require().NoError(err)

	_, err = s.repository.GetDepartmentMember(s.ctx, company.ID, department.ID, employee.ID)
	s.Require().ErrorIs(err, models.ErrDepartmentNotFound)

	_, err = s.repository.GetDepartmentMember(s.ctx, uuid.New(), department.ID, employee.ID)
	s.Require().ErrorIs(err, models.ErrDepartmentNotFound)
}

func (s *RepositorySuite) TestListDepartmentMembersReturnsOnlyActiveMembers() {
	department, company, _ := s.createDepartmentWithCompany()
	active := s.addCompanyEmployee(company.ID)
	suspended := s.addCompanyEmployee(company.ID)

	_, err := s.repository.AddDepartmentMember(s.ctx, company.ID, testDepartmentMember(department.ID, active.ID, models.DepartmentMemberRoleEmployee))
	s.Require().NoError(err)

	suspendedMember := testDepartmentMember(department.ID, suspended.ID, models.DepartmentMemberRoleEmployee)
	suspendedMember.Status = models.MembershipStatusSuspended
	_, err = s.repository.AddDepartmentMember(s.ctx, company.ID, suspendedMember)
	s.Require().NoError(err)

	members, err := s.repository.ListDepartmentMembers(s.ctx, company.ID, department.ID)
	s.Require().NoError(err)
	s.Require().Len(members, 1)
	s.Require().Equal(active.ID, members[0].UserUUID)
	s.Require().Equal(active.Username, members[0].Username)
	s.Require().Equal(active.FullName, members[0].FullName)
	s.Require().Equal(active.FullSurname, members[0].FullSurname)
}

func (s *RepositorySuite) TestListDepartmentMembersRejectsMissingDepartment() {
	_, err := s.repository.ListDepartmentMembers(s.ctx, uuid.New(), uuid.New())

	s.Require().ErrorIs(err, models.ErrDepartmentNotFound)
}

func (s *RepositorySuite) TestListVisibleCompanyDepartments() {
	firstDepartment, company, manager := s.createDepartmentWithCompany()
	secondDepartment, err := s.repository.CreateDepartment(s.ctx, models.Department{
		ID:          uuid.New(),
		CompanyUUID: company.ID,
		Name:        "Support",
		CreatedAt:   time.Now().UTC().Add(time.Second).Truncate(time.Microsecond),
	})
	s.Require().NoError(err)
	departmentEmployee := s.addCompanyEmployee(company.ID)
	outsider := s.createUser(uuid.NewString() + "@example.com")

	_, err = s.repository.AddDepartmentMember(s.ctx, company.ID, testDepartmentMember(firstDepartment.ID, departmentEmployee.ID, models.DepartmentMemberRoleEmployee))
	s.Require().NoError(err)

	managerDepartments, err := s.repository.ListVisibleCompanyDepartments(s.ctx, company.ID, manager.ID)
	s.Require().NoError(err)
	s.Require().Len(managerDepartments, 2)

	employeeDepartments, err := s.repository.ListVisibleCompanyDepartments(s.ctx, company.ID, departmentEmployee.ID)
	s.Require().NoError(err)
	s.Require().Len(employeeDepartments, 1)
	s.Require().Equal(firstDepartment.ID, employeeDepartments[0].ID)

	outsiderDepartments, err := s.repository.ListVisibleCompanyDepartments(s.ctx, company.ID, outsider.ID)
	s.Require().NoError(err)
	s.Require().Empty(outsiderDepartments)

	s.Require().NotEqual(secondDepartment.ID, employeeDepartments[0].ID)
}

func (s *RepositorySuite) TestUpdateDepartmentMemberRole() {
	department, company, _ := s.createDepartmentWithCompany()
	employee := s.addCompanyEmployee(company.ID)
	_, err := s.repository.AddDepartmentMember(s.ctx, company.ID, testDepartmentMember(department.ID, employee.ID, models.DepartmentMemberRoleEmployee))
	s.Require().NoError(err)

	updated, err := s.repository.UpdateDepartmentMemberRole(s.ctx, company.ID, department.ID, employee.ID, models.DepartmentMemberRoleLeader)
	s.Require().NoError(err)
	s.Require().Equal(models.DepartmentMemberRoleLeader, updated.Role)
}

func (s *RepositorySuite) TestUpdateDepartmentMemberRoleRejectsInactiveOrWrongCompany() {
	department, company, _ := s.createDepartmentWithCompany()
	employee := s.addCompanyEmployee(company.ID)
	member := testDepartmentMember(department.ID, employee.ID, models.DepartmentMemberRoleEmployee)
	member.Status = models.MembershipStatusSuspended
	_, err := s.repository.AddDepartmentMember(s.ctx, company.ID, member)
	s.Require().NoError(err)

	_, err = s.repository.UpdateDepartmentMemberRole(s.ctx, company.ID, department.ID, employee.ID, models.DepartmentMemberRoleLeader)
	s.Require().ErrorIs(err, models.ErrDepartmentNotFound)

	_, err = s.repository.UpdateDepartmentMemberRole(s.ctx, uuid.New(), department.ID, employee.ID, models.DepartmentMemberRoleLeader)
	s.Require().ErrorIs(err, models.ErrDepartmentNotFound)
}

func (s *RepositorySuite) TestUpdateDepartmentMemberStatus() {
	department, company, _ := s.createDepartmentWithCompany()
	employee := s.addCompanyEmployee(company.ID)
	_, err := s.repository.AddDepartmentMember(s.ctx, company.ID, testDepartmentMember(department.ID, employee.ID, models.DepartmentMemberRoleEmployee))
	s.Require().NoError(err)

	updated, err := s.repository.UpdateDepartmentMemberStatus(s.ctx, company.ID, department.ID, employee.ID, models.MembershipStatusSuspended)
	s.Require().NoError(err)
	s.Require().Equal(models.MembershipStatusSuspended, updated.Status)

	_, err = s.repository.GetDepartmentMember(s.ctx, company.ID, department.ID, employee.ID)
	s.Require().ErrorIs(err, models.ErrDepartmentNotFound)
}

func (s *RepositorySuite) TestUpdateDepartmentMemberStatusRejectsWrongCompany() {
	department, company, _ := s.createDepartmentWithCompany()
	employee := s.addCompanyEmployee(company.ID)
	_, err := s.repository.AddDepartmentMember(s.ctx, company.ID, testDepartmentMember(department.ID, employee.ID, models.DepartmentMemberRoleEmployee))
	s.Require().NoError(err)

	_, err = s.repository.UpdateDepartmentMemberStatus(s.ctx, uuid.New(), department.ID, employee.ID, models.MembershipStatusSuspended)

	s.Require().ErrorIs(err, models.ErrDepartmentNotFound)
}
