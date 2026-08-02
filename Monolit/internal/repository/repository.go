package repository

import (
	"context"
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

type CallRepository interface {
	//POST
	CreateCall(ctx context.Context, call models.Call) (models.Call, error)
	CreateCallWithProcessingJob(ctx context.Context, call models.Call, job models.ProcessingJob) (models.Call, error)
	//GET
	List(ctx context.Context, userID uuid.UUID) ([]models.Call, error)
	ListFiltered(ctx context.Context, input models.ListCallsInput) (models.ListCallsResult, error)
	GetFilterOptions(ctx context.Context, input models.CallFilterOptionsInput) (models.CallFilterOptions, error)
	GetByUUID(ctx context.Context, id uuid.UUID, userID uuid.UUID) (models.Call, error)
	GetByUUIDForProcessing(ctx context.Context, id uuid.UUID) (models.Call, error)
	//UPDATE
	UpdateCallTitle(ctx context.Context, id uuid.UUID, userID uuid.UUID, title string) (models.Call, error)
	UpdateCallStatus(ctx context.Context, id uuid.UUID, status models.CallStatus) (models.Call, error)
	//DELETE
	DeleteCall(ctx context.Context, id uuid.UUID, userID uuid.UUID) error
	//PROCESSING
	TakeNextForProcessing(ctx context.Context) (models.Call, error)
}

type AnalyticsRepository interface {
	GetAnalyticsOverview(ctx context.Context, input models.AnalyticsOverviewInput) (models.AnalyticsOverview, error)
	CreateAggregateAnalysis(ctx context.Context, analysis models.AggregateAnalysis) (models.AggregateAnalysis, error)
	GetAggregateAnalysisByUUID(ctx context.Context, id uuid.UUID) (models.AggregateAnalysis, error)
	FindReusableAggregateAnalysis(ctx context.Context, input models.CreateDeepAnalysisInput) (models.AggregateAnalysis, error)
	ListAggregateAnalyses(ctx context.Context, input models.ListDeepAnalysesInput) (models.ListAggregateAnalysesResult, error)
	MarkAggregateAnalysisProcessing(ctx context.Context, id uuid.UUID) (models.AggregateAnalysis, error)
	MarkAggregateAnalysisDone(ctx context.Context, id uuid.UUID, result models.AnalysisResult, sourceCallsCount int) (models.AggregateAnalysis, error)
	MarkAggregateAnalysisFailed(ctx context.Context, id uuid.UUID, errorMessage string) (models.AggregateAnalysis, error)
	ListAggregateAnalysisSourceCalls(ctx context.Context, input models.AnalyticsOverviewInput) ([]models.AggregateAnalysisSourceCall, int, error)
	SpendDeepAnalysisUsage(ctx context.Context, subjectType models.DeepAnalysisSubjectType, subjectID uuid.UUID, periodStart time.Time, periodEnd time.Time) error
}

type CallFolderRepository interface {
	Create(ctx context.Context, folder models.CallFolder) (models.CallFolder, error)
	GetByUUID(ctx context.Context, id uuid.UUID) (models.CallFolder, error)
	GetVisibleByUUID(ctx context.Context, id uuid.UUID, userID uuid.UUID) (models.CallFolder, error)
	List(ctx context.Context, input models.ListCallFoldersInput) (models.ListCallFoldersResult, error)
	Update(ctx context.Context, input models.UpdateCallFolderInput) (models.CallFolder, error)
	SoftDelete(ctx context.Context, id uuid.UUID) error
	AssignCall(ctx context.Context, input models.AssignCallToFolderInput) error
	RemoveCall(ctx context.Context, input models.RemoveCallFromFolderInput) error
	ListFolderCalls(ctx context.Context, input models.ListFolderCallsInput) (models.ListCallsResult, error)
	GrantAccess(ctx context.Context, input models.GrantCallFolderAccessInput) (models.CallFolderAccess, error)
	RevokeAccess(ctx context.Context, folderID uuid.UUID, targetUserID uuid.UUID) error
	ListAccesses(ctx context.Context, folderID uuid.UUID) ([]models.CallFolderAccess, error)
}

type UserRepository interface {
	//GET
	GetUserByUUID(ctx context.Context, id uuid.UUID) (models.CurrentUser, error)
	GetUserByEmail(ctx context.Context, email string) (models.CurrentUser, error)
	GetUserByUsername(ctx context.Context, username string) (models.CurrentUser, error)
	//POST
	CreateUser(ctx context.Context, user models.CurrentUser) (models.CurrentUser, error)
	UpdateUsername(ctx context.Context, input models.UpdateUsernameInput) (models.CurrentUser, error)
	UpdatePasswordHash(ctx context.Context, userID uuid.UUID, passwordHash string) (models.CurrentUser, error)
	UpdateProfile(ctx context.Context, input models.UpdateUserProfileInput) (models.CurrentUser, error)
	UpdateAvatar(ctx context.Context, input models.UserAvatarUpdate) (models.CurrentUser, error)
	DeleteAvatar(ctx context.Context, userID uuid.UUID) (models.CurrentUser, error)
}

type ContactRepository interface {
	SearchPublicUsers(ctx context.Context, usernamePrefix string, limit int) ([]models.PublicUser, error)
	GetPublicUserByUUID(ctx context.Context, userID uuid.UUID) (models.PublicUser, error)
	AddContact(ctx context.Context, userID uuid.UUID, contactID uuid.UUID) error
	RemoveContact(ctx context.Context, userID uuid.UUID, contactID uuid.UUID) error
	ListContactIDs(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, error)
	AddFavoriteCall(ctx context.Context, userID uuid.UUID, callID uuid.UUID) error
	RemoveFavoriteCall(ctx context.Context, userID uuid.UUID, callID uuid.UUID) error
	ListFavoriteCallIDs(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, error)
}

type UserPreferencesRepository interface {
	Get(ctx context.Context, userID uuid.UUID) (models.UserPreferences, error)
	Upsert(ctx context.Context, input models.UpdateUserPreferencesInput) (models.UserPreferences, error)
}

type AdminRepository interface {
	CreateAdminAuditLog(ctx context.Context, audit models.AdminAuditLog) (models.AdminAuditLog, error)
	ListAdminUsers(ctx context.Context, input models.ListAdminUsersInput) (models.ListAdminUsersResult, error)
	GetAdminUserByUUID(ctx context.Context, userID uuid.UUID) (models.AdminUser, error)
	ChangeAdminUserRole(ctx context.Context, input models.ChangeAdminUserRoleInput) (models.AdminUser, error)
	ListAdminUserSessions(ctx context.Context, userID uuid.UUID) ([]models.AdminUserSession, error)
	RevokeAdminUserSession(ctx context.Context, input models.AdminSessionMutationInput) error
	RevokeAllAdminUserSessions(ctx context.Context, input models.AdminSessionMutationInput) error
	ListAdminCompanies(ctx context.Context, input models.ListAdminCompaniesInput) (models.ListAdminCompaniesResult, error)
	GetAdminCompanyByUUID(ctx context.Context, companyID uuid.UUID) (models.AdminCompany, error)
	GetAdminPersonalSubscription(ctx context.Context, userID uuid.UUID) (models.AdminSubscription, error)
	GetAdminCompanySubscription(ctx context.Context, companyID uuid.UUID) (models.AdminSubscription, error)
	GrantAdminSubscription(ctx context.Context, input models.GrantAdminSubscriptionInput) (models.AdminSubscription, error)
	CancelAdminSubscription(ctx context.Context, input models.CancelAdminSubscriptionInput) (models.AdminSubscription, error)
	ResetAdminUsage(ctx context.Context, input models.ResetAdminUsageInput) error
}

type CompanyRepository interface {
	CreateCompany(ctx context.Context, company models.Company, member models.CompanyMember) (models.Company, error)
	UpdateCompany(ctx context.Context, companyID uuid.UUID, name string) (models.Company, error)
	UpdateCompanyTag(ctx context.Context, companyID uuid.UUID, tag string) (models.Company, error)
	ArchiveCompany(ctx context.Context, companyID uuid.UUID) error
	AddCompanyMember(ctx context.Context, member models.CompanyMember) (models.CompanyMember, error)
	UpdateCompanyMemberRole(ctx context.Context, companyID uuid.UUID, userID uuid.UUID, role models.CompanyMemberRole) (models.CompanyMember, error)
	UpdateCompanyMemberStatus(ctx context.Context, companyID uuid.UUID, userID uuid.UUID, status models.MembershipStatus) (models.CompanyMember, error)
	CountActiveCompanyManagers(ctx context.Context, companyID uuid.UUID, exceptUserID uuid.UUID) (int, error)
	ListUserCompanies(ctx context.Context, userID uuid.UUID) ([]models.Company, error)
	GetCompanyByUUID(ctx context.Context, companyID uuid.UUID, userID uuid.UUID) (models.Company, error)
	GetManagedCompanyByUserUUID(ctx context.Context, userID uuid.UUID) (models.Company, error)
	GetCompanyMember(ctx context.Context, companyID uuid.UUID, userID uuid.UUID) (models.CompanyMember, error)
	GetCompanyMembersOverview(ctx context.Context, companyID uuid.UUID) (models.CompanyMembersOverview, error)
	ListCompanyMembers(ctx context.Context, input models.ListCompanyMembersInput) (models.CompanyMembersResult, error)
}

type DepartmentRepository interface {
	CreateDepartment(ctx context.Context, department models.Department) (models.Department, error)
	UpdateDepartment(ctx context.Context, companyID uuid.UUID, departmentID uuid.UUID, name string) (models.Department, error)
	ArchiveDepartment(ctx context.Context, companyID uuid.UUID, departmentID uuid.UUID) error
	AddDepartmentMember(ctx context.Context, companyID uuid.UUID, member models.DepartmentMember) (models.DepartmentMember, error)
	ListDepartmentMembers(ctx context.Context, companyID uuid.UUID, departmentID uuid.UUID) ([]models.DepartmentMember, error)
	UpdateDepartmentMemberRole(ctx context.Context, companyID uuid.UUID, departmentID uuid.UUID, userID uuid.UUID, role models.DepartmentMemberRole) (models.DepartmentMember, error)
	UpdateDepartmentMemberStatus(ctx context.Context, companyID uuid.UUID, departmentID uuid.UUID, userID uuid.UUID, status models.MembershipStatus) (models.DepartmentMember, error)
	ListVisibleCompanyDepartments(ctx context.Context, companyID uuid.UUID, userID uuid.UUID) ([]models.Department, error)
	GetDepartmentMember(ctx context.Context, companyID uuid.UUID, departmentID uuid.UUID, userID uuid.UUID) (models.DepartmentMember, error)
}

type InvitationRepository interface {
	CreateInvitation(ctx context.Context, invitation models.MembershipInvitation) (models.MembershipInvitation, error)
	GetInvitationByUUID(ctx context.Context, id uuid.UUID) (models.MembershipInvitation, error)
	ListUserInvitations(ctx context.Context, input models.ListUserInvitationsInput) ([]models.MembershipInvitation, error)
	ListCompanyInvitations(ctx context.Context, companyID uuid.UUID, status models.InvitationStatus) ([]models.MembershipInvitation, error)
	AcceptInvitation(ctx context.Context, id uuid.UUID, now time.Time) (models.MembershipInvitation, error)
	DeclineInvitation(ctx context.Context, id uuid.UUID, now time.Time) (models.MembershipInvitation, error)
	CancelInvitation(ctx context.Context, id uuid.UUID, now time.Time) (models.MembershipInvitation, error)
}

type RefreshSessionRepository interface {
	CreateRefreshSession(ctx context.Context, session models.RefreshSession) (models.RefreshSession, error)
	GetRefreshSessionByHash(ctx context.Context, refreshTokenHash string) (models.RefreshSession, error)
	GetRefreshSessionByUUID(ctx context.Context, sessionID uuid.UUID) (models.RefreshSession, error)
	ListActiveUserRefreshSessions(ctx context.Context, userID uuid.UUID) ([]models.RefreshSession, error)
	RotateRefreshSession(ctx context.Context, oldRefreshTokenHash string, newRefreshTokenHash string, expiresAt time.Time) (models.RefreshSession, error)
	InvalidateSessionAccess(ctx context.Context, sessionID uuid.UUID) error
	InvalidateAllUserAccess(ctx context.Context, userID uuid.UUID) error
	RevokeRefreshSession(ctx context.Context, sessionID uuid.UUID, reason string) error
	RevokeUserRefreshSession(ctx context.Context, userID uuid.UUID, sessionID uuid.UUID, reason string) error
	RevokeAllUserRefreshSessions(ctx context.Context, userID uuid.UUID, reason string) error
	RevokeOtherUserRefreshSessions(ctx context.Context, userID uuid.UUID, keepSessionID uuid.UUID, reason string) error
}

type TranscriptionRepository interface {
	Create(ctx context.Context, transcription models.Transcription) (models.Transcription, error)
	GetByCallUUID(ctx context.Context, callID uuid.UUID) (models.Transcription, error)
	MarkTranscribed(ctx context.Context, id uuid.UUID, text string, segments []models.TranscriptionSegment, words []models.TranscriptionWord, language *string) (models.Transcription, error)
	MarkFailed(ctx context.Context, id uuid.UUID, errorMessage string) (models.Transcription, error)
}

type AnalysisRepository interface {
	Create(ctx context.Context, analysis models.CallAnalysis) (models.CallAnalysis, error)
	GetByCallUUID(ctx context.Context, callID uuid.UUID) (models.CallAnalysis, error)
	MarkProcessing(ctx context.Context, id uuid.UUID) (models.CallAnalysis, error)
	MarkDone(ctx context.Context, id uuid.UUID, result models.AnalysisResult) (models.CallAnalysis, error)
	MarkFailed(ctx context.Context, id uuid.UUID, errorMessage string) (models.CallAnalysis, error)
}

type ReportRepository interface {
	Create(ctx context.Context, report models.ReportExport) (models.ReportExport, error)
	MarkReady(ctx context.Context, input models.MarkReportReadyInput) (models.ReportExport, error)
	MarkFailed(ctx context.Context, input models.MarkReportFailedInput) (models.ReportExport, error)
	GetByUUID(ctx context.Context, id uuid.UUID) (models.ReportExport, error)
	List(ctx context.Context, input models.ListReportsInput, now time.Time) (models.ListReportsResult, error)
	ListByCallUUID(ctx context.Context, callID uuid.UUID, now time.Time) ([]models.ReportExport, error)
	ListExpiredReady(ctx context.Context, now time.Time, limit int) ([]models.ReportExport, error)
	Delete(ctx context.Context, id uuid.UUID) error
}

type AggregateReportRepository interface {
	CreateAggregate(ctx context.Context, report models.AggregateReportExport) (models.AggregateReportExport, error)
	MarkAggregateReady(ctx context.Context, input models.MarkAggregateReportReadyInput) (models.AggregateReportExport, error)
	MarkAggregateFailed(ctx context.Context, input models.MarkAggregateReportFailedInput) (models.AggregateReportExport, error)
	GetAggregateByUUID(ctx context.Context, id uuid.UUID) (models.AggregateReportExport, error)
	ListAggregateByAnalysisUUID(ctx context.Context, analysisID uuid.UUID, now time.Time) ([]models.AggregateReportExport, error)
	DeleteAggregate(ctx context.Context, id uuid.UUID) error
}

type ProcessingJobRepository interface {
	Create(ctx context.Context, job models.ProcessingJob) (models.ProcessingJob, error)
	Enqueue(ctx context.Context, job models.ProcessingJob) (models.ProcessingJob, error)
	TakeNext(ctx context.Context, workerID string, staleAfter time.Duration) (models.ProcessingJob, error)
	MarkDone(ctx context.Context, id uuid.UUID) (models.ProcessingJob, error)
	MarkRetry(ctx context.Context, id uuid.UUID, lastError string, delay time.Duration) (models.ProcessingJob, error)
	MarkFailed(ctx context.Context, id uuid.UUID, lastError string) (models.ProcessingJob, error)
}

type MonitoringRepository interface {
	GetMonitoring(ctx context.Context, input models.ProcessingMonitoringInput) (models.ProcessingMonitoring, error)
}

type SearchRepository interface {
	Search(ctx context.Context, input models.SearchInput) (models.SearchResult, error)
}

type NotificationRepository interface {
	Create(ctx context.Context, input models.CreateNotificationInput) (models.Notification, error)
	List(ctx context.Context, input models.ListNotificationsInput) (models.ListNotificationsResult, error)
	MarkRead(ctx context.Context, id uuid.UUID, userID uuid.UUID, readAt time.Time) (models.Notification, error)
	MarkAllRead(ctx context.Context, userID uuid.UUID, readAt time.Time) error
}

type AnalysisInstructionRepository interface {
	Create(ctx context.Context, instruction models.AnalysisInstruction) (models.AnalysisInstruction, error)
	GetByUUID(ctx context.Context, id uuid.UUID) (models.AnalysisInstruction, error)
	GetByUUIDIncludingInactive(ctx context.Context, id uuid.UUID) (models.AnalysisInstruction, error)
	List(ctx context.Context, input models.ListAnalysisInstructionsInput) ([]models.AnalysisInstruction, error)
	CountActive(ctx context.Context, input models.ListAnalysisInstructionsInput) (int, error)
	Update(ctx context.Context, input models.UpdateAnalysisInstructionRepositoryInput) (models.AnalysisInstruction, error)
	Reorder(ctx context.Context, items []models.ReorderAnalysisInstructionItem) error
	Deactivate(ctx context.Context, id uuid.UUID) error
}

type BillingRepository interface {
	GetPlanByCode(ctx context.Context, code models.PlanCode) (models.Plan, error)
	ListPlans(ctx context.Context) ([]models.Plan, error)
	GetActivePersonalSubscription(ctx context.Context, userID uuid.UUID) (models.Subscription, error)
	GetActiveBusinessSubscription(ctx context.Context, companyID uuid.UUID) (models.Subscription, error)
	GetBestActiveBusinessSubscriptionForManager(ctx context.Context, managerID uuid.UUID) (models.Subscription, error)
	UpsertSubscription(ctx context.Context, input models.UpsertSubscriptionInput) (models.Subscription, error)
	ActivatePersonalSubscription(ctx context.Context, input models.ActivatePersonalSubscriptionInput, startsAt time.Time) (models.Subscription, error)
	ActivateCompanySubscription(ctx context.Context, input models.ActivateCompanySubscriptionInput, startsAt time.Time) (models.Subscription, error)
	CancelCompanySubscription(ctx context.Context, companyID uuid.UUID, canceledAt time.Time) (models.Subscription, error)
	CountUsedMinutes(ctx context.Context, subscriptionID uuid.UUID, periodStart time.Time) (int, error)
	AddUsageMinutes(ctx context.Context, subscriptionID uuid.UUID, periodStart time.Time, minutes int) (models.UsageCounter, error)
	CountOwnerCompanies(ctx context.Context, ownerID uuid.UUID) (int, error)
	CountCompanyDepartments(ctx context.Context, companyID uuid.UUID) (int, error)
	CountCompanyMembers(ctx context.Context, companyID uuid.UUID) (int, error)
	CountActiveInstructions(ctx context.Context, input models.ListAnalysisInstructionsInput) (int, error)
}
