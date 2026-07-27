//go:build integration

package call

import (
	"time"

	"calllens/monolit/internal/models"

	"github.com/google/uuid"
)

func (s *RepositorySuite) createUser(email string) models.User {
	userID := uuid.New()
	user := models.User{
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

func (s *RepositorySuite) createCompanyWithManager() (models.Company, models.User) {
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

func (s *RepositorySuite) addCompanyEmployee(companyID uuid.UUID, role models.CompanyMemberRole) models.User {
	user := s.createUser(uuid.NewString() + "@example.com")
	_, err := s.companyRepository.AddCompanyMember(s.ctx, models.CompanyMember{
		CompanyUUID: companyID,
		UserUUID:    user.ID,
		Role:        role,
		Status:      models.MembershipStatusActive,
		CreatedAt:   time.Now().UTC().Truncate(time.Microsecond),
	})
	s.Require().NoError(err)

	return user
}

func (s *RepositorySuite) createDepartment(companyID uuid.UUID) models.Department {
	department, err := s.departmentRepository.CreateDepartment(s.ctx, models.Department{
		ID:          uuid.New(),
		CompanyUUID: companyID,
		Name:        "Sales",
		CreatedAt:   time.Now().UTC().Truncate(time.Microsecond),
	})
	s.Require().NoError(err)

	return department
}

func (s *RepositorySuite) addDepartmentMember(companyID uuid.UUID, departmentID uuid.UUID, userID uuid.UUID, role models.DepartmentMemberRole) {
	_, err := s.departmentRepository.AddDepartmentMember(s.ctx, companyID, models.DepartmentMember{
		DepartmentUUID: departmentID,
		UserUUID:       userID,
		Role:           role,
		Status:         models.MembershipStatusActive,
		CreatedAt:      time.Now().UTC().Truncate(time.Microsecond),
	})
	s.Require().NoError(err)
}

func testCall(uploaderID uuid.UUID) models.Call {
	return models.Call{
		ID:                 uuid.New(),
		Title:              "Test call",
		Status:             models.CallStatusNew,
		AudioPath:          "uploads/call.wav",
		OriginalFilename:   "call.wav",
		MimeType:           "audio/wav",
		SizeBytes:          10,
		DurationSeconds:    0,
		UploadedByUserUUID: uuid.NullUUID{UUID: uploaderID, Valid: true},
		VisibilityScope:    models.CallVisibilityScopePersonal,
		CreatedAt:          time.Now().UTC().Truncate(time.Microsecond),
	}
}

func (s *RepositorySuite) TestCreateCallAndGetForProcessing() {
	user := s.createUser(uuid.NewString() + "@example.com")
	input := testCall(user.ID)

	created, err := s.repository.CreateCall(s.ctx, input)
	s.Require().NoError(err)
	s.Require().Equal(input.ID, created.ID)
	s.Require().Equal(input.Title, created.Title)
	s.Require().Equal(models.CallVisibilityScopePersonal, created.VisibilityScope)

	processingCall, err := s.repository.GetByUUIDForProcessing(s.ctx, input.ID)
	s.Require().NoError(err)
	s.Require().Equal(created, processingCall)
}

func (s *RepositorySuite) TestCreateCallWithProcessingJob() {
	user := s.createUser(uuid.NewString() + "@example.com")
	input := testCall(user.ID)
	now := time.Now().UTC().Truncate(time.Microsecond)
	job := models.ProcessingJob{
		ID:          uuid.New(),
		Type:        models.ProcessingJobTypeTranscribeCall,
		EntityUUID:  input.ID,
		Status:      models.ProcessingJobStatusPending,
		MaxAttempts: models.DefaultProcessingJobMaxAttempts,
		AvailableAt: now,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	created, err := s.repository.CreateCallWithProcessingJob(s.ctx, input, job)
	s.Require().NoError(err)
	s.Require().Equal(input.ID, created.ID)

	var count int
	err = s.db.QueryRowContext(s.ctx,
		`SELECT COUNT(*) FROM processing_jobs WHERE job_uuid = $1 AND entity_uuid = $2`,
		job.ID, input.ID,
	).Scan(&count)
	s.Require().NoError(err)
	s.Require().Equal(1, count)
}

func (s *RepositorySuite) TestCreateCallWithProcessingJobRollsBackInvalidJob() {
	user := s.createUser(uuid.NewString() + "@example.com")
	input := testCall(user.ID)
	now := time.Now().UTC().Truncate(time.Microsecond)
	job := models.ProcessingJob{
		ID:          uuid.New(),
		Type:        "invalid",
		EntityUUID:  input.ID,
		Status:      models.ProcessingJobStatusPending,
		MaxAttempts: 3,
		AvailableAt: now,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	_, err := s.repository.CreateCallWithProcessingJob(s.ctx, input, job)
	s.Require().Error(err)

	_, err = s.repository.GetByUUIDForProcessing(s.ctx, input.ID)
	s.Require().ErrorIs(err, models.ErrCallNotFound)
}

func (s *RepositorySuite) TestTakeNextForProcessing() {
	user := s.createUser(uuid.NewString() + "@example.com")
	first := testCall(user.ID)
	second := testCall(user.ID)
	second.CreatedAt = first.CreatedAt.Add(time.Second)
	_, err := s.repository.CreateCall(s.ctx, first)
	s.Require().NoError(err)
	_, err = s.repository.CreateCall(s.ctx, second)
	s.Require().NoError(err)

	taken, err := s.repository.TakeNextForProcessing(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(first.ID, taken.ID)
	s.Require().Equal(models.CallStatusProcessing, taken.Status)

	taken, err = s.repository.TakeNextForProcessing(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(second.ID, taken.ID)

	_, err = s.repository.TakeNextForProcessing(s.ctx)
	s.Require().ErrorIs(err, models.ErrNoCallsForProcessing)
}

func (s *RepositorySuite) TestCreateCallRejectsInvalidPlacementConstraint() {
	user := s.createUser(uuid.NewString() + "@example.com")
	input := testCall(user.ID)
	input.VisibilityScope = models.CallVisibilityScopeCompany

	_, err := s.repository.CreateCall(s.ctx, input)

	s.Require().Error(err)
}

func (s *RepositorySuite) TestPersonalCallVisibility() {
	owner := s.createUser(uuid.NewString() + "@example.com")
	outsider := s.createUser(uuid.NewString() + "@example.com")
	call := testCall(owner.ID)
	_, err := s.repository.CreateCall(s.ctx, call)
	s.Require().NoError(err)

	got, err := s.repository.GetByUUID(s.ctx, call.ID, owner.ID)
	s.Require().NoError(err)
	s.Require().Equal(call.ID, got.ID)

	_, err = s.repository.GetByUUID(s.ctx, call.ID, outsider.ID)
	s.Require().ErrorIs(err, models.ErrCallNotFound)
}

func (s *RepositorySuite) TestCompanyCallVisibility() {
	company, manager := s.createCompanyWithManager()
	employee := s.addCompanyEmployee(company.ID, models.CompanyMemberRoleEmployee)
	uploader := s.createUser(uuid.NewString() + "@example.com")
	call := testCall(uploader.ID)
	call.VisibilityScope = models.CallVisibilityScopeCompany
	call.CompanyUUID = uuid.NullUUID{UUID: company.ID, Valid: true}
	_, err := s.repository.CreateCall(s.ctx, call)
	s.Require().NoError(err)

	_, err = s.repository.GetByUUID(s.ctx, call.ID, manager.ID)
	s.Require().NoError(err)

	_, err = s.repository.GetByUUID(s.ctx, call.ID, employee.ID)
	s.Require().ErrorIs(err, models.ErrCallNotFound)
}

func (s *RepositorySuite) TestDepartmentCallVisibility() {
	company, manager := s.createCompanyWithManager()
	department := s.createDepartment(company.ID)
	leader := s.addCompanyEmployee(company.ID, models.CompanyMemberRoleEmployee)
	employee := s.addCompanyEmployee(company.ID, models.CompanyMemberRoleEmployee)
	uploader := s.createUser(uuid.NewString() + "@example.com")
	s.addDepartmentMember(company.ID, department.ID, leader.ID, models.DepartmentMemberRoleLeader)
	s.addDepartmentMember(company.ID, department.ID, employee.ID, models.DepartmentMemberRoleEmployee)

	call := testCall(uploader.ID)
	call.VisibilityScope = models.CallVisibilityScopeDepartment
	call.CompanyUUID = uuid.NullUUID{UUID: company.ID, Valid: true}
	call.DepartmentUUID = uuid.NullUUID{UUID: department.ID, Valid: true}
	_, err := s.repository.CreateCall(s.ctx, call)
	s.Require().NoError(err)

	_, err = s.repository.GetByUUID(s.ctx, call.ID, manager.ID)
	s.Require().NoError(err)

	_, err = s.repository.GetByUUID(s.ctx, call.ID, leader.ID)
	s.Require().NoError(err)

	_, err = s.repository.GetByUUID(s.ctx, call.ID, employee.ID)
	s.Require().ErrorIs(err, models.ErrCallNotFound)
}

func (s *RepositorySuite) TestListReturnsOnlyVisibleCalls() {
	company, manager := s.createCompanyWithManager()
	department := s.createDepartment(company.ID)
	leader := s.addCompanyEmployee(company.ID, models.CompanyMemberRoleEmployee)
	employee := s.addCompanyEmployee(company.ID, models.CompanyMemberRoleEmployee)
	s.addDepartmentMember(company.ID, department.ID, leader.ID, models.DepartmentMemberRoleLeader)
	s.addDepartmentMember(company.ID, department.ID, employee.ID, models.DepartmentMemberRoleEmployee)

	personal := testCall(employee.ID)
	_, err := s.repository.CreateCall(s.ctx, personal)
	s.Require().NoError(err)

	companyUploader := s.createUser(uuid.NewString() + "@example.com")
	companyCall := testCall(companyUploader.ID)
	companyCall.VisibilityScope = models.CallVisibilityScopeCompany
	companyCall.CompanyUUID = uuid.NullUUID{UUID: company.ID, Valid: true}
	companyCall.CreatedAt = companyCall.CreatedAt.Add(time.Second)
	_, err = s.repository.CreateCall(s.ctx, companyCall)
	s.Require().NoError(err)

	departmentUploader := s.createUser(uuid.NewString() + "@example.com")
	departmentCall := testCall(departmentUploader.ID)
	departmentCall.VisibilityScope = models.CallVisibilityScopeDepartment
	departmentCall.CompanyUUID = uuid.NullUUID{UUID: company.ID, Valid: true}
	departmentCall.DepartmentUUID = uuid.NullUUID{UUID: department.ID, Valid: true}
	departmentCall.CreatedAt = departmentCall.CreatedAt.Add(2 * time.Second)
	_, err = s.repository.CreateCall(s.ctx, departmentCall)
	s.Require().NoError(err)

	managerCalls, err := s.repository.List(s.ctx, manager.ID)
	s.Require().NoError(err)
	s.Require().Len(managerCalls, 2)

	leaderCalls, err := s.repository.List(s.ctx, leader.ID)
	s.Require().NoError(err)
	s.Require().Len(leaderCalls, 1)
	s.Require().Equal(departmentCall.ID, leaderCalls[0].ID)

	employeeCalls, err := s.repository.List(s.ctx, employee.ID)
	s.Require().NoError(err)
	s.Require().Len(employeeCalls, 1)
	s.Require().Equal(personal.ID, employeeCalls[0].ID)
}

func (s *RepositorySuite) TestListFilteredAppliesFiltersPaginationAndVisibility() {
	company, manager := s.createCompanyWithManager()
	outsider := s.createUser(uuid.NewString() + "@example.com")
	uploader := s.createUser(uuid.NewString() + "@example.com")
	baseTime := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)

	first := testCall(uploader.ID)
	first.Title = "Alpha discovery"
	first.OriginalFilename = "first-alpha.wav"
	first.Status = models.CallStatusAnalyzed
	first.VisibilityScope = models.CallVisibilityScopeCompany
	first.CompanyUUID = uuid.NullUUID{UUID: company.ID, Valid: true}
	first.CreatedAt = baseTime
	_, err := s.repository.CreateCall(s.ctx, first)
	s.Require().NoError(err)

	second := testCall(uploader.ID)
	second.Title = "Second"
	second.OriginalFilename = "second-alpha.wav"
	second.Status = models.CallStatusAnalyzed
	second.VisibilityScope = models.CallVisibilityScopeCompany
	second.CompanyUUID = uuid.NullUUID{UUID: company.ID, Valid: true}
	second.CreatedAt = baseTime.Add(time.Hour)
	_, err = s.repository.CreateCall(s.ctx, second)
	s.Require().NoError(err)

	failed := testCall(uploader.ID)
	failed.Title = "Alpha failed"
	failed.Status = models.CallStatusFailed
	failed.VisibilityScope = models.CallVisibilityScopeCompany
	failed.CompanyUUID = uuid.NullUUID{UUID: company.ID, Valid: true}
	failed.CreatedAt = baseTime.Add(2 * time.Hour)
	_, err = s.repository.CreateCall(s.ctx, failed)
	s.Require().NoError(err)

	from := baseTime.Add(-time.Minute)
	to := baseTime.Add(90 * time.Minute)
	result, err := s.repository.ListFiltered(s.ctx, models.ListCallsInput{
		UserID:             manager.ID,
		Q:                  "alpha",
		Status:             models.CallStatusAnalyzed,
		VisibilityScope:    models.CallVisibilityScopeCompany,
		CompanyUUID:        uuid.NullUUID{UUID: company.ID, Valid: true},
		UploadedByUserUUID: uuid.NullUUID{UUID: uploader.ID, Valid: true},
		From:               &from,
		To:                 &to,
		Limit:              1,
		Offset:             0,
	})
	s.Require().NoError(err)
	s.Require().Equal(2, result.Total)
	s.Require().Equal(1, result.Limit)
	s.Require().Equal(0, result.Offset)
	s.Require().Len(result.Items, 1)
	s.Require().Equal(second.ID, result.Items[0].ID)

	outsiderResult, err := s.repository.ListFiltered(s.ctx, models.ListCallsInput{
		UserID: outsider.ID,
		Q:      "alpha",
		Limit:  20,
	})
	s.Require().NoError(err)
	s.Require().Zero(outsiderResult.Total)
	s.Require().Empty(outsiderResult.Items)
}

func (s *RepositorySuite) TestListFilteredCountsWhenOffsetReturnsNoRows() {
	company, manager := s.createCompanyWithManager()
	uploader := s.createUser(uuid.NewString() + "@example.com")
	call := testCall(uploader.ID)
	call.VisibilityScope = models.CallVisibilityScopeCompany
	call.CompanyUUID = uuid.NullUUID{UUID: company.ID, Valid: true}
	_, err := s.repository.CreateCall(s.ctx, call)
	s.Require().NoError(err)

	result, err := s.repository.ListFiltered(s.ctx, models.ListCallsInput{
		UserID: manager.ID,
		Limit:  20,
		Offset: 10,
	})

	s.Require().NoError(err)
	s.Require().Equal(1, result.Total)
	s.Require().Empty(result.Items)
}

func (s *RepositorySuite) TestGetFilterOptionsReturnsVisibleUploaders() {
	company, manager := s.createCompanyWithManager()
	department := s.createDepartment(company.ID)
	leader := s.addCompanyEmployee(company.ID, models.CompanyMemberRoleEmployee)
	s.addDepartmentMember(company.ID, department.ID, leader.ID, models.DepartmentMemberRoleLeader)
	companyUploader := s.createUser(uuid.NewString() + "@example.com")
	departmentUploader := s.createUser(uuid.NewString() + "@example.com")

	companyCall := testCall(companyUploader.ID)
	companyCall.VisibilityScope = models.CallVisibilityScopeCompany
	companyCall.CompanyUUID = uuid.NullUUID{UUID: company.ID, Valid: true}
	_, err := s.repository.CreateCall(s.ctx, companyCall)
	s.Require().NoError(err)

	departmentCall := testCall(departmentUploader.ID)
	departmentCall.VisibilityScope = models.CallVisibilityScopeDepartment
	departmentCall.CompanyUUID = uuid.NullUUID{UUID: company.ID, Valid: true}
	departmentCall.DepartmentUUID = uuid.NullUUID{UUID: department.ID, Valid: true}
	_, err = s.repository.CreateCall(s.ctx, departmentCall)
	s.Require().NoError(err)

	managerOptions, err := s.repository.GetFilterOptions(s.ctx, models.CallFilterOptionsInput{
		UserID:      manager.ID,
		CompanyUUID: uuid.NullUUID{UUID: company.ID, Valid: true},
	})
	s.Require().NoError(err)
	s.Require().Len(managerOptions.Statuses, 5)
	s.Require().Len(managerOptions.Scopes, 3)
	s.Require().Len(managerOptions.Managers, 2)

	leaderOptions, err := s.repository.GetFilterOptions(s.ctx, models.CallFilterOptionsInput{
		UserID:         leader.ID,
		CompanyUUID:    uuid.NullUUID{UUID: company.ID, Valid: true},
		DepartmentUUID: uuid.NullUUID{UUID: department.ID, Valid: true},
	})
	s.Require().NoError(err)
	s.Require().Len(leaderOptions.Managers, 1)
	s.Require().Equal(departmentUploader.ID, leaderOptions.Managers[0].ID)
}

func (s *RepositorySuite) TestUpdateCallTitleRequiresVisibility() {
	owner := s.createUser(uuid.NewString() + "@example.com")
	outsider := s.createUser(uuid.NewString() + "@example.com")
	call := testCall(owner.ID)
	_, err := s.repository.CreateCall(s.ctx, call)
	s.Require().NoError(err)

	updated, err := s.repository.UpdateCallTitle(s.ctx, call.ID, owner.ID, "Updated")
	s.Require().NoError(err)
	s.Require().Equal("Updated", updated.Title)

	_, err = s.repository.UpdateCallTitle(s.ctx, call.ID, outsider.ID, "Nope")
	s.Require().ErrorIs(err, models.ErrCallNotFound)
}

func (s *RepositorySuite) TestUpdateCallStatusIgnoresVisibility() {
	owner := s.createUser(uuid.NewString() + "@example.com")
	call := testCall(owner.ID)
	_, err := s.repository.CreateCall(s.ctx, call)
	s.Require().NoError(err)

	updated, err := s.repository.UpdateCallStatus(s.ctx, call.ID, models.CallStatusProcessing)
	s.Require().NoError(err)
	s.Require().Equal(models.CallStatusProcessing, updated.Status)

	_, err = s.repository.UpdateCallStatus(s.ctx, uuid.New(), models.CallStatusProcessing)
	s.Require().ErrorIs(err, models.ErrCallNotFound)
}

func (s *RepositorySuite) TestDeleteCallRequiresVisibility() {
	owner := s.createUser(uuid.NewString() + "@example.com")
	outsider := s.createUser(uuid.NewString() + "@example.com")
	call := testCall(owner.ID)
	_, err := s.repository.CreateCall(s.ctx, call)
	s.Require().NoError(err)

	err = s.repository.DeleteCall(s.ctx, call.ID, outsider.ID)
	s.Require().ErrorIs(err, models.ErrCallNotFound)

	err = s.repository.DeleteCall(s.ctx, call.ID, owner.ID)
	s.Require().NoError(err)

	_, err = s.repository.GetByUUIDForProcessing(s.ctx, call.ID)
	s.Require().ErrorIs(err, models.ErrCallNotFound)
}

func (s *RepositorySuite) TestDeleteCallRemovesProcessingJobs() {
	owner := s.createUser(uuid.NewString() + "@example.com")
	call := testCall(owner.ID)
	now := time.Now().UTC().Truncate(time.Microsecond)
	job := models.ProcessingJob{
		ID:          uuid.New(),
		Type:        models.ProcessingJobTypeTranscribeCall,
		EntityUUID:  call.ID,
		Status:      models.ProcessingJobStatusPending,
		MaxAttempts: models.DefaultProcessingJobMaxAttempts,
		AvailableAt: now,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	_, err := s.repository.CreateCallWithProcessingJob(s.ctx, call, job)
	s.Require().NoError(err)

	var jobsBefore int
	err = s.db.QueryRowContext(s.ctx, `SELECT COUNT(*) FROM processing_jobs WHERE entity_uuid = $1`, call.ID).Scan(&jobsBefore)
	s.Require().NoError(err)
	s.Require().Positive(jobsBefore)

	err = s.repository.DeleteCall(s.ctx, call.ID, owner.ID)
	s.Require().NoError(err)

	var jobsAfter int
	err = s.db.QueryRowContext(s.ctx, `SELECT COUNT(*) FROM processing_jobs WHERE entity_uuid = $1`, call.ID).Scan(&jobsAfter)
	s.Require().NoError(err)
	s.Zero(jobsAfter)
}
