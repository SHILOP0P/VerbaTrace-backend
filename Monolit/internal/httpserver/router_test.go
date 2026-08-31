package httpserver

import (
	"net/http"
	"net/http/httptest"
	"testing"

	apiMocks "verbatrace/monolit/internal/API/mocks"
	"verbatrace/monolit/internal/logger"
	repositoryMocks "verbatrace/monolit/internal/repository/mocks"

	"github.com/stretchr/testify/require"
)

func TestNewRouterRegistersPublicAndProtectedRoutes(t *testing.T) {
	router := NewRouter(
		apiMocks.NewCallAPI(t),
		stubCallFolderAPI{},
		stubContactAPI{},
		apiMocks.NewAuthAPI(t),
		apiMocks.NewCompanyAPI(t),
		apiMocks.NewDepartmentAPI(t),
		apiMocks.NewAnalysisInstructionAPI(t),
		stubAnalysisContextAPI{},
		apiMocks.NewAnalysisAPI(t),
		stubQualityReviewAPI{},
		stubActionAPI{},
		apiMocks.NewReportAPI(t),
		apiMocks.NewBillingAPI(t),
		apiMocks.NewInvitationAPI(t),
		apiMocks.NewAnalyticsAPI(t),
		apiMocks.NewMonitoringAPI(t),
		stubSearchAPI{},
		stubNotificationAPI{},
		stubAdminAPI{},
		nil,
		nil,
		"test-secret",
		repositoryMocks.NewRefreshSessionRepository(t),
		logger.NewNop(),
	)

	healthRecorder := httptest.NewRecorder()
	router.ServeHTTP(healthRecorder, httptest.NewRequest(http.MethodGet, "/health", nil))
	require.Equal(t, http.StatusOK, healthRecorder.Code)

	readyRecorder := httptest.NewRecorder()
	router.ServeHTTP(readyRecorder, httptest.NewRequest(http.MethodGet, "/health/ready", nil))
	require.Equal(t, http.StatusOK, readyRecorder.Code)

	docsRecorder := httptest.NewRecorder()
	router.ServeHTTP(docsRecorder, httptest.NewRequest(http.MethodGet, "/docs/integrations", nil))
	require.Equal(t, http.StatusOK, docsRecorder.Code)

	openAPIRecorder := httptest.NewRecorder()
	router.ServeHTTP(openAPIRecorder, httptest.NewRequest(http.MethodGet, "/docs/integrations/openapi.yaml", nil))
	require.Equal(t, http.StatusOK, openAPIRecorder.Code)
	require.Contains(t, openAPIRecorder.Body.String(), "openapi: 3.1.0")

	protectedRecorder := httptest.NewRecorder()
	router.ServeHTTP(protectedRecorder, httptest.NewRequest(http.MethodGet, "/api/v1/calls", nil))
	require.Equal(t, http.StatusUnauthorized, protectedRecorder.Code)

	adminRecorder := httptest.NewRecorder()
	router.ServeHTTP(adminRecorder, httptest.NewRequest(http.MethodGet, "/api/v1/admin/capabilities", nil))
	require.Equal(t, http.StatusUnauthorized, adminRecorder.Code)

	reopenRecorder := httptest.NewRecorder()
	router.ServeHTTP(reopenRecorder, httptest.NewRequest(http.MethodPost, "/api/v1/admin/actions/00000000-0000-0000-0000-000000000001/reopen", nil))
	require.Equal(t, http.StatusUnauthorized, reopenRecorder.Code)

	notFoundRecorder := httptest.NewRecorder()
	router.ServeHTTP(notFoundRecorder, httptest.NewRequest(http.MethodGet, "/missing", nil))
	require.Equal(t, http.StatusNotFound, notFoundRecorder.Code)
}

type stubSearchAPI struct{}

func (stubSearchAPI) Search(w http.ResponseWriter, r *http.Request) {}

type stubAnalysisContextAPI struct{}

func (stubAnalysisContextAPI) Get(w http.ResponseWriter, r *http.Request)  {}
func (stubAnalysisContextAPI) Save(w http.ResponseWriter, r *http.Request) {}

type stubQualityReviewAPI struct{}

func (stubQualityReviewAPI) Create(http.ResponseWriter, *http.Request)                {}
func (stubQualityReviewAPI) GetAnalysisContext(http.ResponseWriter, *http.Request)    {}
func (stubQualityReviewAPI) ChallengeAnalysis(http.ResponseWriter, *http.Request)     {}
func (stubQualityReviewAPI) List(http.ResponseWriter, *http.Request)                  {}
func (stubQualityReviewAPI) Get(http.ResponseWriter, *http.Request)                   {}
func (stubQualityReviewAPI) Claim(http.ResponseWriter, *http.Request)                 {}
func (stubQualityReviewAPI) SaveDraft(http.ResponseWriter, *http.Request)             {}
func (stubQualityReviewAPI) DiscardDraft(http.ResponseWriter, *http.Request)          {}
func (stubQualityReviewAPI) Publish(http.ResponseWriter, *http.Request)               {}
func (stubQualityReviewAPI) CreateAppeal(http.ResponseWriter, *http.Request)          {}
func (stubQualityReviewAPI) ResolveAppeal(http.ResponseWriter, *http.Request)         {}
func (stubQualityReviewAPI) ListEvents(http.ResponseWriter, *http.Request)            {}
func (stubQualityReviewAPI) CreateAnalysisComment(http.ResponseWriter, *http.Request) {}
func (stubQualityReviewAPI) UpdateAnalysisComment(http.ResponseWriter, *http.Request) {}

type stubActionAPI struct{}

func (stubActionAPI) Create(http.ResponseWriter, *http.Request)             {}
func (stubActionAPI) SetDisposition(http.ResponseWriter, *http.Request)     {}
func (stubActionAPI) Get(http.ResponseWriter, *http.Request)                {}
func (stubActionAPI) GetAdmin(http.ResponseWriter, *http.Request)           {}
func (stubActionAPI) List(http.ResponseWriter, *http.Request)               {}
func (stubActionAPI) ListAdmin(http.ResponseWriter, *http.Request)          {}
func (stubActionAPI) ListAssignees(http.ResponseWriter, *http.Request)      {}
func (stubActionAPI) ListAssigneesAdmin(http.ResponseWriter, *http.Request) {}
func (stubActionAPI) Start(http.ResponseWriter, *http.Request)              {}
func (stubActionAPI) Complete(http.ResponseWriter, *http.Request)           {}
func (stubActionAPI) Cancel(http.ResponseWriter, *http.Request)             {}
func (stubActionAPI) Reschedule(http.ResponseWriter, *http.Request)         {}
func (stubActionAPI) Reassign(http.ResponseWriter, *http.Request)           {}
func (stubActionAPI) Reopen(http.ResponseWriter, *http.Request)             {}
func (stubActionAPI) CreateTransfer(http.ResponseWriter, *http.Request)     {}
func (stubActionAPI) ApproveTransfer(http.ResponseWriter, *http.Request)    {}
func (stubActionAPI) RejectTransfer(http.ResponseWriter, *http.Request)     {}
func (stubActionAPI) CompleteAdmin(http.ResponseWriter, *http.Request)      {}
func (stubActionAPI) CancelAdmin(http.ResponseWriter, *http.Request)        {}
func (stubActionAPI) RescheduleAdmin(http.ResponseWriter, *http.Request)    {}
func (stubActionAPI) ReassignAdmin(http.ResponseWriter, *http.Request)      {}
func (stubActionAPI) ReopenAdmin(http.ResponseWriter, *http.Request)        {}

type stubCallFolderAPI struct{}

func (stubCallFolderAPI) Create(w http.ResponseWriter, r *http.Request)              {}
func (stubCallFolderAPI) List(w http.ResponseWriter, r *http.Request)                {}
func (stubCallFolderAPI) Get(w http.ResponseWriter, r *http.Request)                 {}
func (stubCallFolderAPI) Update(w http.ResponseWriter, r *http.Request)              {}
func (stubCallFolderAPI) Delete(w http.ResponseWriter, r *http.Request)              {}
func (stubCallFolderAPI) ListCalls(w http.ResponseWriter, r *http.Request)           {}
func (stubCallFolderAPI) AssignCall(w http.ResponseWriter, r *http.Request)          {}
func (stubCallFolderAPI) RemoveCall(w http.ResponseWriter, r *http.Request)          {}
func (stubCallFolderAPI) GrantAccess(w http.ResponseWriter, r *http.Request)         {}
func (stubCallFolderAPI) RevokeAccess(w http.ResponseWriter, r *http.Request)        {}
func (stubCallFolderAPI) ListAccesses(w http.ResponseWriter, r *http.Request)        {}
func (stubCallFolderAPI) ReplaceInstructions(w http.ResponseWriter, r *http.Request) {}

type stubContactAPI struct{}

func (stubContactAPI) SearchContacts(http.ResponseWriter, *http.Request)     {}
func (stubContactAPI) ListContacts(http.ResponseWriter, *http.Request)       {}
func (stubContactAPI) AddContact(http.ResponseWriter, *http.Request)         {}
func (stubContactAPI) RemoveContact(http.ResponseWriter, *http.Request)      {}
func (stubContactAPI) ListFavoriteCalls(http.ResponseWriter, *http.Request)  {}
func (stubContactAPI) AddFavoriteCall(http.ResponseWriter, *http.Request)    {}
func (stubContactAPI) RemoveFavoriteCall(http.ResponseWriter, *http.Request) {}

type stubNotificationAPI struct{}

func (stubNotificationAPI) List(w http.ResponseWriter, r *http.Request)        {}
func (stubNotificationAPI) Events(w http.ResponseWriter, r *http.Request)      {}
func (stubNotificationAPI) MarkRead(w http.ResponseWriter, r *http.Request)    {}
func (stubNotificationAPI) MarkUnread(w http.ResponseWriter, r *http.Request)  {}
func (stubNotificationAPI) MarkAllRead(w http.ResponseWriter, r *http.Request) {}

type stubAdminAPI struct{}

func (stubAdminAPI) ResetPersonalUsage(http.ResponseWriter, *http.Request) {}
func (stubAdminAPI) ResetCompanyUsage(http.ResponseWriter, *http.Request)  {}

func (stubAdminAPI) GetCapabilities(w http.ResponseWriter, r *http.Request)            {}
func (stubAdminAPI) ListUsers(w http.ResponseWriter, r *http.Request)                  {}
func (stubAdminAPI) GetUser(w http.ResponseWriter, r *http.Request)                    {}
func (stubAdminAPI) ListUserCalls(w http.ResponseWriter, r *http.Request)              {}
func (stubAdminAPI) UpdateUserProfile(w http.ResponseWriter, r *http.Request)          {}
func (stubAdminAPI) ChangeUserRole(w http.ResponseWriter, r *http.Request)             {}
func (stubAdminAPI) ListUserSessions(w http.ResponseWriter, r *http.Request)           {}
func (stubAdminAPI) RevokeUserSession(w http.ResponseWriter, r *http.Request)          {}
func (stubAdminAPI) RevokeAllUserSessions(w http.ResponseWriter, r *http.Request)      {}
func (stubAdminAPI) ListCompanies(w http.ResponseWriter, r *http.Request)              {}
func (stubAdminAPI) GetCompany(w http.ResponseWriter, r *http.Request)                 {}
func (stubAdminAPI) GetPersonalSubscription(w http.ResponseWriter, r *http.Request)    {}
func (stubAdminAPI) GetCompanySubscription(w http.ResponseWriter, r *http.Request)     {}
func (stubAdminAPI) GrantPersonalSubscription(w http.ResponseWriter, r *http.Request)  {}
func (stubAdminAPI) GrantCompanySubscription(w http.ResponseWriter, r *http.Request)   {}
func (stubAdminAPI) CancelPersonalSubscription(w http.ResponseWriter, r *http.Request) {}
func (stubAdminAPI) CancelCompanySubscription(w http.ResponseWriter, r *http.Request)  {}
func (stubAdminAPI) GetCall(w http.ResponseWriter, r *http.Request)                    {}
func (stubAdminAPI) GetCallAudio(w http.ResponseWriter, r *http.Request)               {}
