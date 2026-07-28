package httpserver

import (
	"net/http"
	"time"

	"calllens/monolit/internal/API"
	"calllens/monolit/internal/API/health"
	authMiddleware "calllens/monolit/internal/httpserver/middleware"
	"calllens/monolit/internal/logger"
	"calllens/monolit/internal/models"
	"calllens/monolit/internal/repository"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func NewRouter(callAPI API.CallAPI, callFolderAPI API.CallFolderAPI, contactAPI API.ContactAPI, authAPI API.AuthAPI, companyAPI API.CompanyAPI, departmentAPI API.DepartmentAPI, instructionAPI API.AnalysisInstructionAPI, analysisContextAPI API.AnalysisContextAPI, analysisAPI API.AnalysisAPI, reportAPI API.ReportAPI, billingAPI API.BillingAPI, invitationAPI API.InvitationAPI, analyticsAPI API.AnalyticsAPI, monitoringAPI API.MonitoringAPI, searchAPI API.SearchAPI, notificationAPI API.NotificationAPI, adminAPI API.AdminAPI, healthHandler *health.Handler, jwtSecret string, refreshSessionRepository repository.RefreshSessionRepository, log logger.Logger) http.Handler {
	r := chi.NewRouter()

	authGuard := authMiddleware.Auth(jwtSecret, refreshSessionRepository)
	if healthHandler == nil {
		healthHandler = health.NewHandler()
	}

	r.Use(middleware.RequestID)
	r.Use(authMiddleware.RequestLogger(log))
	r.Use(authMiddleware.Recoverer(log))
	r.Use(middleware.URLFormat)

	r.Get("/health", healthHandler.Health)
	r.Get("/health/live", healthHandler.Live)
	r.Get("/health/ready", healthHandler.Ready)
	r.Get("/health/startup", healthHandler.Startup)
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
				r.With(authMiddleware.RequirePermission(models.AdminPermissionCallsRead)).Get("/calls/{call_uuid}", adminAPI.GetCall)
				r.With(authMiddleware.RequirePermission(models.AdminPermissionCallsRead)).Get("/calls/{call_uuid}/audio", adminAPI.GetCallAudio)
				r.With(authMiddleware.RequirePermission(models.AdminPermissionCallsRead)).Get("/calls/{call_uuid}/media", adminAPI.GetCallAudio)
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
			r.With(authGuard).Post("/calls/{uuid}/analysis", analysisAPI.AnalyzeCall)
			r.With(authGuard).Get("/calls/{uuid}/analysis", analysisAPI.GetByCallUUID)
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
			r.With(authGuard).Get("/search", searchAPI.Search)

			//NOTIFICATIONS
			r.With(authGuard).Get("/notifications", notificationAPI.List)
			r.With(authGuard).Post("/notifications/{uuid}/read", notificationAPI.MarkRead)
			r.With(authGuard).Post("/notifications/read-all", notificationAPI.MarkAllRead)

			//BILLING
			r.Get("/plans", billingAPI.ListPlans)
			r.With(authGuard).Get("/subscription", billingAPI.GetPersonalSubscription)
			r.With(authGuard).Get("/subscription/usage", billingAPI.GetPersonalSubscriptionUsage)
			r.With(authGuard).Get("/companies/{uuid}/subscription", billingAPI.GetCompanySubscription)
			r.With(authGuard).Get("/companies/{uuid}/subscription/usage", billingAPI.GetCompanySubscriptionUsage)

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
			r.With(authGuard).Delete("/instructions/{uuid}", instructionAPI.Delete)
		})
	})

	return r
}
