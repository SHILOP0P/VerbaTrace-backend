package call_folder

import (
	"context"
	"testing"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestPersonalFolderCreateAssignAndDelete(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	callID := uuid.New()
	repo := newFolderRepoStub()
	callRepo := &callRepoStub{calls: map[uuid.UUID]models.Call{
		callID: {
			ID:                 callID,
			UploadedByUserUUID: uuid.NullUUID{UUID: userID, Valid: true},
			VisibilityScope:    models.CallVisibilityScopePersonal,
		},
	}}
	svc := NewService(repo, callRepo, &companyRepoStub{}, &departmentRepoStub{})

	folder, err := svc.Create(ctx, models.CreateCallFolderInput{UserID: userID, Scope: models.CallFolderScopePersonal, Name: " Price objections ", Color: strPtr("#3b82f6")})
	require.NoError(t, err)
	require.Equal(t, "Price objections", folder.Name)

	require.NoError(t, svc.AssignCall(ctx, models.AssignCallToFolderInput{UserID: userID, FolderUUID: folder.ID, CallUUID: callID}))
	require.NoError(t, svc.Delete(ctx, folder.ID, userID))
	require.ErrorIs(t, svc.AssignCall(ctx, models.AssignCallToFolderInput{UserID: userID, FolderUUID: folder.ID, CallUUID: callID}), models.ErrCallFolderNotFound)
}

func TestCompanyMemberCannotManageCompanyFolder(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	companyID := uuid.New()
	svc := NewService(newFolderRepoStub(), &callRepoStub{}, &companyRepoStub{members: map[uuid.UUID]models.CompanyMember{
		userID: {UserUUID: userID, CompanyUUID: companyID, Role: models.CompanyMemberRoleEmployee, Status: models.MembershipStatusActive},
	}}, &departmentRepoStub{})

	_, err := svc.Create(ctx, models.CreateCallFolderInput{
		UserID:      userID,
		Scope:       models.CallFolderScopeCompany,
		CompanyUUID: uuid.NullUUID{UUID: companyID, Valid: true},
		Name:        "Team",
	})
	require.ErrorIs(t, err, models.ErrForbidden)
}

func TestDepartmentLeaderCanManageOwnDepartmentOnly(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	companyID := uuid.New()
	departmentID := uuid.New()
	otherDepartmentID := uuid.New()
	svc := NewService(newFolderRepoStub(), &callRepoStub{}, &companyRepoStub{}, &departmentRepoStub{members: map[uuid.UUID]models.DepartmentMember{
		departmentID: {UserUUID: userID, DepartmentUUID: departmentID, Role: models.DepartmentMemberRoleLeader, Status: models.MembershipStatusActive},
	}})

	_, err := svc.Create(ctx, models.CreateCallFolderInput{
		UserID:         userID,
		Scope:          models.CallFolderScopeDepartment,
		CompanyUUID:    uuid.NullUUID{UUID: companyID, Valid: true},
		DepartmentUUID: uuid.NullUUID{UUID: departmentID, Valid: true},
		Name:           "Support",
	})
	require.NoError(t, err)

	_, err = svc.Create(ctx, models.CreateCallFolderInput{
		UserID:         userID,
		Scope:          models.CallFolderScopeDepartment,
		CompanyUUID:    uuid.NullUUID{UUID: companyID, Valid: true},
		DepartmentUUID: uuid.NullUUID{UUID: otherDepartmentID, Valid: true},
		Name:           "Sales",
	})
	require.ErrorIs(t, err, models.ErrForbidden)
}

func TestAssignMismatchedCallReturnsScopeMismatch(t *testing.T) {
	ctx := context.Background()
	managerID := uuid.New()
	companyID := uuid.New()
	otherCompanyID := uuid.New()
	callID := uuid.New()
	repo := newFolderRepoStub()
	svc := NewService(repo, &callRepoStub{calls: map[uuid.UUID]models.Call{
		callID: {
			ID:              callID,
			CompanyUUID:     uuid.NullUUID{UUID: otherCompanyID, Valid: true},
			VisibilityScope: models.CallVisibilityScopeCompany,
		},
	}}, &companyRepoStub{members: map[uuid.UUID]models.CompanyMember{
		managerID: {UserUUID: managerID, CompanyUUID: companyID, Role: models.CompanyMemberRoleManager, Status: models.MembershipStatusActive},
	}}, &departmentRepoStub{})

	folder, err := svc.Create(ctx, models.CreateCallFolderInput{
		UserID:      managerID,
		Scope:       models.CallFolderScopeCompany,
		CompanyUUID: uuid.NullUUID{UUID: companyID, Valid: true},
		Name:        "Company",
	})
	require.NoError(t, err)

	err = svc.AssignCall(ctx, models.AssignCallToFolderInput{UserID: managerID, FolderUUID: folder.ID, CallUUID: callID})
	require.ErrorIs(t, err, models.ErrCallFolderScopeMismatch)
}

func TestPersonalFolderRejectsCompanyCall(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	callID := uuid.New()
	repo := newFolderRepoStub()
	svc := NewService(repo, &callRepoStub{calls: map[uuid.UUID]models.Call{
		callID: {ID: callID, CompanyUUID: uuid.NullUUID{UUID: uuid.New(), Valid: true}, VisibilityScope: models.CallVisibilityScopeCompany},
	}}, &companyRepoStub{}, &departmentRepoStub{})

	folder, err := svc.Create(ctx, models.CreateCallFolderInput{UserID: userID, Scope: models.CallFolderScopePersonal, Name: "Private"})
	require.NoError(t, err)
	err = svc.AssignCall(ctx, models.AssignCallToFolderInput{UserID: userID, FolderUUID: folder.ID, CallUUID: callID})
	require.ErrorIs(t, err, models.ErrCallFolderScopeMismatch)
}

func TestOtherUserCannotManagePersonalFolder(t *testing.T) {
	ctx := context.Background()
	ownerID := uuid.New()
	repo := newFolderRepoStub()
	svc := NewService(repo, &callRepoStub{}, &companyRepoStub{}, &departmentRepoStub{})
	folder, err := svc.Create(ctx, models.CreateCallFolderInput{UserID: ownerID, Scope: models.CallFolderScopePersonal, Name: "Private"})
	require.NoError(t, err)
	require.ErrorIs(t, svc.Delete(ctx, folder.ID, uuid.New()), models.ErrForbidden)
}

func TestManagerCanGrantEmployeeDepartmentAccessButNotLeader(t *testing.T) {
	ctx := context.Background()
	managerID := uuid.New()
	companyID := uuid.New()
	departmentID := uuid.New()
	folderID := uuid.New()
	employeeID := uuid.New()
	leaderID := uuid.New()
	repo := newFolderRepoStub()
	repo.folders[folderID] = models.CallFolder{ID: folderID, Scope: models.CallFolderScopeDepartment, CompanyUUID: uuid.NullUUID{UUID: companyID, Valid: true}, DepartmentUUID: uuid.NullUUID{UUID: departmentID, Valid: true}}
	companyRepo := &companyRepoStub{members: map[uuid.UUID]models.CompanyMember{
		managerID:  {UserUUID: managerID, CompanyUUID: companyID, Role: models.CompanyMemberRoleManager, Status: models.MembershipStatusActive},
		employeeID: {UserUUID: employeeID, CompanyUUID: companyID, Role: models.CompanyMemberRoleEmployee, Status: models.MembershipStatusActive},
		leaderID:   {UserUUID: leaderID, CompanyUUID: companyID, Role: models.CompanyMemberRoleEmployee, Status: models.MembershipStatusActive},
	}}
	employeeRepo := &departmentRepoStub{members: map[uuid.UUID]models.DepartmentMember{
		departmentID: {UserUUID: employeeID, DepartmentUUID: departmentID, Role: models.DepartmentMemberRoleEmployee, Status: models.MembershipStatusActive},
	}}
	svc := NewService(repo, &callRepoStub{}, companyRepo, employeeRepo)
	_, err := svc.GrantAccess(ctx, models.GrantCallFolderAccessInput{UserID: managerID, FolderUUID: folderID, TargetUserUUID: employeeID})
	require.NoError(t, err)

	leaderRepo := &departmentRepoStub{members: map[uuid.UUID]models.DepartmentMember{
		departmentID: {UserUUID: leaderID, DepartmentUUID: departmentID, Role: models.DepartmentMemberRoleLeader, Status: models.MembershipStatusActive},
	}}
	svc = NewService(repo, &callRepoStub{}, companyRepo, leaderRepo)
	_, err = svc.GrantAccess(ctx, models.GrantCallFolderAccessInput{UserID: managerID, FolderUUID: folderID, TargetUserUUID: leaderID})
	require.ErrorIs(t, err, models.ErrForbidden)
}

type folderRepoStub struct {
	folders  map[uuid.UUID]models.CallFolder
	deleted  map[uuid.UUID]bool
	accesses map[uuid.UUID]map[uuid.UUID]models.CallFolderAccess
}

func newFolderRepoStub() *folderRepoStub {
	return &folderRepoStub{folders: map[uuid.UUID]models.CallFolder{}, deleted: map[uuid.UUID]bool{}, accesses: map[uuid.UUID]map[uuid.UUID]models.CallFolderAccess{}}
}

func (r *folderRepoStub) Create(_ context.Context, folder models.CallFolder) (models.CallFolder, error) {
	r.folders[folder.ID] = folder
	return folder, nil
}

func (r *folderRepoStub) GetByUUID(_ context.Context, id uuid.UUID) (models.CallFolder, error) {
	if r.deleted[id] {
		return models.CallFolder{}, models.ErrCallFolderNotFound
	}
	folder, ok := r.folders[id]
	if !ok {
		return models.CallFolder{}, models.ErrCallFolderNotFound
	}
	return folder, nil
}

func (r *folderRepoStub) GetVisibleByUUID(ctx context.Context, id uuid.UUID, _ uuid.UUID) (models.CallFolder, error) {
	return r.GetByUUID(ctx, id)
}

func (r *folderRepoStub) List(_ context.Context, input models.ListCallFoldersInput) (models.ListCallFoldersResult, error) {
	return models.ListCallFoldersResult{Limit: input.Limit, Offset: input.Offset}, nil
}

func (r *folderRepoStub) Update(_ context.Context, input models.UpdateCallFolderInput) (models.CallFolder, error) {
	folder := r.folders[input.FolderUUID]
	if input.Name != nil {
		folder.Name = *input.Name
	}
	r.folders[input.FolderUUID] = folder
	return folder, nil
}

func (r *folderRepoStub) SoftDelete(_ context.Context, id uuid.UUID) error {
	if _, ok := r.folders[id]; !ok {
		return models.ErrCallFolderNotFound
	}
	r.deleted[id] = true
	return nil
}

func (r *folderRepoStub) AssignCall(context.Context, models.AssignCallToFolderInput) error {
	return nil
}
func (r *folderRepoStub) RemoveCall(context.Context, models.RemoveCallFromFolderInput) error {
	return nil
}
func (r *folderRepoStub) ListFolderCalls(_ context.Context, input models.ListFolderCallsInput) (models.ListCallsResult, error) {
	return models.ListCallsResult{Limit: input.Limit, Offset: input.Offset}, nil
}
func (r *folderRepoStub) GrantAccess(_ context.Context, input models.GrantCallFolderAccessInput) (models.CallFolderAccess, error) {
	if r.accesses[input.FolderUUID] == nil {
		r.accesses[input.FolderUUID] = map[uuid.UUID]models.CallFolderAccess{}
	}
	access := models.CallFolderAccess{FolderUUID: input.FolderUUID, UserUUID: input.TargetUserUUID, GrantedByUserUUID: input.UserID}
	r.accesses[input.FolderUUID][input.TargetUserUUID] = access
	return access, nil
}
func (r *folderRepoStub) RevokeAccess(_ context.Context, folderID uuid.UUID, userID uuid.UUID) error {
	if _, ok := r.accesses[folderID][userID]; !ok {
		return models.ErrCallFolderNotFound
	}
	delete(r.accesses[folderID], userID)
	return nil
}
func (r *folderRepoStub) ListAccesses(_ context.Context, folderID uuid.UUID) ([]models.CallFolderAccess, error) {
	items := []models.CallFolderAccess{}
	for _, item := range r.accesses[folderID] {
		items = append(items, item)
	}
	return items, nil
}

type callRepoStub struct {
	calls map[uuid.UUID]models.Call
}

func (r *callRepoStub) CreateCall(context.Context, models.Call) (models.Call, error) {
	return models.Call{}, nil
}
func (r *callRepoStub) CreateCallWithProcessingJob(context.Context, models.Call, models.ProcessingJob) (models.Call, error) {
	return models.Call{}, nil
}
func (r *callRepoStub) List(context.Context, uuid.UUID) ([]models.Call, error) { return nil, nil }
func (r *callRepoStub) ListFiltered(_ context.Context, input models.ListCallsInput) (models.ListCallsResult, error) {
	return models.ListCallsResult{Limit: input.Limit, Offset: input.Offset}, nil
}
func (r *callRepoStub) GetFilterOptions(context.Context, models.CallFilterOptionsInput) (models.CallFilterOptions, error) {
	return models.CallFilterOptions{}, nil
}
func (r *callRepoStub) GetByUUID(_ context.Context, id uuid.UUID, _ uuid.UUID) (models.Call, error) {
	call, ok := r.calls[id]
	if !ok {
		return models.Call{}, models.ErrCallNotFound
	}
	return call, nil
}
func (r *callRepoStub) GetByUUIDForProcessing(context.Context, uuid.UUID) (models.Call, error) {
	return models.Call{}, nil
}
func (r *callRepoStub) UpdateCallTitle(context.Context, uuid.UUID, uuid.UUID, string) (models.Call, error) {
	return models.Call{}, nil
}
func (r *callRepoStub) UpdateCallStatus(context.Context, uuid.UUID, models.CallStatus) (models.Call, error) {
	return models.Call{}, nil
}
func (r *callRepoStub) DeleteCall(context.Context, uuid.UUID, uuid.UUID) error { return nil }
func (r *callRepoStub) TakeNextForProcessing(context.Context) (models.Call, error) {
	return models.Call{}, nil
}

type companyRepoStub struct {
	members map[uuid.UUID]models.CompanyMember
}

func (r *companyRepoStub) GetCompanyMember(_ context.Context, _ uuid.UUID, userID uuid.UUID) (models.CompanyMember, error) {
	member, ok := r.members[userID]
	if !ok {
		return models.CompanyMember{}, models.ErrCompanyNotFound
	}
	return member, nil
}

type departmentRepoStub struct {
	members map[uuid.UUID]models.DepartmentMember
}

func (r *departmentRepoStub) GetDepartmentMember(_ context.Context, _ uuid.UUID, departmentID uuid.UUID, _ uuid.UUID) (models.DepartmentMember, error) {
	member, ok := r.members[departmentID]
	if !ok {
		return models.DepartmentMember{}, models.ErrDepartmentNotFound
	}
	return member, nil
}

func (r *departmentRepoStub) ListVisibleCompanyDepartments(context.Context, uuid.UUID, uuid.UUID) ([]models.Department, error) {
	return nil, nil
}

func strPtr(value string) *string {
	return &value
}
