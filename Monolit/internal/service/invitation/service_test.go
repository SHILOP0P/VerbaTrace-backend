package invitation

import (
	"context"
	"testing"
	"time"

	"calllens/monolit/internal/logger"
	"calllens/monolit/internal/models"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestCreateCompanyInvitationAllowsManager(t *testing.T) {
	ctx := context.Background()
	f := newServiceFixture()
	companyID := uuid.New()
	managerID := uuid.New()
	userID := uuid.New()
	f.users[userID] = models.CurrentUser{ID: userID}
	f.companyMembers[companyKey(companyID, managerID)] = models.CompanyMember{CompanyUUID: companyID, UserUUID: managerID, Role: models.CompanyMemberRoleManager, Status: models.MembershipStatusActive}

	invitation, err := f.service.CreateCompanyInvitation(ctx, models.CreateCompanyInvitationInput{
		CompanyUUID: companyID,
		RequestUser: managerID,
		UserUUID:    userID,
		Role:        models.CompanyMemberRoleEmployee,
	})

	require.NoError(t, err)
	require.Equal(t, userID, invitation.InvitedUserUUID)
	require.Equal(t, models.InvitationStatusPending, invitation.Status)
}

func TestCreateCompanyInvitationRejectsEmployee(t *testing.T) {
	ctx := context.Background()
	f := newServiceFixture()
	companyID := uuid.New()
	employeeID := uuid.New()
	userID := uuid.New()
	f.users[userID] = models.CurrentUser{ID: userID}
	f.companyMembers[companyKey(companyID, employeeID)] = models.CompanyMember{CompanyUUID: companyID, UserUUID: employeeID, Role: models.CompanyMemberRoleEmployee, Status: models.MembershipStatusActive}

	_, err := f.service.CreateCompanyInvitation(ctx, models.CreateCompanyInvitationInput{
		CompanyUUID: companyID,
		RequestUser: employeeID,
		UserUUID:    userID,
		Role:        models.CompanyMemberRoleEmployee,
	})

	require.ErrorIs(t, err, models.ErrForbidden)
}

func TestDepartmentLeaderCanInviteOnlyEmployeeToOwnDepartment(t *testing.T) {
	ctx := context.Background()
	f := newServiceFixture()
	companyID := uuid.New()
	departmentID := uuid.New()
	leaderID := uuid.New()
	userID := uuid.New()
	f.users[userID] = models.CurrentUser{ID: userID}
	f.companyMembers[companyKey(companyID, leaderID)] = models.CompanyMember{CompanyUUID: companyID, UserUUID: leaderID, Role: models.CompanyMemberRoleEmployee, Status: models.MembershipStatusActive}
	f.companyMembers[companyKey(companyID, userID)] = models.CompanyMember{CompanyUUID: companyID, UserUUID: userID, Role: models.CompanyMemberRoleEmployee, Status: models.MembershipStatusActive}
	f.departmentMembers[departmentKey(companyID, departmentID, leaderID)] = models.DepartmentMember{DepartmentUUID: departmentID, UserUUID: leaderID, Role: models.DepartmentMemberRoleLeader, Status: models.MembershipStatusActive}

	_, err := f.service.CreateDepartmentInvitation(ctx, models.CreateDepartmentInvitationInput{
		CompanyUUID:    companyID,
		DepartmentUUID: departmentID,
		RequestUser:    leaderID,
		UserUUID:       userID,
		Role:           models.DepartmentMemberRoleEmployee,
	})
	require.NoError(t, err)

	_, err = f.service.CreateDepartmentInvitation(ctx, models.CreateDepartmentInvitationInput{
		CompanyUUID:    companyID,
		DepartmentUUID: departmentID,
		RequestUser:    leaderID,
		UserUUID:       userID,
		Role:           models.DepartmentMemberRoleLeader,
	})
	require.ErrorIs(t, err, models.ErrForbidden)
}

func TestAcceptInvitationRejectsDifferentUserAndExpired(t *testing.T) {
	ctx := context.Background()
	f := newServiceFixture()
	companyID := uuid.New()
	userID := uuid.New()
	otherID := uuid.New()
	invitationID := uuid.New()
	now := time.Now().UTC()
	f.invitations[invitationID] = models.MembershipInvitation{
		ID:                invitationID,
		CompanyUUID:       companyID,
		InvitedUserUUID:   userID,
		InvitedByUserUUID: uuid.New(),
		CompanyRole:       models.CompanyMemberRoleEmployee,
		Status:            models.InvitationStatusPending,
		ExpiresAt:         now.Add(time.Hour),
	}

	_, err := f.service.AcceptInvitation(ctx, models.AcceptInvitationInput{InvitationUUID: invitationID, RequestUser: otherID})
	require.ErrorIs(t, err, models.ErrForbidden)

	expiredID := uuid.New()
	f.invitations[expiredID] = models.MembershipInvitation{
		ID:              expiredID,
		CompanyUUID:     companyID,
		InvitedUserUUID: userID,
		CompanyRole:     models.CompanyMemberRoleEmployee,
		Status:          models.InvitationStatusPending,
		ExpiresAt:       now.Add(-time.Hour),
	}
	_, err = f.service.AcceptInvitation(ctx, models.AcceptInvitationInput{InvitationUUID: expiredID, RequestUser: userID})
	require.ErrorIs(t, err, models.ErrInvitationExpired)
}

func TestAcceptInvitationRejectsNonPending(t *testing.T) {
	ctx := context.Background()
	f := newServiceFixture()
	invitationID := uuid.New()
	userID := uuid.New()
	f.invitations[invitationID] = models.MembershipInvitation{
		ID:              invitationID,
		CompanyUUID:     uuid.New(),
		InvitedUserUUID: userID,
		CompanyRole:     models.CompanyMemberRoleEmployee,
		Status:          models.InvitationStatusAccepted,
		ExpiresAt:       time.Now().UTC().Add(time.Hour),
	}

	_, err := f.service.AcceptInvitation(ctx, models.AcceptInvitationInput{InvitationUUID: invitationID, RequestUser: userID})

	require.ErrorIs(t, err, models.ErrInvitationNotPending)
}

type serviceFixture struct {
	service           *Service
	users             map[uuid.UUID]models.CurrentUser
	companyMembers    map[string]models.CompanyMember
	departmentMembers map[string]models.DepartmentMember
	invitations       map[uuid.UUID]models.MembershipInvitation
}

func newServiceFixture() *serviceFixture {
	f := &serviceFixture{
		users:             map[uuid.UUID]models.CurrentUser{},
		companyMembers:    map[string]models.CompanyMember{},
		departmentMembers: map[string]models.DepartmentMember{},
		invitations:       map[uuid.UUID]models.MembershipInvitation{},
	}
	f.service = NewService(f, f, f, f, logger.NewNop())
	now := time.Now().UTC()
	f.service.SetNow(func() time.Time { return now })
	return f
}

func companyKey(companyID uuid.UUID, userID uuid.UUID) string {
	return companyID.String() + ":" + userID.String()
}

func departmentKey(companyID uuid.UUID, departmentID uuid.UUID, userID uuid.UUID) string {
	return companyID.String() + ":" + departmentID.String() + ":" + userID.String()
}

func (f *serviceFixture) GetUserByUUID(ctx context.Context, id uuid.UUID) (models.CurrentUser, error) {
	user, ok := f.users[id]
	if !ok {
		return models.CurrentUser{}, models.ErrUserNotFound
	}
	return user, nil
}

func (f *serviceFixture) GetUserByEmail(ctx context.Context, email string) (models.CurrentUser, error) {
	return models.CurrentUser{}, models.ErrUserNotFound
}

func (f *serviceFixture) GetUserByUsername(ctx context.Context, username string) (models.CurrentUser, error) {
	for _, user := range f.users {
		if user.Username == username {
			return user, nil
		}
	}
	return models.CurrentUser{}, models.ErrUserNotFound
}

func (f *serviceFixture) CreateUser(ctx context.Context, user models.CurrentUser) (models.CurrentUser, error) {
	f.users[user.ID] = user
	return user, nil
}

func (f *serviceFixture) UpdateUsername(ctx context.Context, input models.UpdateUsernameInput) (models.CurrentUser, error) {
	user, ok := f.users[input.UserUUID]
	if !ok {
		return models.CurrentUser{}, models.ErrUserNotFound
	}
	user.Username = input.Username
	f.users[input.UserUUID] = user
	return user, nil
}

func (f *serviceFixture) UpdatePasswordHash(ctx context.Context, userID uuid.UUID, passwordHash string) (models.CurrentUser, error) {
	user, ok := f.users[userID]
	if !ok {
		return models.CurrentUser{}, models.ErrUserNotFound
	}
	user.PasswordHash = passwordHash
	f.users[userID] = user
	return user, nil
}

func (f *serviceFixture) UpdateProfile(ctx context.Context, input models.UpdateUserProfileInput) (models.CurrentUser, error) {
	user, ok := f.users[input.UserUUID]
	if !ok {
		return models.CurrentUser{}, models.ErrUserNotFound
	}
	if input.FullName != nil {
		user.FullName = *input.FullName
	}
	if input.FullSurname != nil {
		user.FullSurname = *input.FullSurname
	}
	user.Post = input.Post
	user.Phone = input.Phone
	user.Timezone = input.Timezone
	f.users[input.UserUUID] = user
	return user, nil
}

func (f *serviceFixture) UpdateAvatar(ctx context.Context, input models.UserAvatarUpdate) (models.CurrentUser, error) {
	user, ok := f.users[input.UserUUID]
	if !ok {
		return models.CurrentUser{}, models.ErrUserNotFound
	}
	user.AvatarPath = input.Path
	user.AvatarMime = input.MimeType
	user.AvatarSize = input.SizeBytes
	user.AvatarUpdatedAt = input.UpdatedAt
	f.users[input.UserUUID] = user
	return user, nil
}

func (f *serviceFixture) DeleteAvatar(ctx context.Context, userID uuid.UUID) (models.CurrentUser, error) {
	user, ok := f.users[userID]
	if !ok {
		return models.CurrentUser{}, models.ErrUserNotFound
	}
	user.AvatarPath = nil
	user.AvatarMime = nil
	user.AvatarSize = nil
	user.AvatarUpdatedAt = nil
	f.users[userID] = user
	return user, nil
}

func (f *serviceFixture) CreateInvitation(ctx context.Context, invitation models.MembershipInvitation) (models.MembershipInvitation, error) {
	f.invitations[invitation.ID] = invitation
	return invitation, nil
}

func (f *serviceFixture) GetInvitationByUUID(ctx context.Context, id uuid.UUID) (models.MembershipInvitation, error) {
	invitation, ok := f.invitations[id]
	if !ok {
		return models.MembershipInvitation{}, models.ErrInvitationNotFound
	}
	return invitation, nil
}

func (f *serviceFixture) ListUserInvitations(ctx context.Context, input models.ListUserInvitationsInput) ([]models.MembershipInvitation, error) {
	return nil, nil
}

func (f *serviceFixture) ListCompanyInvitations(ctx context.Context, companyID uuid.UUID, status models.InvitationStatus) ([]models.MembershipInvitation, error) {
	return nil, nil
}

func (f *serviceFixture) AcceptInvitation(ctx context.Context, id uuid.UUID, now time.Time) (models.MembershipInvitation, error) {
	invitation := f.invitations[id]
	if !invitation.ExpiresAt.After(now) {
		invitation.Status = models.InvitationStatusExpired
		f.invitations[id] = invitation
		return models.MembershipInvitation{}, models.ErrInvitationExpired
	}
	invitation.Status = models.InvitationStatusAccepted
	f.invitations[id] = invitation
	return invitation, nil
}

func (f *serviceFixture) DeclineInvitation(ctx context.Context, id uuid.UUID, now time.Time) (models.MembershipInvitation, error) {
	invitation := f.invitations[id]
	invitation.Status = models.InvitationStatusDeclined
	f.invitations[id] = invitation
	return invitation, nil
}

func (f *serviceFixture) CancelInvitation(ctx context.Context, id uuid.UUID, now time.Time) (models.MembershipInvitation, error) {
	invitation := f.invitations[id]
	invitation.Status = models.InvitationStatusCanceled
	f.invitations[id] = invitation
	return invitation, nil
}

func (f *serviceFixture) CreateCompany(ctx context.Context, company models.Company, member models.CompanyMember) (models.Company, error) {
	return company, nil
}

func (f *serviceFixture) UpdateCompany(ctx context.Context, companyID uuid.UUID, name string) (models.Company, error) {
	return models.Company{ID: companyID, Name: name}, nil
}

func (f *serviceFixture) UpdateCompanyTag(ctx context.Context, companyID uuid.UUID, tag string) (models.Company, error) {
	return models.Company{ID: companyID, Tag: tag}, nil
}

func (f *serviceFixture) ArchiveCompany(ctx context.Context, companyID uuid.UUID) error {
	return nil
}

func (f *serviceFixture) AddCompanyMember(ctx context.Context, member models.CompanyMember) (models.CompanyMember, error) {
	f.companyMembers[companyKey(member.CompanyUUID, member.UserUUID)] = member
	return member, nil
}

func (f *serviceFixture) UpdateCompanyMemberRole(ctx context.Context, companyID uuid.UUID, userID uuid.UUID, role models.CompanyMemberRole) (models.CompanyMember, error) {
	return models.CompanyMember{}, nil
}

func (f *serviceFixture) UpdateCompanyMemberStatus(ctx context.Context, companyID uuid.UUID, userID uuid.UUID, status models.MembershipStatus) (models.CompanyMember, error) {
	return models.CompanyMember{}, nil
}

func (f *serviceFixture) CountActiveCompanyManagers(ctx context.Context, companyID uuid.UUID, exceptUserID uuid.UUID) (int, error) {
	count := 0
	for _, member := range f.companyMembers {
		if member.CompanyUUID == companyID && member.UserUUID != exceptUserID && member.Role == models.CompanyMemberRoleManager && member.Status == models.MembershipStatusActive {
			count++
		}
	}
	return count, nil
}

func (f *serviceFixture) ListUserCompanies(ctx context.Context, userID uuid.UUID) ([]models.Company, error) {
	return nil, nil
}

func (f *serviceFixture) GetCompanyByUUID(ctx context.Context, companyID uuid.UUID, userID uuid.UUID) (models.Company, error) {
	return models.Company{}, nil
}

func (f *serviceFixture) GetManagedCompanyByUserUUID(ctx context.Context, userID uuid.UUID) (models.Company, error) {
	return models.Company{}, nil
}

func (f *serviceFixture) GetCompanyMember(ctx context.Context, companyID uuid.UUID, userID uuid.UUID) (models.CompanyMember, error) {
	member, ok := f.companyMembers[companyKey(companyID, userID)]
	if !ok || member.Status != models.MembershipStatusActive {
		return models.CompanyMember{}, models.ErrCompanyNotFound
	}
	return member, nil
}

func (f *serviceFixture) GetCompanyMembersOverview(ctx context.Context, companyID uuid.UUID) (models.CompanyMembersOverview, error) {
	return models.CompanyMembersOverview{}, nil
}

func (f *serviceFixture) ListCompanyMembers(ctx context.Context, input models.ListCompanyMembersInput) (models.CompanyMembersResult, error) {
	return models.CompanyMembersResult{}, nil
}

func (f *serviceFixture) CreateDepartment(ctx context.Context, department models.Department) (models.Department, error) {
	return department, nil
}

func (f *serviceFixture) UpdateDepartment(ctx context.Context, companyID uuid.UUID, departmentID uuid.UUID, name string) (models.Department, error) {
	return models.Department{ID: departmentID, CompanyUUID: companyID, Name: name}, nil
}

func (f *serviceFixture) ArchiveDepartment(ctx context.Context, companyID uuid.UUID, departmentID uuid.UUID) error {
	return nil
}

func (f *serviceFixture) AddDepartmentMember(ctx context.Context, companyID uuid.UUID, member models.DepartmentMember) (models.DepartmentMember, error) {
	return member, nil
}

func (f *serviceFixture) ListDepartmentMembers(ctx context.Context, companyID uuid.UUID, departmentID uuid.UUID) ([]models.DepartmentMember, error) {
	return nil, nil
}

func (f *serviceFixture) UpdateDepartmentMemberRole(ctx context.Context, companyID uuid.UUID, departmentID uuid.UUID, userID uuid.UUID, role models.DepartmentMemberRole) (models.DepartmentMember, error) {
	return models.DepartmentMember{}, nil
}

func (f *serviceFixture) UpdateDepartmentMemberStatus(ctx context.Context, companyID uuid.UUID, departmentID uuid.UUID, userID uuid.UUID, status models.MembershipStatus) (models.DepartmentMember, error) {
	return models.DepartmentMember{}, nil
}

func (f *serviceFixture) ListVisibleCompanyDepartments(ctx context.Context, companyID uuid.UUID, userID uuid.UUID) ([]models.Department, error) {
	return nil, nil
}

func (f *serviceFixture) GetDepartmentMember(ctx context.Context, companyID uuid.UUID, departmentID uuid.UUID, userID uuid.UUID) (models.DepartmentMember, error) {
	member, ok := f.departmentMembers[departmentKey(companyID, departmentID, userID)]
	if !ok || member.Status != models.MembershipStatusActive {
		return models.DepartmentMember{}, models.ErrDepartmentNotFound
	}
	return member, nil
}
