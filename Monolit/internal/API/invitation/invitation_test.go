package invitation

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/httpserver/middleware"
	"verbatrace/monolit/internal/models"
	serviceMocks "verbatrace/monolit/internal/service/mocks"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestCreateCompanyInvitationSuccess(t *testing.T) {
	companyID := uuid.New()
	requestUserID := uuid.New()
	userID := uuid.New()
	service := serviceMocks.NewInvitationService(t)
	service.EXPECT().CreateCompanyInvitation(mock.Anything, models.CreateCompanyInvitationInput{
		CompanyUUID: companyID, RequestUser: requestUserID, UserUUID: userID,
		Role: models.CompanyMemberRoleEmployee,
	}).Return(testAPIInvitation(companyID, userID, requestUserID), nil).Once()
	api := NewHandler(service)

	rec, req := request(http.MethodPost, "/api/v1/companies/"+companyID.String()+"/invitations", `{"user_uuid":"`+userID.String()+`","role":"employee"}`, requestUserID, map[string]string{"uuid": companyID.String()})
	api.CreateCompanyInvitation(rec, req)

	require.Equal(t, http.StatusCreated, rec.Code)
}

func TestCreateDepartmentInvitationSuccess(t *testing.T) {
	companyID := uuid.New()
	departmentID := uuid.New()
	requestUserID := uuid.New()
	userID := uuid.New()
	service := serviceMocks.NewInvitationService(t)
	role := models.DepartmentMemberRoleEmployee
	invitation := testAPIInvitation(companyID, userID, requestUserID)
	invitation.DepartmentUUID = uuid.NullUUID{UUID: departmentID, Valid: true}
	invitation.DepartmentRole = &role
	service.EXPECT().CreateDepartmentInvitation(mock.Anything, models.CreateDepartmentInvitationInput{
		CompanyUUID: companyID, DepartmentUUID: departmentID, RequestUser: requestUserID,
		UserUUID: userID, Role: models.DepartmentMemberRoleEmployee,
	}).Return(invitation, nil).Once()
	api := NewHandler(service)

	rec, req := request(http.MethodPost, "/api/v1/companies/"+companyID.String()+"/departments/"+departmentID.String()+"/invitations", `{"user_uuid":"`+userID.String()+`","role":"employee"}`, requestUserID, map[string]string{"uuid": companyID.String(), "department_uuid": departmentID.String()})
	api.CreateDepartmentInvitation(rec, req)

	require.Equal(t, http.StatusCreated, rec.Code)
}

func TestListAcceptDeclineAndErrors(t *testing.T) {
	companyID := uuid.New()
	userID := uuid.New()
	invitationID := uuid.New()
	service := serviceMocks.NewInvitationService(t)
	service.EXPECT().ListUserInvitations(mock.Anything, models.ListUserInvitationsInput{
		UserUUID: userID,
	}).Return([]models.MembershipInvitation{testAPIInvitation(companyID, userID, uuid.New())}, nil).Once()
	service.EXPECT().AcceptInvitation(mock.Anything, models.AcceptInvitationInput{
		InvitationUUID: invitationID, RequestUser: userID,
	}).Return(testAPIInvitation(companyID, userID, uuid.New()), nil).Once()
	service.EXPECT().DeclineInvitation(mock.Anything, models.DeclineInvitationInput{
		InvitationUUID: invitationID, RequestUser: userID,
	}).Return(testAPIInvitation(companyID, userID, uuid.New()), nil).Once()
	api := NewHandler(service)

	rec, req := request(http.MethodGet, "/api/v1/invitations", "", userID, nil)
	api.ListUserInvitations(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	rec, req = request(http.MethodPost, "/api/v1/invitations/"+invitationID.String()+"/accept", "", userID, map[string]string{"invitation_uuid": invitationID.String()})
	api.AcceptInvitation(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	rec, req = request(http.MethodPost, "/api/v1/invitations/"+invitationID.String()+"/decline", "", userID, map[string]string{"invitation_uuid": invitationID.String()})
	api.DeclineInvitation(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	service.EXPECT().AcceptInvitation(mock.Anything, mock.Anything).
		Return(models.MembershipInvitation{}, models.ErrForbidden).Once()
	rec, req = request(http.MethodPost, "/api/v1/invitations/"+invitationID.String()+"/accept", "", userID, map[string]string{"invitation_uuid": invitationID.String()})
	api.AcceptInvitation(rec, req)
	require.Equal(t, http.StatusForbidden, rec.Code)
	requireErrorCode(t, rec, response.CodeForbidden)

	service.EXPECT().CreateCompanyInvitation(mock.Anything, mock.Anything).
		Return(models.MembershipInvitation{}, models.ErrInvitationAlreadyExists).Once()
	rec, req = request(http.MethodPost, "/api/v1/companies/"+companyID.String()+"/invitations", `{"user_uuid":"`+userID.String()+`","role":"employee"}`, userID, map[string]string{"uuid": companyID.String()})
	api.CreateCompanyInvitation(rec, req)
	require.Equal(t, http.StatusConflict, rec.Code)
	requireErrorCode(t, rec, response.CodeInvitationAlreadyExists)
}

func testAPIInvitation(companyID uuid.UUID, invitedUserID uuid.UUID, invitedByUserID uuid.UUID) models.MembershipInvitation {
	now := time.Now().UTC()
	return models.MembershipInvitation{
		ID:                uuid.New(),
		CompanyUUID:       companyID,
		InvitedUserUUID:   invitedUserID,
		InvitedByUserUUID: invitedByUserID,
		CompanyRole:       models.CompanyMemberRoleEmployee,
		Status:            models.InvitationStatusPending,
		ExpiresAt:         now.Add(time.Hour),
		CreatedAt:         now,
		UpdatedAt:         now,
	}
}

func request(method string, path string, body string, userID uuid.UUID, params map[string]string) (*httptest.ResponseRecorder, *http.Request) {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if userID != uuid.Nil {
		req = req.WithContext(middleware.ContextWithUserID(req.Context(), userID))
	}
	if len(params) > 0 {
		routeCtx := chi.NewRouteContext()
		for key, value := range params {
			routeCtx.URLParams.Add(key, value)
		}
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx))
	}
	return httptest.NewRecorder(), req
}

func requireErrorCode(t *testing.T, rec *httptest.ResponseRecorder, expectedCode string) {
	var resp response.ErrorResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, expectedCode, resp.Error.Code)
}
