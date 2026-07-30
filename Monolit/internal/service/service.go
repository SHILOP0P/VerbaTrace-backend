package service

import (
	"context"

	"calllens/monolit/internal/models"

	"github.com/google/uuid"
)

type CallService interface {
	//POST
	CreateCall(ctx context.Context, input models.CreateCallInput) (models.Call, error)

	//GET
	List(ctx context.Context, userID uuid.UUID) ([]models.Call, error)
	ListFiltered(ctx context.Context, input models.ListCallsInput) (models.ListCallsResult, error)
	GetFilterOptions(ctx context.Context, input models.CallFilterOptionsInput) (models.CallFilterOptions, error)
	GetByUUID(ctx context.Context, id uuid.UUID, userID uuid.UUID) (models.Call, error)
	GetAudioByUUID(ctx context.Context, id uuid.UUID, userID uuid.UUID) (models.File, error)
	GetTranscriptionByCallUUID(ctx context.Context, id uuid.UUID, userID uuid.UUID) (models.Transcription, error)

	//UPDATE
	UpdateCallTitle(ctx context.Context, id uuid.UUID, userID uuid.UUID, title string) (models.Call, error)
	UpdateCallStatus(ctx context.Context, input models.UpdateCallStatusInput) (models.Call, error)
	//DELETE
	DeleteCall(ctx context.Context, id uuid.UUID, userID uuid.UUID) error
}

type AnalyticsService interface {
	GetOverview(ctx context.Context, input models.AnalyticsOverviewInput) (models.AnalyticsOverview, error)
	CreateDeepAnalysis(ctx context.Context, input models.CreateDeepAnalysisInput) (models.AggregateAnalysis, error)
	ListDeepAnalyses(ctx context.Context, input models.ListDeepAnalysesInput) (models.ListAggregateAnalysesResult, error)
	GetDeepAnalysis(ctx context.Context, id uuid.UUID, userID uuid.UUID) (models.AggregateAnalysis, error)
	CreateAggregateReport(ctx context.Context, input models.CreateAggregateReportInput) (models.AggregateReportExport, error)
	ListAggregateReports(ctx context.Context, analysisID uuid.UUID, userID uuid.UUID) ([]models.AggregateReportExport, error)
	GetAggregateReportFile(ctx context.Context, reportID uuid.UUID, userID uuid.UUID) (models.AggregateReportFile, error)
	DeleteAggregateReport(ctx context.Context, reportID uuid.UUID, userID uuid.UUID) error
}

type CallFolderService interface {
	Create(ctx context.Context, input models.CreateCallFolderInput) (models.CallFolder, error)
	Get(ctx context.Context, id uuid.UUID, userID uuid.UUID) (models.CallFolder, error)
	List(ctx context.Context, input models.ListCallFoldersInput) (models.ListCallFoldersResult, error)
	Update(ctx context.Context, input models.UpdateCallFolderInput) (models.CallFolder, error)
	Delete(ctx context.Context, id uuid.UUID, userID uuid.UUID) error
	AssignCall(ctx context.Context, input models.AssignCallToFolderInput) error
	RemoveCall(ctx context.Context, input models.RemoveCallFromFolderInput) error
	ListFolderCalls(ctx context.Context, input models.ListFolderCallsInput) (models.ListCallsResult, error)
	GrantAccess(ctx context.Context, input models.GrantCallFolderAccessInput) (models.CallFolderAccess, error)
	RevokeAccess(ctx context.Context, input models.RevokeCallFolderAccessInput) error
	ListAccesses(ctx context.Context, folderID uuid.UUID, userID uuid.UUID) ([]models.CallFolderAccess, error)
	ReplaceInstructions(ctx context.Context, userID uuid.UUID, folderID uuid.UUID, instructionIDs []uuid.UUID) error
}

type MonitoringService interface {
	GetProcessing(ctx context.Context, input models.ProcessingMonitoringInput) (models.ProcessingMonitoring, error)
}

type ContactService interface {
	SearchContacts(ctx context.Context, userID uuid.UUID, value string) ([]models.PublicUser, error)
	AddContact(ctx context.Context, input models.AddContactInput) error
	RemoveContact(ctx context.Context, input models.AddContactInput) error
	ListContacts(ctx context.Context, userID uuid.UUID) (models.ContactList, error)
	AddFavoriteCall(ctx context.Context, input models.FavoriteCallInput) error
	RemoveFavoriteCall(ctx context.Context, input models.FavoriteCallInput) error
	ListFavoriteCalls(ctx context.Context, userID uuid.UUID) (models.FavoriteCallList, error)
}

type SearchService interface {
	Search(ctx context.Context, input models.SearchInput) (models.SearchResult, error)
}

type NotificationService interface {
	Create(ctx context.Context, input models.CreateNotificationInput) (models.Notification, error)
	List(ctx context.Context, input models.ListNotificationsInput) (models.ListNotificationsResult, error)
	MarkRead(ctx context.Context, id uuid.UUID, userID uuid.UUID) (models.Notification, error)
	MarkAllRead(ctx context.Context, userID uuid.UUID) error
}

type AuthService interface {
	Register(ctx context.Context, input models.CreateUserInput) (models.CurrentUser, error)
	Login(ctx context.Context, input models.LoginInput) (models.CurrentUser, string, string, error)
	Refresh(ctx context.Context, input models.RefreshTokenInput) (models.CurrentUser, string, string, error)
	Logout(ctx context.Context, sessionID uuid.UUID) error
	LogoutAll(ctx context.Context, userID uuid.UUID, currentSessionID uuid.UUID) error
	Me(ctx context.Context, userID uuid.UUID) (models.CurrentUser, error)
	UpdateUsername(ctx context.Context, input models.UpdateUsernameInput) (models.CurrentUser, error)
	UpdatePassword(ctx context.Context, input models.UpdatePasswordInput) (models.UpdatePasswordResult, error)
	ListSessions(ctx context.Context, userID uuid.UUID, currentSessionID uuid.UUID) ([]models.UserSession, error)
	RevokeSession(ctx context.Context, userID uuid.UUID, currentSessionID uuid.UUID, sessionID uuid.UUID) error
	GetUserByUsername(ctx context.Context, username string) (models.CurrentUser, error)
	UpdateProfile(ctx context.Context, input models.UpdateUserProfileInput) (models.CurrentUser, error)
	UploadAvatar(ctx context.Context, input models.SaveUserAvatarInput) (models.UserAvatarResponse, error)
	DeleteAvatar(ctx context.Context, userID uuid.UUID) (models.UserAvatarResponse, error)
	GetAvatar(ctx context.Context, userID uuid.UUID) (models.File, error)
	GetPreferences(ctx context.Context, userID uuid.UUID) (models.UserPreferences, error)
	UpdatePreferences(ctx context.Context, input models.UpdateUserPreferencesInput) (models.UserPreferences, error)
}

type AdminService interface {
	GetCapabilities(ctx context.Context, role models.UserRole) (models.AdminCapabilities, error)
	RecordAudit(ctx context.Context, input models.CreateAdminAuditLogInput) (models.AdminAuditLog, error)
	ListUsers(ctx context.Context, input models.ListAdminUsersInput) (models.ListAdminUsersResult, error)
	GetUser(ctx context.Context, userID uuid.UUID) (models.AdminUser, error)
	UpdateUserProfile(ctx context.Context, input models.UpdateAdminUserProfileInput) (models.AdminUser, error)
	ChangeUserRole(ctx context.Context, input models.ChangeAdminUserRoleInput) (models.AdminUser, error)
	ListUserSessions(ctx context.Context, actorUserID uuid.UUID, targetUserID uuid.UUID) ([]models.AdminUserSession, error)
	RevokeUserSession(ctx context.Context, input models.AdminSessionMutationInput) error
	RevokeAllUserSessions(ctx context.Context, input models.AdminSessionMutationInput) error
	ListCompanies(ctx context.Context, input models.ListAdminCompaniesInput) (models.ListAdminCompaniesResult, error)
	GetCompany(ctx context.Context, companyID uuid.UUID) (models.AdminCompany, error)
	GetPersonalSubscription(ctx context.Context, userID uuid.UUID) (models.AdminSubscription, error)
	GetCompanySubscription(ctx context.Context, companyID uuid.UUID) (models.AdminSubscription, error)
	GrantSubscription(ctx context.Context, input models.GrantAdminSubscriptionInput) (models.AdminSubscription, error)
	CancelSubscription(ctx context.Context, input models.CancelAdminSubscriptionInput) (models.AdminSubscription, error)
	ResetUsage(ctx context.Context, input models.ResetAdminUsageInput) error
	GetCall(ctx context.Context, id uuid.UUID) (models.Call, error)
	ListUserCalls(ctx context.Context, userID uuid.UUID, limit int, offset int) (models.ListCallsResult, error)
	GetCallAudio(ctx context.Context, id uuid.UUID) (models.File, error)
}

type CompanyService interface {
	CreateCompany(ctx context.Context, input models.CreateCompanyInput) (models.Company, error)
	UpdateCompany(ctx context.Context, input models.UpdateCompanyInput) (models.Company, error)
	UpdateCompanyTag(ctx context.Context, input models.UpdateCompanyTagInput) (models.Company, error)
	UpdateCompanyTagAsAdmin(ctx context.Context, companyID uuid.UUID, tag string) (models.Company, error)
	DeleteCompany(ctx context.Context, input models.DeleteCompanyInput) error
	AddCompanyMember(ctx context.Context, input models.AddCompanyMemberInput) (models.CompanyMember, error)
	UpdateCompanyMemberRole(ctx context.Context, input models.UpdateCompanyMemberRoleInput) (models.CompanyMember, error)
	UpdateCompanyMemberStatus(ctx context.Context, input models.UpdateCompanyMemberStatusInput) (models.CompanyMember, error)
	UpdateCompanyMemberJobTitle(ctx context.Context, input models.UpdateCompanyMemberJobTitleInput) (models.CompanyMember, error)
	LeaveCompany(ctx context.Context, companyID uuid.UUID, userID uuid.UUID) (models.CompanyMember, error)
	ListUserCompanies(ctx context.Context, userID uuid.UUID) ([]models.Company, error)
	GetCompanyByUUID(ctx context.Context, companyID uuid.UUID, userID uuid.UUID) (models.Company, error)
	GetCompanyMembersOverview(ctx context.Context, companyID uuid.UUID, userID uuid.UUID) (models.CompanyMembersOverview, error)
	ListCompanyMembers(ctx context.Context, input models.ListCompanyMembersInput) (models.CompanyMembersResult, error)
}

type DepartmentService interface {
	CreateDepartment(ctx context.Context, input models.CreateDepartmentInput) (models.Department, error)
	UpdateDepartment(ctx context.Context, input models.UpdateDepartmentInput) (models.Department, error)
	DeleteDepartment(ctx context.Context, input models.DeleteDepartmentInput) error
	AddDepartmentMember(ctx context.Context, input models.AddDepartmentMemberInput) (models.DepartmentMember, error)
	ListDepartmentMembers(ctx context.Context, companyID uuid.UUID, departmentID uuid.UUID, userID uuid.UUID) ([]models.DepartmentMember, error)
	UpdateDepartmentMemberRole(ctx context.Context, input models.UpdateDepartmentMemberRoleInput) (models.DepartmentMember, error)
	UpdateDepartmentMemberStatus(ctx context.Context, input models.UpdateDepartmentMemberStatusInput) (models.DepartmentMember, error)
	ListCompanyDepartments(ctx context.Context, companyID uuid.UUID, userID uuid.UUID) ([]models.Department, error)
}

type AnalysisInstructionService interface {
	Create(ctx context.Context, input models.CreateAnalysisInstructionInput) (models.AnalysisInstruction, error)
	Get(ctx context.Context, id uuid.UUID, userID uuid.UUID) (models.AnalysisInstruction, error)
	List(ctx context.Context, input models.ListAnalysisInstructionsInput) ([]models.AnalysisInstruction, error)
	Update(ctx context.Context, input models.UpdateAnalysisInstructionInput) (models.AnalysisInstruction, error)
	ReplaceFile(ctx context.Context, input models.ReplaceAnalysisInstructionFileInput) (models.AnalysisInstruction, error)
	GetFile(ctx context.Context, id uuid.UUID, userID uuid.UUID) (models.File, error)
	Reorder(ctx context.Context, input models.ReorderAnalysisInstructionsInput) error
	Delete(ctx context.Context, id uuid.UUID, userID uuid.UUID) error
}

type AnalysisService interface {
	AnalyzeCall(ctx context.Context, input models.AnalyzeCallInput) (models.CallAnalysis, error)
	GetByCallUUID(ctx context.Context, callUUID uuid.UUID, userID uuid.UUID) (models.CallAnalysis, error)
}

type ReportService interface {
	Create(ctx context.Context, input models.CreateReportInput) (models.ReportExport, error)
	CreateGlobal(ctx context.Context, input models.CreateGlobalReportInput) (models.ReportExport, error)
	List(ctx context.Context, input models.ListReportsInput) (models.ListReportsResult, error)
	ListByCallUUID(ctx context.Context, callID uuid.UUID, userID uuid.UUID) ([]models.ReportExport, error)
	GetFile(ctx context.Context, reportID uuid.UUID, userID uuid.UUID) (models.ReportFile, error)
	Delete(ctx context.Context, reportID uuid.UUID, userID uuid.UUID) error
}

type BillingService interface {
	ListPlans(ctx context.Context) ([]models.Plan, error)
	GetPersonalSubscription(ctx context.Context, userID uuid.UUID) (models.Subscription, error)
	GetCompanySubscription(ctx context.Context, input models.GetCompanySubscriptionInput) (models.Subscription, error)
	GetPersonalSubscriptionUsage(ctx context.Context, input models.GetPersonalSubscriptionUsageInput) (models.SubscriptionUsage, error)
	GetCompanySubscriptionUsage(ctx context.Context, input models.GetCompanySubscriptionUsageInput) (models.SubscriptionUsage, error)
	ActivatePersonalSubscription(ctx context.Context, input models.ActivatePersonalSubscriptionInput) (models.Subscription, error)
	ActivateCompanySubscription(ctx context.Context, input models.ActivateCompanySubscriptionInput) (models.Subscription, error)
	CancelCompanySubscription(ctx context.Context, input models.CancelCompanySubscriptionInput) (models.Subscription, error)
}

type InvitationService interface {
	CreateCompanyInvitation(ctx context.Context, input models.CreateCompanyInvitationInput) (models.MembershipInvitation, error)
	CreateDepartmentInvitation(ctx context.Context, input models.CreateDepartmentInvitationInput) (models.MembershipInvitation, error)
	ListUserInvitations(ctx context.Context, input models.ListUserInvitationsInput) ([]models.MembershipInvitation, error)
	AcceptInvitation(ctx context.Context, input models.AcceptInvitationInput) (models.MembershipInvitation, error)
	DeclineInvitation(ctx context.Context, input models.DeclineInvitationInput) (models.MembershipInvitation, error)
	CancelInvitation(ctx context.Context, input models.CancelInvitationInput) (models.MembershipInvitation, error)
}
