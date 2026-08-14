//go:build integration

package call_folder

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
		Username:     "user_" + userID.String()[:6],
		Role:         models.UserRoleUser,
		CreatedAt:    time.Now().UTC().Truncate(time.Microsecond),
	}

	created, err := s.userRepository.CreateUser(s.ctx, user)
	s.Require().NoError(err)

	return created
}

func (s *RepositorySuite) TestCreatePersonalFolderReturnsCreatedFolder() {
	user := s.createUser(uuid.NewString() + "@example.com")
	description := "Personal sales calls"
	color := "#a855f7"
	input := models.CallFolder{
		ID:                uuid.New(),
		Scope:             models.CallFolderScopePersonal,
		UserUUID:          uuid.NullUUID{UUID: user.ID, Valid: true},
		Name:              "First personal folder",
		Description:       &description,
		Color:             &color,
		CreatedByUserUUID: user.ID,
	}

	created, err := s.repository.Create(s.ctx, input)

	s.Require().NoError(err)
	s.Require().Equal(input.ID, created.ID)
	s.Require().Equal(models.CallFolderScopePersonal, created.Scope)
	s.Require().Equal(input.UserUUID, created.UserUUID)
	s.Require().False(created.CompanyUUID.Valid)
	s.Require().False(created.DepartmentUUID.Valid)
	s.Require().Equal(input.Name, created.Name)
	s.Require().NotNil(created.Description)
	s.Require().Equal(description, *created.Description)
	s.Require().NotNil(created.Color)
	s.Require().Equal(color, *created.Color)
	s.Require().Zero(created.CallsCount)
	s.Require().Equal(user.ID, created.CreatedByUserUUID)
}

func (s *RepositorySuite) TestGrantAndRevokeFolderAccess() {
	owner := s.createUser(uuid.NewString() + "@example.com")
	target := s.createUser(uuid.NewString() + "@example.com")
	folder, err := s.repository.Create(s.ctx, models.CallFolder{
		ID: uuid.New(), Scope: models.CallFolderScopePersonal,
		UserUUID: uuid.NullUUID{UUID: owner.ID, Valid: true}, Name: "Private",
		CreatedByUserUUID: owner.ID,
	})
	s.Require().NoError(err)

	access, err := s.repository.GrantAccess(s.ctx, models.GrantCallFolderAccessInput{UserID: owner.ID, FolderUUID: folder.ID, TargetUserUUID: target.ID})
	s.Require().NoError(err)
	s.Require().Equal(target.ID, access.UserUUID)
	items, err := s.repository.ListAccesses(s.ctx, folder.ID)
	s.Require().NoError(err)
	s.Require().Len(items, 1)

	s.Require().NoError(s.repository.RevokeAccess(s.ctx, folder.ID, target.ID))
	items, err = s.repository.ListAccesses(s.ctx, folder.ID)
	s.Require().NoError(err)
	s.Require().Empty(items)
}

func (s *RepositorySuite) TestDepartmentEmployeeAutomaticallySeesDepartmentFolder() {
	manager := s.createUser(uuid.NewString() + "@example.com")
	employee := s.createUser(uuid.NewString() + "@example.com")
	companyID := uuid.New()
	departmentID := uuid.New()

	_, err := s.db.ExecContext(s.ctx, `
		INSERT INTO companies (company_uuid, name, tag, manager_user_uuid, member_limit)
		VALUES ($1, 'Visibility company', $2, $3, 10)`, companyID, "@company_"+companyID.String()[:8], manager.ID)
	s.Require().NoError(err)
	_, err = s.db.ExecContext(s.ctx, `
		INSERT INTO company_members (company_uuid, user_uuid, role, status)
		VALUES ($1, $2, 'company_manager', 'active'), ($1, $3, 'employee', 'active')`, companyID, manager.ID, employee.ID)
	s.Require().NoError(err)
	_, err = s.db.ExecContext(s.ctx, `
		INSERT INTO departments (department_uuid, company_uuid, name)
		VALUES ($1, $2, 'Sales')`, departmentID, companyID)
	s.Require().NoError(err)
	_, err = s.db.ExecContext(s.ctx, `
		INSERT INTO department_members (department_uuid, user_uuid, role, status)
		VALUES ($1, $2, 'employee', 'active')`, departmentID, employee.ID)
	s.Require().NoError(err)

	folder, err := s.repository.Create(s.ctx, models.CallFolder{
		ID: uuid.New(), Scope: models.CallFolderScopeDepartment,
		CompanyUUID:    uuid.NullUUID{UUID: companyID, Valid: true},
		DepartmentUUID: uuid.NullUUID{UUID: departmentID, Valid: true},
		Name:           "Shared sales folder", CreatedByUserUUID: manager.ID,
	})
	s.Require().NoError(err)

	visible, err := s.repository.GetVisibleByUUID(s.ctx, folder.ID, employee.ID)
	s.Require().NoError(err)
	s.Require().Equal(folder.ID, visible.ID)

	listed, err := s.repository.List(s.ctx, models.ListCallFoldersInput{
		UserID: employee.ID, Scope: models.CallFolderScopeDepartment,
		CompanyUUID:    uuid.NullUUID{UUID: companyID, Valid: true},
		DepartmentUUID: uuid.NullUUID{UUID: departmentID, Valid: true},
		Limit:          50,
	})
	s.Require().NoError(err)
	s.Require().Len(listed.Items, 1)
	s.Require().Equal(folder.ID, listed.Items[0].ID)
}
