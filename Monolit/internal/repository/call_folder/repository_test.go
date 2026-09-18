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

// An employee has no business seeing a department folder until one of their own
// calls is in it: a folder must never widen call visibility.
func (s *RepositorySuite) TestDepartmentEmployeeSeesFolderOnlyWithOwnCall() {
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

	_, err = s.repository.GetVisibleByUUID(s.ctx, folder.ID, employee.ID)
	s.Require().ErrorIs(err, models.ErrCallFolderNotFound)

	// The manager sees it by role.
	visible, err := s.repository.GetVisibleByUUID(s.ctx, folder.ID, manager.ID)
	s.Require().NoError(err)
	s.Require().Equal(folder.ID, visible.ID)

	callID := uuid.New()
	_, err = s.db.ExecContext(s.ctx, `
		INSERT INTO calls (call_uuid, title, status, audio_path, original_filename, mime_type, size_bytes, duration_seconds, uploaded_by_user_uuid, company_uuid, department_uuid, visibility_scope, created_at)
		VALUES ($1, 'Call', 'new', 'uploads/a.wav', 'a.wav', 'audio/wav', 10, 5, $2, $3, $4, 'department', now())`,
		callID, employee.ID, companyID, departmentID)
	s.Require().NoError(err)
	_, err = s.db.ExecContext(s.ctx, `
		INSERT INTO call_folder_assignments (folder_uuid, call_uuid, assigned_by_user_uuid)
		VALUES ($1, $2, $3)`, folder.ID, callID, manager.ID)
	s.Require().NoError(err)

	visible, err = s.repository.GetVisibleByUUID(s.ctx, folder.ID, employee.ID)
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

// A folder listing must not leak calls the viewer cannot open.
func (s *RepositorySuite) TestListFolderCallsHidesForeignAndDeletedCalls() {
	manager := s.createUser(uuid.NewString() + "@example.com")
	employee := s.createUser(uuid.NewString() + "@example.com")
	companyID := uuid.New()

	_, err := s.db.ExecContext(s.ctx, `
		INSERT INTO companies (company_uuid, name, tag, manager_user_uuid, member_limit)
		VALUES ($1, 'Folder calls company', $2, $3, 10)`, companyID, "@company_"+companyID.String()[:8], manager.ID)
	s.Require().NoError(err)
	_, err = s.db.ExecContext(s.ctx, `
		INSERT INTO company_members (company_uuid, user_uuid, role, status)
		VALUES ($1, $2, 'company_manager', 'active'), ($1, $3, 'employee', 'active')`, companyID, manager.ID, employee.ID)
	s.Require().NoError(err)

	folder, err := s.repository.Create(s.ctx, models.CallFolder{
		ID: uuid.New(), Scope: models.CallFolderScopeCompany,
		CompanyUUID: uuid.NullUUID{UUID: companyID, Valid: true},
		Name:        "Company folder", CreatedByUserUUID: manager.ID,
	})
	s.Require().NoError(err)

	own, foreign, binned := uuid.New(), uuid.New(), uuid.New()
	for _, item := range []struct {
		id       uuid.UUID
		uploader uuid.UUID
		deleted  bool
	}{{own, employee.ID, false}, {foreign, manager.ID, false}, {binned, employee.ID, true}} {
		_, err = s.db.ExecContext(s.ctx, `
			INSERT INTO calls (call_uuid, title, status, audio_path, original_filename, mime_type, size_bytes, duration_seconds, uploaded_by_user_uuid, company_uuid, visibility_scope, created_at, deleted_at, purge_after)
			VALUES ($1, 'Call', 'new', 'uploads/a.wav', 'a.wav', 'audio/wav', 10, 5, $2, $3, 'company', now(),
			        CASE WHEN $4 THEN now() ELSE NULL END, CASE WHEN $4 THEN now() + interval '30 days' ELSE NULL END)`,
			item.id, item.uploader, companyID, item.deleted)
		s.Require().NoError(err)
		_, err = s.db.ExecContext(s.ctx, `
			INSERT INTO call_folder_assignments (folder_uuid, call_uuid, assigned_by_user_uuid)
			VALUES ($1, $2, $3)`, folder.ID, item.id, manager.ID)
		s.Require().NoError(err)
	}

	employeeCalls, err := s.repository.ListFolderCalls(s.ctx, models.ListFolderCallsInput{UserID: employee.ID, FolderUUID: folder.ID, Limit: 50})
	s.Require().NoError(err)
	s.Require().Len(employeeCalls.Items, 1)
	s.Require().Equal(own, employeeCalls.Items[0].ID)

	managerCalls, err := s.repository.ListFolderCalls(s.ctx, models.ListFolderCallsInput{UserID: manager.ID, FolderUUID: folder.ID, Limit: 50})
	s.Require().NoError(err)
	s.Require().Len(managerCalls.Items, 2)
}

// A call that came through a sandbox application is a test call in a folder
// too: the list used to leave the flag out, and the call showed a processing
// status instead of «Тестовый».
func (s *RepositorySuite) TestListFolderCallsMarksSandboxCalls() {
	manager := s.createUser(uuid.NewString() + "@example.com")
	companyID, billingID, applicationID, connectionID, eventID, itemID := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	exec := func(query string, args ...any) {
		_, err := s.db.ExecContext(s.ctx, query, args...)
		s.Require().NoError(err)
	}
	exec(`INSERT INTO companies (company_uuid, name, tag, manager_user_uuid, member_limit) VALUES ($1, 'Sandbox company', $2, $3, 10)`, companyID, "@sandbox_"+companyID.String()[:8], manager.ID)
	exec(`INSERT INTO company_members (company_uuid, user_uuid, role, status) VALUES ($1, $2, 'company_manager', 'active')`, companyID, manager.ID)
	exec(`INSERT INTO billing_accounts (billing_account_uuid, owner_type, company_uuid) VALUES ($1, 'company', $2)`, billingID, companyID)
	exec(`INSERT INTO developer_applications (application_uuid, owner_type, company_uuid, billing_account_uuid, name, environment, status) VALUES ($1, 'company', $2, $3, 'Sandbox app', 'sandbox', 'active')`, applicationID, companyID, billingID)
	exec(`INSERT INTO integration_connections (connection_uuid, application_uuid, company_uuid, created_by_user_uuid, name, provider, status) VALUES ($1, $2, $3, $4, 'API', 'generic_api', 'active')`, connectionID, applicationID, companyID, manager.ID)
	exec(`INSERT INTO ingest_events (event_uuid, connection_uuid, external_event_id, event_type, schema_version, payload_sha256, accepted) VALUES ($1, $2, 'e1', 'call.completed', 1, '\x00'::bytea, true)`, eventID, connectionID)
	exec(`INSERT INTO ingest_items (ingest_item_uuid, connection_uuid, event_uuid, external_call_id, idempotency_key, request_sha256, source_kind, title, status, stage, application_uuid, billing_account_uuid, destination_scope, destination_company_uuid, source_ref)
		VALUES ($1, $2, $3, 'c1', 'k1', '\x00'::bytea, 'upload', 'Test', 'completed', 'completed', $4, $5, 'company', $6, 'r1')`, itemID, connectionID, eventID, applicationID, billingID, companyID)

	folder, err := s.repository.Create(s.ctx, models.CallFolder{
		ID: uuid.New(), Scope: models.CallFolderScopeCompany,
		CompanyUUID: uuid.NullUUID{UUID: companyID, Valid: true},
		Name:        "Тестовые звонки", CreatedByUserUUID: manager.ID,
	})
	s.Require().NoError(err)
	sandboxCall, uploadedCall := uuid.New(), uuid.New()
	for _, item := range []struct {
		id     uuid.UUID
		ingest any
	}{{sandboxCall, itemID}, {uploadedCall, nil}} {
		exec(`INSERT INTO calls (call_uuid, title, status, audio_path, original_filename, mime_type, size_bytes, duration_seconds, uploaded_by_user_uuid, company_uuid, visibility_scope, ingest_item_uuid, created_at)
			VALUES ($1, 'Call', 'new', 'uploads/a.wav', 'a.wav', 'audio/wav', 10, 5, $2, $3, 'company', $4, now())`, item.id, manager.ID, companyID, item.ingest)
		exec(`INSERT INTO call_folder_assignments (folder_uuid, call_uuid, assigned_by_user_uuid) VALUES ($1, $2, $3)`, folder.ID, item.id, manager.ID)
	}

	listed, err := s.repository.ListFolderCalls(s.ctx, models.ListFolderCallsInput{UserID: manager.ID, FolderUUID: folder.ID, Limit: 50})
	s.Require().NoError(err)
	flags := map[uuid.UUID]bool{}
	for _, item := range listed.Items {
		flags[item.ID] = item.IsTest
	}
	s.Require().Equal(map[uuid.UUID]bool{sandboxCall: true, uploadedCall: false}, flags)
}
