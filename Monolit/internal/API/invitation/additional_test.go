package invitation

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/models"
	serviceMocks "verbatrace/monolit/internal/service/mocks"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
)

func TestCancelInvitationsWithMockery(t *testing.T) {
	companyID := uuid.New()
	departmentID := uuid.New()
	invitationID := uuid.New()
	userID := uuid.New()
	service := serviceMocks.NewInvitationService(t)
	handler := NewHandler(service)

	service.EXPECT().CancelInvitation(mock.Anything, models.CancelInvitationInput{
		CompanyUUID: companyID, InvitationUUID: invitationID, RequestUser: userID,
	}).Return(testAPIInvitation(companyID, userID, userID), nil).Once()
	rec, req := request(http.MethodDelete, "/", "", userID, map[string]string{
		"uuid": companyID.String(), "invitation_uuid": invitationID.String(),
	})
	handler.CancelCompanyInvitation(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("company cancel status = %d", rec.Code)
	}

	service.EXPECT().CancelInvitation(mock.Anything, models.CancelInvitationInput{
		CompanyUUID: companyID, DepartmentUUID: uuid.NullUUID{UUID: departmentID, Valid: true},
		InvitationUUID: invitationID, RequestUser: userID,
	}).Return(testAPIInvitation(companyID, userID, userID), nil).Once()
	rec, req = request(http.MethodDelete, "/", "", userID, map[string]string{
		"uuid": companyID.String(), "department_uuid": departmentID.String(), "invitation_uuid": invitationID.String(),
	})
	handler.CancelDepartmentInvitation(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("department cancel status = %d", rec.Code)
	}
}

func TestInvitationValidationAndMappings(t *testing.T) {
	handler := NewHandler(serviceMocks.NewInvitationService(t))
	for _, method := range []func(http.ResponseWriter, *http.Request){
		handler.CreateCompanyInvitation,
		handler.CreateDepartmentInvitation,
		handler.ListUserInvitations,
		handler.AcceptInvitation,
		handler.DeclineInvitation,
		handler.CancelCompanyInvitation,
		handler.CancelDepartmentInvitation,
	} {
		rec, req := request(http.MethodPost, "/", "", uuid.Nil, nil)
		method(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("unauthorized status = %d", rec.Code)
		}
	}

	userID := uuid.New()
	for _, method := range []func(http.ResponseWriter, *http.Request){
		handler.CreateCompanyInvitation, handler.CancelCompanyInvitation,
	} {
		rec, req := request(http.MethodPost, "/", `{}`, userID, map[string]string{"uuid": "bad"})
		method(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("invalid company status = %d", rec.Code)
		}
	}
	rec := httptest.NewRecorder()
	if _, ok := parseOptionalUserUUID(rec, "bad"); ok || rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid optional UUID: ok=%v status=%d", ok, rec.Code)
	}
	if id, ok := parseOptionalUserUUID(httptest.NewRecorder(), ""); !ok || id != uuid.Nil {
		t.Fatalf("empty optional UUID = %v, %v", id, ok)
	}

	for _, method := range []func(http.ResponseWriter, *http.Request){
		handler.AcceptInvitation, handler.DeclineInvitation,
	} {
		rec, req := request(http.MethodPost, "/", "", userID, map[string]string{"invitation_uuid": "bad"})
		method(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("invalid invitation UUID status = %d", rec.Code)
		}
	}

	for _, tt := range []struct {
		err  error
		code int
	}{
		{models.ErrInvalidInvitationInput, http.StatusBadRequest},
		{models.ErrInvalidCompanyInput, http.StatusBadRequest},
		{models.ErrInvalidDepartmentInput, http.StatusBadRequest},
		{models.ErrInvitationAlreadyExists, http.StatusConflict},
		{models.ErrInvitationNotPending, http.StatusConflict},
		{models.ErrInvitationExpired, http.StatusConflict},
		{models.ErrInvitationNotFound, http.StatusNotFound},
		{models.ErrCompanyNotFound, http.StatusNotFound},
		{models.ErrDepartmentNotFound, http.StatusNotFound},
		{models.ErrUserNotFound, http.StatusNotFound},
		{models.ErrForbidden, http.StatusForbidden},
		{models.ErrSubscriptionRequired, http.StatusPaymentRequired},
		{models.ErrMemberLimitExceeded, http.StatusBadRequest},
		{errors.New("db"), http.StatusInternalServerError},
	} {
		rec := httptest.NewRecorder()
		writeInvitationError(rec, tt.err, response.CodeInternalServerError, "failed")
		if rec.Code != tt.code {
			t.Fatalf("error %v: status=%d want=%d", tt.err, rec.Code, tt.code)
		}
	}
}
