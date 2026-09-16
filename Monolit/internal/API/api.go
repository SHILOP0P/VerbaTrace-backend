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
	UpdateTranscription(w http.ResponseWriter, r *http.Request)
	ListTranscriptionRevisions(w http.ResponseWriter, r *http.Request)
	GetTranscriptionRevision(w http.ResponseWriter, r *http.Request)
	RestoreTranscriptionRevision(w http.ResponseWriter, r *http.Request)
	ListTranscriptionSpeakerAssignments(w http.ResponseWriter, r *http.Request)
	ReplaceTranscriptionSpeakerAssignments(w http.ResponseWriter, r *http.Request)
	ListDeletedCalls(w http.ResponseWriter, r *http.Request)

	//UPDATE
	UpdateCallTitle(w http.ResponseWriter, r *http.Request)
	//DELETE
	DeleteCall(w http.ResponseWriter, r *http.Request)
	RestoreCall(w http.ResponseWriter, r *http.Request)
}

type AnalyticsAPI interface {
	GetOverview(w http.ResponseWriter, r *http.Request)
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
	ReplaceInstructions(w http.ResponseWriter, r *http.Request)
}

type MonitoringAPI interface {
	GetProcessing(w http.ResponseWriter, r *http.Request)
}

type ContactAPI interface {
	SearchContacts(w http.ResponseWriter, r *http.Request)
	ListContacts(w http.ResponseWriter, r *http.Request)
	AddContact(w http.ResponseWriter, r *http.Request)
	RemoveContact(w http.ResponseWriter, r *http.Request)
	ListFavoriteCalls(w http.ResponseWriter, r *http.Request)
	AddFavoriteCall(w http.ResponseWriter, r *http.Request)
	RemoveFavoriteCall(w http.ResponseWriter, r *http.Request)
}

type SearchAPI interface {
	Search(w http.ResponseWriter, r *http.Request)
}

type NotificationAPI interface {
	List(w http.ResponseWriter, r *http.Request)
	Events(w http.ResponseWriter, r *http.Request)
	MarkRead(w http.ResponseWriter, r *http.Request)
	MarkUnread(w http.ResponseWriter, r *http.Request)
	MarkAllRead(w http.ResponseWriter, r *http.Request)
}

type ActionAPI interface {
	Create(http.ResponseWriter, *http.Request)
	SetDisposition(http.ResponseWriter, *http.Request)
	Get(http.ResponseWriter, *http.Request)
	GetAdmin(http.ResponseWriter, *http.Request)
	List(http.ResponseWriter, *http.Request)
	ListAdmin(http.ResponseWriter, *http.Request)
	ListAssignees(http.ResponseWriter, *http.Request)
	ListAssigneesAdmin(http.ResponseWriter, *http.Request)
	Start(http.ResponseWriter, *http.Request)
	Complete(http.ResponseWriter, *http.Request)
	Cancel(http.ResponseWriter, *http.Request)
	Reschedule(http.ResponseWriter, *http.Request)
	Reassign(http.ResponseWriter, *http.Request)
	Edit(http.ResponseWriter, *http.Request)
	RevertStatus(http.ResponseWriter, *http.Request)
	CreateTransfer(http.ResponseWriter, *http.Request)
	ApproveTransfer(http.ResponseWriter, *http.Request)
	RejectTransfer(http.ResponseWriter, *http.Request)
	CompleteAdmin(http.ResponseWriter, *http.Request)
	CancelAdmin(http.ResponseWriter, *http.Request)
	RescheduleAdmin(http.ResponseWriter, *http.Request)
	ReassignAdmin(http.ResponseWriter, *http.Request)
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
	UpdateCompanyMemberRole(w http.ResponseWriter, r *http.Request)
	RemoveCompanyMember(w http.ResponseWriter, r *http.Request)
	OfferOwnership(w http.ResponseWriter, r *http.Request)
	AcceptOwnership(w http.ResponseWriter, r *http.Request)
	DeclineOwnership(w http.ResponseWriter, r *http.Request)
	CancelOwnershipOffer(w http.ResponseWriter, r *http.Request)
	ListIncomingOwnership(w http.ResponseWriter, r *http.Request)
	UpdateCompanyMemberJobTitle(w http.ResponseWriter, r *http.Request)
	LeaveCompany(w http.ResponseWriter, r *http.Request)
	List(w http.ResponseWriter, r *http.Request)
	GetByUUID(w http.ResponseWriter, r *http.Request)
	GetCompanyMembersOverview(w http.ResponseWriter, r *http.Request)
}

type DepartmentTransferAPI interface {
	CreateDepartmentTransfer(w http.ResponseWriter, r *http.Request)
	ListDepartmentTransfers(w http.ResponseWriter, r *http.Request)
	ApproveDepartmentTransfer(w http.ResponseWriter, r *http.Request)
	RejectDepartmentTransfer(w http.ResponseWriter, r *http.Request)
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
	ListVersions(w http.ResponseWriter, r *http.Request)
	GetVersionFile(w http.ResponseWriter, r *http.Request)
}

type AnalysisAPI interface {
	AnalyzeCall(w http.ResponseWriter, r *http.Request)
	GetByCallUUID(w http.ResponseWriter, r *http.Request)
	ListAppliedInstructions(w http.ResponseWriter, r *http.Request)
	GetAppliedInstruction(w http.ResponseWriter, r *http.Request)
	RequestRerun(w http.ResponseWriter, r *http.Request)
	DecideRerun(w http.ResponseWriter, r *http.Request)
	ListRerunRequests(w http.ResponseWriter, r *http.Request)
}

type QualityReviewAPI interface {
	Create(w http.ResponseWriter, r *http.Request)
	GetAnalysisContext(w http.ResponseWriter, r *http.Request)
	ChallengeAnalysis(w http.ResponseWriter, r *http.Request)
	List(w http.ResponseWriter, r *http.Request)
	Get(w http.ResponseWriter, r *http.Request)
	Claim(w http.ResponseWriter, r *http.Request)
	SaveDraft(w http.ResponseWriter, r *http.Request)
	DiscardDraft(w http.ResponseWriter, r *http.Request)
	Publish(w http.ResponseWriter, r *http.Request)
	CreateAppeal(w http.ResponseWriter, r *http.Request)
	ResolveAppeal(w http.ResponseWriter, r *http.Request)
	ListEvents(w http.ResponseWriter, r *http.Request)
	CreateAnalysisComment(w http.ResponseWriter, r *http.Request)
	UpdateAnalysisComment(w http.ResponseWriter, r *http.Request)
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

type IntegrationAPI interface {
	CreateConnection(http.ResponseWriter, *http.Request)
	ListConnections(http.ResponseWriter, *http.Request)
	GetConnection(http.ResponseWriter, *http.Request)
	UpdateConnection(http.ResponseWriter, *http.Request)
	EnableConnection(http.ResponseWriter, *http.Request)
	DisableConnection(http.ResponseWriter, *http.Request)
	RevokeConnection(http.ResponseWriter, *http.Request)
	IngestSandbox(http.ResponseWriter, *http.Request)
	IngestProduction(http.ResponseWriter, *http.Request)
	UploadSandbox(http.ResponseWriter, *http.Request)
	UploadProduction(http.ResponseWriter, *http.Request)
	GetSandboxIngest(http.ResponseWriter, *http.Request)
	GetProductionIngest(http.ResponseWriter, *http.Request)
	ListSandboxDestinations(http.ResponseWriter, *http.Request)
	ListProductionDestinations(http.ResponseWriter, *http.Request)
	ListSandboxFolders(http.ResponseWriter, *http.Request)
	ListProductionFolders(http.ResponseWriter, *http.Request)
	GetSandboxCall(http.ResponseWriter, *http.Request)
	GetProductionCall(http.ResponseWriter, *http.Request)
	ListSandboxCalls(http.ResponseWriter, *http.Request)
	ListProductionCalls(http.ResponseWriter, *http.Request)
	GetSandboxCallBySourceRef(http.ResponseWriter, *http.Request)
	GetProductionCallBySourceRef(http.ResponseWriter, *http.Request)
	GetSandboxTranscription(http.ResponseWriter, *http.Request)
	GetProductionTranscription(http.ResponseWriter, *http.Request)
	GetSandboxAnalysis(http.ResponseWriter, *http.Request)
	GetProductionAnalysis(http.ResponseWriter, *http.Request)
	GetSandboxUsage(http.ResponseWriter, *http.Request)
	GetProductionUsage(http.ResponseWriter, *http.Request)
	CreateWebhook(http.ResponseWriter, *http.Request)
	ListWebhooks(http.ResponseWriter, *http.Request)
	RevokeWebhook(http.ResponseWriter, *http.Request)
	ListWebhookDeliveries(http.ResponseWriter, *http.Request)
	ReplayWebhookDelivery(http.ResponseWriter, *http.Request)
	ListIngestItems(http.ResponseWriter, *http.Request)
	RetryIngestItem(http.ResponseWriter, *http.Request)
	CancelIngestItem(http.ResponseWriter, *http.Request)
	ListAuditEvents(http.ResponseWriter, *http.Request)
	TestWebhook(http.ResponseWriter, *http.Request)
}

type InvitationAPI interface {
	CreateCompanyInvitation(w http.ResponseWriter, r *http.Request)
	CreateDepartmentInvitation(w http.ResponseWriter, r *http.Request)
	ListUserInvitations(w http.ResponseWriter, r *http.Request)
	AcceptInvitation(w http.ResponseWriter, r *http.Request)
	DeclineInvitation(w http.ResponseWriter, r *http.Request)
	CancelCompanyInvitation(w http.ResponseWriter, r *http.Request)
	CancelDepartmentInvitation(w http.ResponseWriter, r *http.Request)
	ListCompanyInvitations(w http.ResponseWriter, r *http.Request)
	ApproveInvitation(w http.ResponseWriter, r *http.Request)
	RejectInvitation(w http.ResponseWriter, r *http.Request)
}
