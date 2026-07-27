package API

import "net/http"

type CallAPI interface {
	//POST
	Create(w http.ResponseWriter, r *http.Request)

	//GET
	GetByUUID(w http.ResponseWriter, r *http.Request)
	Events(w http.ResponseWriter, r *http.Request)
	List(w http.ResponseWriter, r *http.Request)
	GetFilterOptions(w http.ResponseWriter, r *http.Request)
	GetAudioByUUID(w http.ResponseWriter, r *http.Request)
	GetTranscriptionByCallUUID(w http.ResponseWriter, r *http.Request)

	//UPDATE
	UpdateCallTitle(w http.ResponseWriter, r *http.Request)
	//DELETE
	DeleteCall(w http.ResponseWriter, r *http.Request)
}

type AnalyticsAPI interface {
	GetOverview(w http.ResponseWriter, r *http.Request)
	CreateDeepAnalysis(w http.ResponseWriter, r *http.Request)
	ListDeepAnalyses(w http.ResponseWriter, r *http.Request)
	GetDeepAnalysis(w http.ResponseWriter, r *http.Request)
	DeepAnalysisEvents(w http.ResponseWriter, r *http.Request)
	CreateAggregateReport(w http.ResponseWriter, r *http.Request)
	ListAggregateReports(w http.ResponseWriter, r *http.Request)
	DownloadAggregateReport(w http.ResponseWriter, r *http.Request)
	DeleteAggregateReport(w http.ResponseWriter, r *http.Request)
}

type CallFolderAPI interface {
	Create(w http.ResponseWriter, r *http.Request)
	List(w http.ResponseWriter, r *http.Request)
	Get(w http.ResponseWriter, r *http.Request)
	Update(w http.ResponseWriter, r *http.Request)
	Delete(w http.ResponseWriter, r *http.Request)
	ListCalls(w http.ResponseWriter, r *http.Request)
	AssignCall(w http.ResponseWriter, r *http.Request)
	RemoveCall(w http.ResponseWriter, r *http.Request)
	GrantAccess(w http.ResponseWriter, r *http.Request)
	RevokeAccess(w http.ResponseWriter, r *http.Request)
	ListAccesses(w http.ResponseWriter, r *http.Request)
	ReplaceInstructions(w http.ResponseWriter, r *http.Request)
}

type MonitoringAPI interface {
	GetProcessing(w http.ResponseWriter, r *http.Request)
}

type SearchAPI interface {
	Search(w http.ResponseWriter, r *http.Request)
}

type NotificationAPI interface {
	List(w http.ResponseWriter, r *http.Request)
	MarkRead(w http.ResponseWriter, r *http.Request)
	MarkAllRead(w http.ResponseWriter, r *http.Request)
}

type AuthAPI interface {
	Register(w http.ResponseWriter, r *http.Request)
	Login(w http.ResponseWriter, r *http.Request)
	Refresh(w http.ResponseWriter, r *http.Request)
	Logout(w http.ResponseWriter, r *http.Request)
	LogoutAll(w http.ResponseWriter, r *http.Request)
	Me(w http.ResponseWriter, r *http.Request)
	UpdateUsername(w http.ResponseWriter, r *http.Request)
	UpdatePassword(w http.ResponseWriter, r *http.Request)
	ListSessions(w http.ResponseWriter, r *http.Request)
	DeleteSession(w http.ResponseWriter, r *http.Request)
	LookupUser(w http.ResponseWriter, r *http.Request)
	UpdateProfile(w http.ResponseWriter, r *http.Request)
	UploadAvatar(w http.ResponseWriter, r *http.Request)
	GetAvatar(w http.ResponseWriter, r *http.Request)
	DeleteAvatar(w http.ResponseWriter, r *http.Request)
	GetPreferences(w http.ResponseWriter, r *http.Request)
	UpdatePreferences(w http.ResponseWriter, r *http.Request)
}

type AdminAPI interface {
	GetCapabilities(w http.ResponseWriter, r *http.Request)
	ListUsers(w http.ResponseWriter, r *http.Request)
	GetUser(w http.ResponseWriter, r *http.Request)
	ListUserCalls(w http.ResponseWriter, r *http.Request)
	UpdateUserProfile(w http.ResponseWriter, r *http.Request)
	ChangeUserRole(w http.ResponseWriter, r *http.Request)
	ListUserSessions(w http.ResponseWriter, r *http.Request)
	RevokeUserSession(w http.ResponseWriter, r *http.Request)
	RevokeAllUserSessions(w http.ResponseWriter, r *http.Request)
	ListCompanies(w http.ResponseWriter, r *http.Request)
	GetCompany(w http.ResponseWriter, r *http.Request)
	GetPersonalSubscription(w http.ResponseWriter, r *http.Request)
	GetCompanySubscription(w http.ResponseWriter, r *http.Request)
	GrantPersonalSubscription(w http.ResponseWriter, r *http.Request)
	GrantCompanySubscription(w http.ResponseWriter, r *http.Request)
	CancelPersonalSubscription(w http.ResponseWriter, r *http.Request)
	CancelCompanySubscription(w http.ResponseWriter, r *http.Request)
	ResetPersonalUsage(w http.ResponseWriter, r *http.Request)
	ResetCompanyUsage(w http.ResponseWriter, r *http.Request)
	GetCall(w http.ResponseWriter, r *http.Request)
	GetCallAudio(w http.ResponseWriter, r *http.Request)
}

type CompanyAPI interface {
	Create(w http.ResponseWriter, r *http.Request)
	Update(w http.ResponseWriter, r *http.Request)
	UpdateTag(w http.ResponseWriter, r *http.Request)
	UpdateTagAsAdmin(w http.ResponseWriter, r *http.Request)
	Delete(w http.ResponseWriter, r *http.Request)
	AddCompanyMember(w http.ResponseWriter, r *http.Request)
	UpdateCompanyMemberRole(w http.ResponseWriter, r *http.Request)
	UpdateCompanyMemberStatus(w http.ResponseWriter, r *http.Request)
	LeaveCompany(w http.ResponseWriter, r *http.Request)
	List(w http.ResponseWriter, r *http.Request)
	GetByUUID(w http.ResponseWriter, r *http.Request)
	GetCompanyMembersOverview(w http.ResponseWriter, r *http.Request)
}

type DepartmentAPI interface {
	CreateDepartment(w http.ResponseWriter, r *http.Request)
	UpdateDepartment(w http.ResponseWriter, r *http.Request)
	DeleteDepartment(w http.ResponseWriter, r *http.Request)
	AddDepartmentMember(w http.ResponseWriter, r *http.Request)
	ListDepartmentMembers(w http.ResponseWriter, r *http.Request)
	UpdateDepartmentMemberRole(w http.ResponseWriter, r *http.Request)
	UpdateDepartmentMemberStatus(w http.ResponseWriter, r *http.Request)
	ListDepartments(w http.ResponseWriter, r *http.Request)
}

type AnalysisInstructionAPI interface {
	Create(w http.ResponseWriter, r *http.Request)
	List(w http.ResponseWriter, r *http.Request)
	Get(w http.ResponseWriter, r *http.Request)
	Update(w http.ResponseWriter, r *http.Request)
	ReplaceFile(w http.ResponseWriter, r *http.Request)
	GetFile(w http.ResponseWriter, r *http.Request)
	Reorder(w http.ResponseWriter, r *http.Request)
	Delete(w http.ResponseWriter, r *http.Request)
}

type AnalysisAPI interface {
	AnalyzeCall(w http.ResponseWriter, r *http.Request)
	GetByCallUUID(w http.ResponseWriter, r *http.Request)
}

type AnalysisContextAPI interface {
	Get(w http.ResponseWriter, r *http.Request)
	Save(w http.ResponseWriter, r *http.Request)
}

type ReportAPI interface {
	Create(w http.ResponseWriter, r *http.Request)
	CreateGlobal(w http.ResponseWriter, r *http.Request)
	List(w http.ResponseWriter, r *http.Request)
	ListByCallUUID(w http.ResponseWriter, r *http.Request)
	Download(w http.ResponseWriter, r *http.Request)
	Delete(w http.ResponseWriter, r *http.Request)
}

type BillingAPI interface {
	ListPlans(w http.ResponseWriter, r *http.Request)
	GetPersonalSubscription(w http.ResponseWriter, r *http.Request)
	GetCompanySubscription(w http.ResponseWriter, r *http.Request)
	GetPersonalSubscriptionUsage(w http.ResponseWriter, r *http.Request)
	GetCompanySubscriptionUsage(w http.ResponseWriter, r *http.Request)
	ActivatePersonalSubscription(w http.ResponseWriter, r *http.Request)
	ActivateCompanySubscription(w http.ResponseWriter, r *http.Request)
	CancelCompanySubscription(w http.ResponseWriter, r *http.Request)
}

type InvitationAPI interface {
	CreateCompanyInvitation(w http.ResponseWriter, r *http.Request)
	CreateDepartmentInvitation(w http.ResponseWriter, r *http.Request)
	ListUserInvitations(w http.ResponseWriter, r *http.Request)
	AcceptInvitation(w http.ResponseWriter, r *http.Request)
	DeclineInvitation(w http.ResponseWriter, r *http.Request)
	CancelCompanyInvitation(w http.ResponseWriter, r *http.Request)
	CancelDepartmentInvitation(w http.ResponseWriter, r *http.Request)
}
