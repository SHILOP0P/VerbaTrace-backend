package httpserver

import (
	"net/http"
	"time"

	"verbatrace/monolit/internal/API"
	"verbatrace/monolit/internal/API/health"
	authMiddleware "verbatrace/monolit/internal/httpserver/middleware"
	"verbatrace/monolit/internal/logger"
	"verbatrace/monolit/internal/models"
	"verbatrace/monolit/internal/repository"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func NewRouter(callAPI API.CallAPI, callFolderAPI API.CallFolderAPI, contactAPI API.ContactAPI, authAPI API.AuthAPI, companyAPI API.CompanyAPI, departmentAPI API.DepartmentAPI, instructionAPI API.AnalysisInstructionAPI, analysisContextAPI API.AnalysisContextAPI, analysisAPI API.AnalysisAPI, qualityReviewAPI API.QualityReviewAPI, actionAPI API.ActionAPI, reportAPI API.ReportAPI, billingAPI API.BillingAPI, invitationAPI API.InvitationAPI, analyticsAPI API.AnalyticsAPI, monitoringAPI API.MonitoringAPI, searchAPI API.SearchAPI, notificationAPI API.NotificationAPI, adminAPI API.AdminAPI, integrationAPI API.IntegrationAPI, healthHandler *health.Handler, jwtSecret string, refreshSessionRepository repository.RefreshSessionRepository, log logger.Logger) http.Handler {
	r := chi.NewRouter()

	authGuard := authMiddleware.Auth(jwtSecret, refreshSessionRepository)
	if healthHandler == nil {
		healthHandler = health.NewHandler()
	}

	r.Use(middleware.RequestID)
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Request-ID", middleware.GetReqID(r.Context()))
			next.ServeHTTP(w, r)
		})
	})
	r.Use(authMiddleware.RequestLogger(log))
	r.Use(authMiddleware.Recoverer(log))
	r.Use(middleware.URLFormat)

	r.Get("/health", healthHandler.Health)
	r.Get("/health/live", healthHandler.Live)
	r.Get("/health/ready", healthHandler.Ready)
	r.Get("/health/startup", healthHandler.Startup)
	if integrationAPI, ok := billingAPI.(interface {
		ValidateSandboxKey(http.ResponseWriter, *http.Request)
		ValidateProductionKey(http.ResponseWriter, *http.Request)
	}); ok {
		r.With(middleware.Timeout(10*time.Second)).Get("/api/sandbox/v1/auth/validate", integrationAPI.ValidateSandboxKey)
		r.With(middleware.Timeout(10*time.Second)).Get("/api/production/v1/auth/validate", integrationAPI.ValidateProductionKey)
	}
	if integrationAPI != nil {
		r.With(middleware.Timeout(30*time.Second)).Post("/api/sandbox/v1/ingest/calls", integrationAPI.IngestSandbox)
		r.With(middleware.Timeout(45*time.Minute)).Post("/api/sandbox/v1/ingest/calls/upload", integrationAPI.UploadSandbox)
		r.With(middleware.Timeout(30*time.Second)).Get("/api/sandbox/v1/ingest/items/{ingest_item_uuid}", integrationAPI.GetSandboxIngest)
		r.With(middleware.Timeout(30*time.Second)).Post("/api/production/v1/ingest/calls", integrationAPI.IngestProduction)
		r.With(middleware.Timeout(45*time.Minute)).Post("/api/production/v1/ingest/calls/upload", integrationAPI.UploadProduction)
		r.With(middleware.Timeout(30*time.Second)).Get("/api/production/v1/ingest/items/{ingest_item_uuid}", integrationAPI.GetProductionIngest)
		r.With(middleware.Timeout(30*time.Second)).Post("/api/sandbox/v2/ingest/calls", integrationAPI.IngestSandbox)
		r.With(middleware.Timeout(45*time.Minute)).Post("/api/sandbox/v2/ingest/calls/upload", integrationAPI.UploadSandbox)
		r.With(middleware.Timeout(30*time.Second)).Get("/api/sandbox/v2/ingest/items/{ingest_item_uuid}", integrationAPI.GetSandboxIngest)
		r.With(middleware.Timeout(30*time.Second)).Get("/api/sandbox/v2/destinations", integrationAPI.ListSandboxDestinations)
		r.With(middleware.Timeout(30*time.Second)).Get("/api/sandbox/v2/folders", integrationAPI.ListSandboxFolders)
		r.With(middleware.Timeout(30*time.Second)).Get("/api/sandbox/v2/calls/{call_uuid}", integrationAPI.GetSandboxCall)
		r.With(middleware.Timeout(30*time.Second)).Get("/api/sandbox/v2/calls/{call_uuid}/transcription", integrationAPI.GetSandboxTranscription)
		r.With(middleware.Timeout(30*time.Second)).Get("/api/sandbox/v2/calls/{call_uuid}/analysis", integrationAPI.GetSandboxAnalysis)
		r.With(middleware.Timeout(30*time.Second)).Post("/api/production/v2/ingest/calls", integrationAPI.IngestProduction)
		r.With(middleware.Timeout(45*time.Minute)).Post("/api/production/v2/ingest/calls/upload", integrationAPI.UploadProduction)
		r.With(middleware.Timeout(30*time.Second)).Get("/api/production/v2/ingest/items/{ingest_item_uuid}", integrationAPI.GetProductionIngest)
		r.With(middleware.Timeout(30*time.Second)).Get("/api/production/v2/destinations", integrationAPI.ListProductionDestinations)
		r.With(middleware.Timeout(30*time.Second)).Get("/api/production/v2/folders", integrationAPI.ListProductionFolders)
		r.With(middleware.Timeout(30*time.Second)).Get("/api/production/v2/calls/{call_uuid}", integrationAPI.GetProductionCall)
		r.With(middleware.Timeout(30*time.Second)).Get("/api/production/v2/calls/{call_uuid}/transcription", integrationAPI.GetProductionTranscription)
		r.With(middleware.Timeout(30*time.Second)).Get("/api/production/v2/calls/{call_uuid}/analysis", integrationAPI.GetProductionAnalysis)
	}
	r.Route("/api/v1", func(r chi.Router) {
		r.With(authGuard).Get("/calls/{uuid}/events", callAPI.Events)
		r.With(authGuard).Get("/analytics/deep-analyses/{uuid}/events", analyticsAPI.DeepAnalysisEvents)
		// Large audio/video uploads must not inherit the normal 10-second API timeout.
		r.With(authGuard).With(middleware.Timeout(45*time.Minute)).Post("/calls", callAPI.Create)

		r.Group(func(r chi.Router) {
			r.Use(middleware.Timeout(10 * time.Second))

			// ADMIN
			r.Route("/admin", func(r chi.Router) {
				r.Use(authGuard)
				r.Use(authMiddleware.RequirePermission(models.AdminPermissionPanelAccess))
				r.Get("/capabilities", adminAPI.GetCapabilities)
				r.With(authMiddleware.RequirePermission(models.AdminPermissionUsersRead)).Get("/users", adminAPI.ListUsers)
				r.With(authMiddleware.RequirePermission(models.AdminPermissionUsersRead)).Get("/users/{user_uuid}", adminAPI.GetUser)
				r.With(authMiddleware.RequirePermission(models.AdminPermissionCallsRead)).Get("/users/{user_uuid}/calls", adminAPI.ListUserCalls)
				r.With(authMiddleware.RequirePermission(models.AdminPermissionUsersManage)).Patch("/users/{user_uuid}/profile", adminAPI.UpdateUserProfile)
				r.With(authMiddleware.RequirePermission(models.AdminPermissionRolesManageHelpers)).Patch("/users/{user_uuid}/role", adminAPI.ChangeUserRole)
				r.With(authMiddleware.RequirePermission(models.AdminPermissionSessionsRead)).Get("/users/{user_uuid}/sessions", adminAPI.ListUserSessions)
				r.With(authMiddleware.RequirePermission(models.AdminPermissionSessionsManage)).Delete("/users/{user_uuid}/sessions", adminAPI.RevokeAllUserSessions)
				r.With(authMiddleware.RequirePermission(models.AdminPermissionSessionsManage)).Delete("/users/{user_uuid}/sessions/{session_uuid}", adminAPI.RevokeUserSession)
				r.With(authMiddleware.RequirePermission(models.AdminPermissionCompaniesRead)).Get("/companies", adminAPI.ListCompanies)
				r.With(authMiddleware.RequirePermission(models.AdminPermissionCompaniesRead)).Get("/companies/{company_uuid}", adminAPI.GetCompany)
				r.With(authMiddleware.RequirePermission(models.AdminPermissionCompaniesManage)).Patch("/companies/{uuid}/tag", companyAPI.UpdateTagAsAdmin)
				r.With(authMiddleware.RequirePermission(models.AdminPermissionSubscriptionsRead)).Get("/users/{user_uuid}/subscription", adminAPI.GetPersonalSubscription)
				r.With(authMiddleware.RequirePermission(models.AdminPermissionSubscriptionsRead)).Get("/companies/{company_uuid}/subscription", adminAPI.GetCompanySubscription)
				r.With(authMiddleware.RequirePermission(models.AdminPermissionSubscriptionsManage)).Post("/users/{user_uuid}/subscription/grant", adminAPI.GrantPersonalSubscription)
				r.With(authMiddleware.RequirePermission(models.AdminPermissionSubscriptionsManage)).Post("/companies/{company_uuid}/subscription/grant", adminAPI.GrantCompanySubscription)
				r.With(authMiddleware.RequirePermission(models.AdminPermissionSubscriptionsManage)).Post("/users/{user_uuid}/subscription/cancel", adminAPI.CancelPersonalSubscription)
				r.With(authMiddleware.RequirePermission(models.AdminPermissionSubscriptionsManage)).Post("/companies/{company_uuid}/subscription/cancel", adminAPI.CancelCompanySubscription)
				r.With(authMiddleware.RequirePermission(models.AdminPermissionSubscriptionsManage)).Post("/users/{user_uuid}/usage/reset", adminAPI.ResetPersonalUsage)
				r.With(authMiddleware.RequirePermission(models.AdminPermissionSubscriptionsManage)).Post("/companies/{company_uuid}/usage/reset", adminAPI.ResetCompanyUsage)
				if bulkResetAPI, ok := adminAPI.(interface {
					BulkResetUsage(http.ResponseWriter, *http.Request)
				}); ok {
					r.With(authMiddleware.RequirePermission(models.AdminPermissionSubscriptionsManage)).Post("/usage/reset/bulk", bulkResetAPI.BulkResetUsage)
				}
				if resetBatchAPI, ok := adminAPI.(interface {
					CreateUsageResetBatch(http.ResponseWriter, *http.Request)
					ApproveUsageResetBatch(http.ResponseWriter, *http.Request)
					ExecuteUsageResetBatch(http.ResponseWriter, *http.Request)
					GetUsageResetBatch(http.ResponseWriter, *http.Request)
					PreviewUsageResetBatch(http.ResponseWriter, *http.Request)
				}); ok {
					r.With(authMiddleware.RequirePermission(models.AdminPermissionSubscriptionsManage)).Post("/usage/reset/batches", resetBatchAPI.CreateUsageResetBatch)
					r.With(authMiddleware.RequirePermission(models.AdminPermissionSubscriptionsManage)).Post("/usage/reset/batches/preview", resetBatchAPI.PreviewUsageResetBatch)
					r.With(authMiddleware.RequirePermission(models.AdminPermissionSubscriptionsManage)).Get("/usage/reset/batches/{batch_uuid}", resetBatchAPI.GetUsageResetBatch)
					r.With(authMiddleware.RequirePermission(models.AdminPermissionSubscriptionsManage)).Post("/usage/reset/batches/{batch_uuid}/approve", resetBatchAPI.ApproveUsageResetBatch)
					r.With(authMiddleware.RequirePermission(models.AdminPermissionSubscriptionsManage)).Post("/usage/reset/batches/{batch_uuid}/execute", resetBatchAPI.ExecuteUsageResetBatch)
				}
				r.With(authMiddleware.RequirePermission(models.AdminPermissionCallsRead)).Get("/calls/{call_uuid}", adminAPI.GetCall)
				r.With(authMiddleware.RequirePermission(models.AdminPermissionCallsRead)).Get("/calls/{call_uuid}/audio", adminAPI.GetCallAudio)
				r.With(authMiddleware.RequirePermission(models.AdminPermissionCallsRead)).Get("/calls/{call_uuid}/media", adminAPI.GetCallAudio)
				r.With(authMiddleware.RequirePermission(models.AdminPermissionActionsRead)).Get("/actions", actionAPI.ListAdmin)
				r.With(authMiddleware.RequirePermission(models.AdminPermissionActionsRead)).Get("/actions/{action_uuid}", actionAPI.GetAdmin)
				r.With(authMiddleware.RequirePermission(models.AdminPermissionActionsManage)).Get("/companies/{uuid}/action-assignees", actionAPI.ListAssigneesAdmin)
				r.With(authMiddleware.RequirePermission(models.AdminPermissionActionsManage)).Post("/actions/{action_uuid}/complete", actionAPI.CompleteAdmin)
				r.With(authMiddleware.RequirePermission(models.AdminPermissionActionsManage)).Post("/actions/{action_uuid}/cancel", actionAPI.CancelAdmin)
				r.With(authMiddleware.RequirePermission(models.AdminPermissionActionsManage)).Post("/actions/{action_uuid}/reschedule", actionAPI.RescheduleAdmin)
				r.With(authMiddleware.RequirePermission(models.AdminPermissionActionsManage)).Post("/actions/{action_uuid}/reassign", actionAPI.ReassignAdmin)
				r.With(authMiddleware.RequirePermission(models.AdminPermissionActionsManage)).Post("/actions/{action_uuid}/reopen", actionAPI.ReopenAdmin)
			})

			//CALL
			//POST
			//GET
			r.With(authGuard).Get("/calls", callAPI.List)
			r.With(authGuard).Get("/calls/filters", callAPI.GetFilterOptions)
			r.With(authGuard).Get("/calls/{uuid}", callAPI.GetByUUID)
			r.With(authGuard).Get("/calls/{uuid}/audio", callAPI.GetAudioByUUID)
			r.With(authGuard).Get("/calls/{uuid}/media", callAPI.GetAudioByUUID)
			r.With(authGuard).Get("/calls/{uuid}/transcription", callAPI.GetTranscriptionByCallUUID)
			r.With(authGuard).Patch("/calls/{uuid}/transcription", callAPI.UpdateTranscription)
			r.With(authGuard).Get("/calls/{uuid}/transcription/revisions", callAPI.ListTranscriptionRevisions)
			r.With(authGuard).Get("/calls/{uuid}/transcription/revisions/{revision}", callAPI.GetTranscriptionRevision)
			r.With(authGuard).Post("/calls/{uuid}/transcription/revisions/{revision}/restore", callAPI.RestoreTranscriptionRevision)
			r.With(authGuard).Get("/calls/{uuid}/transcription/speakers", callAPI.ListTranscriptionSpeakerAssignments)
			r.With(authGuard).Put("/calls/{uuid}/transcription/speakers", callAPI.ReplaceTranscriptionSpeakerAssignments)
			r.With(authGuard).Post("/calls/{uuid}/analysis", analysisAPI.AnalyzeCall)
			r.With(authGuard).Get("/calls/{uuid}/analysis", analysisAPI.GetByCallUUID)
			r.With(authGuard).Get("/analyses/{analysis_uuid}/instructions", analysisAPI.ListAppliedInstructions)
			r.With(authGuard).Get("/analyses/{analysis_uuid}/instructions/{version_uuid}", analysisAPI.GetAppliedInstruction)
			r.With(authGuard).Post("/calls/{uuid}/quality-reviews", qualityReviewAPI.Create)
			r.With(authGuard).Get("/calls/{uuid}/quality-review-context", qualityReviewAPI.GetAnalysisContext)
			r.With(authGuard).Post("/calls/{uuid}/analysis-comments", qualityReviewAPI.CreateAnalysisComment)
			r.With(authGuard).Patch("/analysis-comments/{comment_uuid}", qualityReviewAPI.UpdateAnalysisComment)
			r.With(authGuard).Post("/calls/{uuid}/quality-review-challenge", qualityReviewAPI.ChallengeAnalysis)
			r.With(authGuard).Get("/quality-reviews", qualityReviewAPI.List)
			r.With(authGuard).Get("/quality-reviews/{review_uuid}", qualityReviewAPI.Get)
			r.With(authGuard).Post("/quality-reviews/{review_uuid}/claim", qualityReviewAPI.Claim)
			r.With(authGuard).Put("/quality-reviews/{review_uuid}/draft", qualityReviewAPI.SaveDraft)
			r.With(authGuard).Delete("/quality-reviews/{review_uuid}/draft", qualityReviewAPI.DiscardDraft)
			r.With(authGuard).Post("/quality-reviews/{review_uuid}/publish", qualityReviewAPI.Publish)
			r.With(authGuard).Post("/quality-reviews/{review_uuid}/appeals", qualityReviewAPI.CreateAppeal)
			r.With(authGuard).Post("/quality-review-appeals/{appeal_uuid}/resolve", qualityReviewAPI.ResolveAppeal)
			r.With(authGuard).Get("/quality-reviews/{review_uuid}/events", qualityReviewAPI.ListEvents)

			// ACTIONS
			r.With(authGuard).Put("/calls/{uuid}/analyses/{analysis_uuid}/action-disposition", actionAPI.SetDisposition)
			r.With(authGuard).Post("/calls/{uuid}/actions", actionAPI.Create)
			r.With(authGuard).Get("/actions", actionAPI.List)
			r.With(authGuard).Get("/actions/{action_uuid}", actionAPI.Get)
			r.With(authGuard).Post("/actions/{action_uuid}/start", actionAPI.Start)
			r.With(authGuard).Post("/actions/{action_uuid}/complete", actionAPI.Complete)
			r.With(authGuard).Post("/actions/{action_uuid}/cancel", actionAPI.Cancel)
			r.With(authGuard).Post("/actions/{action_uuid}/reschedule", actionAPI.Reschedule)
			r.With(authGuard).Post("/actions/{action_uuid}/reassign", actionAPI.Reassign)
			r.With(authGuard).Post("/actions/{action_uuid}/reopen", actionAPI.Reopen)
			r.With(authGuard).Post("/actions/{action_uuid}/transfer-requests", actionAPI.CreateTransfer)
			r.With(authGuard).Post("/actions/{action_uuid}/transfer-requests/{request_uuid}/approve", actionAPI.ApproveTransfer)
			r.With(authGuard).Post("/actions/{action_uuid}/transfer-requests/{request_uuid}/reject", actionAPI.RejectTransfer)
			r.With(authGuard).Get("/companies/{uuid}/action-assignees", actionAPI.ListAssignees)
			r.With(authGuard).Post("/calls/{uuid}/reports", reportAPI.Create)
			r.With(authGuard).Get("/calls/{uuid}/reports", reportAPI.ListByCallUUID)
			r.With(authGuard).Get("/reports", reportAPI.List)
			r.With(authGuard).Post("/reports", reportAPI.CreateGlobal)
			r.With(authGuard).Get("/reports/{report_uuid}/download", reportAPI.Download)
			r.With(authGuard).Delete("/reports/{report_uuid}", reportAPI.Delete)
			//UPDATE
			r.With(authGuard).Patch("/calls/{uuid}", callAPI.UpdateCallTitle)
			//DELETE
			r.With(authGuard).Delete("/calls/{uuid}", callAPI.DeleteCall)

			//CALL FOLDERS
			r.With(authGuard).Get("/call-folders", callFolderAPI.List)
			r.With(authGuard).Post("/call-folders", callFolderAPI.Create)
			r.With(authGuard).Get("/call-folders/{folder_uuid}", callFolderAPI.Get)
			r.With(authGuard).Patch("/call-folders/{folder_uuid}", callFolderAPI.Update)
			r.With(authGuard).Delete("/call-folders/{folder_uuid}", callFolderAPI.Delete)
			r.With(authGuard).Get("/call-folders/{folder_uuid}/calls", callFolderAPI.ListCalls)
			r.With(authGuard).Post("/call-folders/{folder_uuid}/calls", callFolderAPI.AssignCall)
			r.With(authGuard).Delete("/call-folders/{folder_uuid}/calls/{call_uuid}", callFolderAPI.RemoveCall)
			r.With(authGuard).Get("/call-folders/{folder_uuid}/accesses", callFolderAPI.ListAccesses)
			r.With(authGuard).Put("/call-folders/{folder_uuid}/accesses/{user_uuid}", callFolderAPI.GrantAccess)
			r.With(authGuard).Delete("/call-folders/{folder_uuid}/accesses/{user_uuid}", callFolderAPI.RevokeAccess)
			r.With(authGuard).Put("/call-folders/{folder_uuid}/instructions", callFolderAPI.ReplaceInstructions)

			//ANALYTICS
			r.With(authGuard).Get("/analytics/overview", analyticsAPI.GetOverview)
			r.With(authGuard).Post("/analytics/deep-analyses", analyticsAPI.CreateDeepAnalysis)
			r.With(authGuard).Get("/analytics/deep-analyses", analyticsAPI.ListDeepAnalyses)
			r.With(authGuard).Get("/analytics/deep-analyses/{uuid}", analyticsAPI.GetDeepAnalysis)
			r.With(authGuard).Post("/analytics/deep-analyses/{uuid}/reports", analyticsAPI.CreateAggregateReport)
			r.With(authGuard).Get("/analytics/deep-analyses/{uuid}/reports", analyticsAPI.ListAggregateReports)
			r.With(authGuard).Get("/analytics/deep-analysis-reports/{report_uuid}/download", analyticsAPI.DownloadAggregateReport)
			r.With(authGuard).Delete("/analytics/deep-analysis-reports/{report_uuid}", analyticsAPI.DeleteAggregateReport)
			r.With(authGuard).With(authMiddleware.RequirePermission(models.AdminPermissionMonitoringRead)).Get("/monitoring/processing", monitoringAPI.GetProcessing)
			r.With(authGuard).Get("/contacts/search", contactAPI.SearchContacts)
			r.With(authGuard).Get("/contacts", contactAPI.ListContacts)
			r.With(authGuard).Put("/contacts/{user_uuid}", contactAPI.AddContact)
			r.With(authGuard).Delete("/contacts/{user_uuid}", contactAPI.RemoveContact)
			r.With(authGuard).Get("/favorite-calls", contactAPI.ListFavoriteCalls)
			r.With(authGuard).Put("/favorite-calls/{call_uuid}", contactAPI.AddFavoriteCall)
			r.With(authGuard).Delete("/favorite-calls/{call_uuid}", contactAPI.RemoveFavoriteCall)
			if integrationAPI != nil {
				r.With(authGuard).Post("/developer/applications/{application_uuid}/connections", integrationAPI.CreateConnection)
				r.With(authGuard).Get("/developer/applications/{application_uuid}/connections", integrationAPI.ListConnections)
				r.With(authGuard).Get("/integrations/{connection_uuid}", integrationAPI.GetConnection)
				r.With(authGuard).Patch("/integrations/{connection_uuid}", integrationAPI.UpdateConnection)
				r.With(authGuard).Post("/integrations/{connection_uuid}/enable", integrationAPI.EnableConnection)
				r.With(authGuard).Post("/integrations/{connection_uuid}/disable", integrationAPI.DisableConnection)
				r.With(authGuard).Delete("/integrations/{connection_uuid}", integrationAPI.RevokeConnection)
				r.With(authGuard).Post("/integrations/{connection_uuid}/webhooks", integrationAPI.CreateWebhook)
				r.With(authGuard).Get("/integrations/{connection_uuid}/webhooks", integrationAPI.ListWebhooks)
				r.With(authGuard).Delete("/integration-webhooks/{webhook_uuid}", integrationAPI.RevokeWebhook)
				r.With(authGuard).Get("/integrations/{connection_uuid}/webhook-deliveries", integrationAPI.ListWebhookDeliveries)
				r.With(authGuard).Post("/integrations/{connection_uuid}/webhook/test", integrationAPI.TestWebhook)
				r.With(authGuard).Get("/integrations/{connection_uuid}/ingest-items", integrationAPI.ListIngestItems)
				r.With(authGuard).Post("/ingest-items/{ingest_item_uuid}/retry", integrationAPI.RetryIngestItem)
				r.With(authGuard).Post("/ingest-items/{ingest_item_uuid}/cancel", integrationAPI.CancelIngestItem)
				r.With(authGuard).Get("/integrations/{connection_uuid}/audit-events", integrationAPI.ListAuditEvents)
			}
			r.With(authGuard).Get("/search", searchAPI.Search)

			//NOTIFICATIONS
			r.With(authGuard).Get("/notifications", notificationAPI.List)
			r.With(authGuard).Get("/notifications/events", notificationAPI.Events)
			r.With(authGuard).Post("/notifications/{uuid}/read", notificationAPI.MarkRead)
			r.With(authGuard).Post("/notifications/{uuid}/unread", notificationAPI.MarkUnread)
			r.With(authGuard).Post("/notifications/read-all", notificationAPI.MarkAllRead)

			//BILLING
			r.Get("/plans", billingAPI.ListPlans)
			r.With(authGuard).Get("/subscription", billingAPI.GetPersonalSubscription)
			r.With(authGuard).Get("/subscription/usage", billingAPI.GetPersonalSubscriptionUsage)
			r.With(authGuard).Get("/companies/{uuid}/subscription", billingAPI.GetCompanySubscription)
			r.With(authGuard).Get("/companies/{uuid}/subscription/usage", billingAPI.GetCompanySubscriptionUsage)
			if dashboardAPI, ok := billingAPI.(interface {
				GetPersonalCreditDashboard(http.ResponseWriter, *http.Request)
				GetCompanyCreditDashboard(http.ResponseWriter, *http.Request)
				UpdateCompanyCreditVisibility(http.ResponseWriter, *http.Request)
			}); ok {
				r.With(authGuard).Get("/credits/dashboard", dashboardAPI.GetPersonalCreditDashboard)
				r.With(authGuard).Get("/companies/{uuid}/credits/dashboard", dashboardAPI.GetCompanyCreditDashboard)
				r.With(authGuard).Patch("/companies/{uuid}/credits/visibility", dashboardAPI.UpdateCompanyCreditVisibility)
			}
			if developerAPI, ok := billingAPI.(interface {
				CreateDeveloperApplication(http.ResponseWriter, *http.Request)
				ListDeveloperApplications(http.ResponseWriter, *http.Request)
				CreateDeveloperAPIKey(http.ResponseWriter, *http.Request)
				RevokeDeveloperAPIKey(http.ResponseWriter, *http.Request)
				RotateDeveloperAPIKey(http.ResponseWriter, *http.Request)
				MockPurchaseCredits(http.ResponseWriter, *http.Request)
				CreateIntegrationServiceAccount(http.ResponseWriter, *http.Request)
				ListIntegrationServiceAccounts(http.ResponseWriter, *http.Request)
				CreateServiceAccountAPIKey(http.ResponseWriter, *http.Request)
				GetDeveloperApplication(http.ResponseWriter, *http.Request)
				DisableDeveloperApplication(http.ResponseWriter, *http.Request)
				EnableDeveloperApplication(http.ResponseWriter, *http.Request)
				RevokeDeveloperApplication(http.ResponseWriter, *http.Request)
				AdjustSandboxWallet(http.ResponseWriter, *http.Request)
				GetSandboxWallet(http.ResponseWriter, *http.Request)
				ListServiceAccountAPIKeys(http.ResponseWriter, *http.Request)
				UpdateDeveloperApplication(http.ResponseWriter, *http.Request)
				RevokeIntegrationServiceAccount(http.ResponseWriter, *http.Request)
			}); ok {
				r.With(authGuard).Get("/developer/applications", developerAPI.ListDeveloperApplications)
				r.With(authGuard).Post("/developer/applications", developerAPI.CreateDeveloperApplication)
				r.With(authGuard).Post("/developer/applications/{application_uuid}/keys", developerAPI.CreateDeveloperAPIKey)
				r.With(authGuard).Delete("/developer/keys/{key_uuid}", developerAPI.RevokeDeveloperAPIKey)
				r.With(authGuard).Post("/developer/keys/{key_uuid}/rotate", developerAPI.RotateDeveloperAPIKey)
				r.With(authGuard).Post("/credits/purchases/mock", developerAPI.MockPurchaseCredits)
				r.With(authGuard).Post("/integrations/{connection_uuid}/service-accounts", developerAPI.CreateIntegrationServiceAccount)
				r.With(authGuard).Get("/integrations/{connection_uuid}/service-accounts", developerAPI.ListIntegrationServiceAccounts)
				r.With(authGuard).Post("/service-accounts/{service_account_uuid}/keys", developerAPI.CreateServiceAccountAPIKey)
				r.With(authGuard).Get("/service-accounts/{service_account_uuid}/keys", developerAPI.ListServiceAccountAPIKeys)
				r.With(authGuard).Delete("/service-accounts/{service_account_uuid}", developerAPI.RevokeIntegrationServiceAccount)
				r.With(authGuard).Get("/developer/applications/{application_uuid}", developerAPI.GetDeveloperApplication)
				r.With(authGuard).Patch("/developer/applications/{application_uuid}", developerAPI.UpdateDeveloperApplication)
				r.With(authGuard).Post("/developer/applications/{application_uuid}/disable", developerAPI.DisableDeveloperApplication)
				r.With(authGuard).Post("/developer/applications/{application_uuid}/enable", developerAPI.EnableDeveloperApplication)
				r.With(authGuard).Post("/developer/applications/{application_uuid}/revoke", developerAPI.RevokeDeveloperApplication)
				r.With(authGuard).Post("/developer/applications/{application_uuid}/sandbox-wallet", developerAPI.AdjustSandboxWallet)
				r.With(authGuard).Get("/developer/applications/{application_uuid}/sandbox-wallet", developerAPI.GetSandboxWallet)
			}

			//INVITATIONS
			r.With(authGuard).Get("/invitations", invitationAPI.ListUserInvitations)
			r.With(authGuard).Post("/invitations/{invitation_uuid}/accept", invitationAPI.AcceptInvitation)
			r.With(authGuard).Post("/invitations/{invitation_uuid}/decline", invitationAPI.DeclineInvitation)

			//AUTH
			r.Post("/auth/register", authAPI.Register)
			r.Post("/auth/login", authAPI.Login)
			r.Post("/auth/refresh", authAPI.Refresh)
			r.With(authGuard).Get("/auth/me", authAPI.Me)
			r.With(authGuard).Patch("/auth/me/password", authAPI.UpdatePassword)
			r.With(authGuard).Get("/auth/me/sessions", authAPI.ListSessions)
			r.With(authGuard).Delete("/auth/me/sessions/{session_uuid}", authAPI.DeleteSession)
			r.With(authGuard).Patch("/auth/me/profile", authAPI.UpdateProfile)
			r.With(authGuard).Post("/auth/me/avatar", authAPI.UploadAvatar)
			r.With(authGuard).Get("/auth/me/avatar", authAPI.GetAvatar)
			r.With(authGuard).Delete("/auth/me/avatar", authAPI.DeleteAvatar)
			r.With(authGuard).Get("/auth/me/preferences", authAPI.GetPreferences)
			r.With(authGuard).Patch("/auth/me/preferences", authAPI.UpdatePreferences)
			r.With(authGuard).Patch("/auth/me/username", authAPI.UpdateUsername)
			r.With(authGuard).Get("/users/lookup", authAPI.LookupUser)
			r.With(authGuard).Post("/auth/logout", authAPI.Logout)
			r.With(authGuard).Post("/auth/logout-all", authAPI.LogoutAll)

			//COMPANY
			r.With(authGuard).Post("/companies", companyAPI.Create)
			r.With(authGuard).Get("/companies", companyAPI.List)
			r.With(authGuard).Get("/companies/{uuid}", companyAPI.GetByUUID)
			r.With(authGuard).Patch("/companies/{uuid}", companyAPI.Update)
			r.With(authGuard).Patch("/companies/{uuid}/tag", companyAPI.UpdateTag)
			r.With(authGuard).Delete("/companies/{uuid}", companyAPI.Delete)
			r.With(authGuard).Get("/companies/{uuid}/members", companyAPI.GetCompanyMembersOverview)
			r.With(authGuard).Post("/companies/{uuid}/members", companyAPI.AddCompanyMember)
			r.With(authGuard).Post("/companies/{uuid}/invitations", invitationAPI.CreateCompanyInvitation)
			r.With(authGuard).Post("/companies/{uuid}/invitations/{invitation_uuid}/cancel", invitationAPI.CancelCompanyInvitation)
			r.With(authGuard).Patch("/companies/{uuid}/members/{user_uuid}/role", companyAPI.UpdateCompanyMemberRole)
			r.With(authGuard).Patch("/companies/{uuid}/members/{user_uuid}/status", companyAPI.UpdateCompanyMemberStatus)
			r.With(authGuard).Patch("/companies/{uuid}/members/{user_uuid}/job-title", companyAPI.UpdateCompanyMemberJobTitle)
			r.With(authGuard).Post("/companies/{uuid}/leave", companyAPI.LeaveCompany)
			r.With(authGuard).Post("/companies/{uuid}/departments", departmentAPI.CreateDepartment)
			r.With(authGuard).Get("/companies/{uuid}/departments", departmentAPI.ListDepartments)
			r.With(authGuard).Patch("/companies/{uuid}/departments/{department_uuid}", departmentAPI.UpdateDepartment)
			r.With(authGuard).Delete("/companies/{uuid}/departments/{department_uuid}", departmentAPI.DeleteDepartment)
			r.With(authGuard).Get("/companies/{uuid}/departments/{department_uuid}/members", departmentAPI.ListDepartmentMembers)
			r.With(authGuard).Post("/companies/{uuid}/departments/{department_uuid}/members", departmentAPI.AddDepartmentMember)
			r.With(authGuard).Post("/companies/{uuid}/departments/{department_uuid}/invitations", invitationAPI.CreateDepartmentInvitation)
			r.With(authGuard).Post("/companies/{uuid}/departments/{department_uuid}/invitations/{invitation_uuid}/cancel", invitationAPI.CancelDepartmentInvitation)
			r.With(authGuard).Patch("/companies/{uuid}/departments/{department_uuid}/members/{user_uuid}/role", departmentAPI.UpdateDepartmentMemberRole)
			r.With(authGuard).Patch("/companies/{uuid}/departments/{department_uuid}/members/{user_uuid}/status", departmentAPI.UpdateDepartmentMemberStatus)

			// ANALYSIS PERSONALIZATION
			r.With(authGuard).Get("/analysis-personalization", analysisContextAPI.Get)
			r.With(authGuard).Put("/analysis-personalization", analysisContextAPI.Save)

			//ANALYSIS INSTRUCTIONS
			r.With(authGuard).Post("/instructions", instructionAPI.Create)
			r.With(authGuard).Get("/instructions", instructionAPI.List)
			r.With(authGuard).Patch("/instructions/reorder", instructionAPI.Reorder)
			r.With(authGuard).Get("/instructions/{uuid}", instructionAPI.Get)
			r.With(authGuard).Patch("/instructions/{uuid}", instructionAPI.Update)
			r.With(authGuard).Put("/instructions/{uuid}/file", instructionAPI.ReplaceFile)
			r.With(authGuard).Get("/instructions/{uuid}/file", instructionAPI.GetFile)
			r.With(authGuard).Get("/instructions/{uuid}/download", instructionAPI.GetFile)
			r.With(authGuard).Get("/instructions/{uuid}/versions", instructionAPI.ListVersions)
			r.With(authGuard).Get("/instructions/{uuid}/versions/{version_uuid}/file", instructionAPI.GetVersionFile)
			r.With(authGuard).Delete("/instructions/{uuid}", instructionAPI.Delete)
		})
	})

	return r
}
