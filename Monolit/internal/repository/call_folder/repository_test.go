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
