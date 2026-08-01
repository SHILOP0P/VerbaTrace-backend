package company

import (
	"net/http"
	"time"

	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
)

func (s *APISuite) TestUpdateCompanyMemberStatusSuccess() {
	companyID := uuid.New()
	requestUserID := uuid.New()
	userID := uuid.New()

	s.service.On("UpdateCompanyMemberStatus", mock.Anything, models.UpdateCompanyMemberStatusInput{
		CompanyUUID: companyID,
		RequestUser: requestUserID,
		UserUUID:    userID,
		Status:      models.MembershipStatusSuspended,
	}).
		Return(models.CompanyMember{CompanyUUID: companyID, UserUUID: userID, Status: models.MembershipStatusSuspended, CreatedAt: time.Now().UTC()}, nil).
		Once()

	rec, req := s.request(http.MethodPatch, "/api/v1/companies/"+companyID.String()+"/members/"+userID.String()+"/status", `{"status":"suspended"}`, requestUserID, map[string]string{
		"uuid":      companyID.String(),
		"user_uuid": userID.String(),
	})

	s.api.UpdateCompanyMemberStatus(rec, req)

	s.Require().Equal(http.StatusOK, rec.Code)
}

func (s *APISuite) TestUpdateCompanyMemberStatusRejectsInvalidBody() {
	companyID := uuid.New()
	userID := uuid.New()

	rec, req := s.request(http.MethodPatch, "/api/v1/companies/"+companyID.String()+"/members/"+userID.String()+"/status", `{`, uuid.New(), map[string]string{
		"uuid":      companyID.String(),
		"user_uuid": userID.String(),
	})

	s.api.UpdateCompanyMemberStatus(rec, req)

	s.Require().Equal(http.StatusBadRequest, rec.Code)
	s.requireErrorCode(rec, response.CodeInvalidRequestBody)
}

func (s *APISuite) TestUpdateCompanyMemberStatusRequiresAuth() {
	companyID := uuid.New()
	userID := uuid.New()

	rec, req := s.request(http.MethodPatch, "/api/v1/companies/"+companyID.String()+"/members/"+userID.String()+"/status", `{"status":"suspended"}`, uuid.Nil, map[string]string{
		"uuid":      companyID.String(),
		"user_uuid": userID.String(),
	})

	s.api.UpdateCompanyMemberStatus(rec, req)

	s.Require().Equal(http.StatusUnauthorized, rec.Code)
	s.requireErrorCode(rec, response.CodeUnauthorized)
}

func (s *APISuite) TestUpdateCompanyMemberStatusRejectsInvalidUserUUID() {
	companyID := uuid.New()

	rec, req := s.request(http.MethodPatch, "/api/v1/companies/"+companyID.String()+"/members/bad/status", `{"status":"suspended"}`, uuid.New(), map[string]string{
		"uuid":      companyID.String(),
		"user_uuid": "bad",
	})

	s.api.UpdateCompanyMemberStatus(rec, req)

	s.Require().Equal(http.StatusBadRequest, rec.Code)
	s.requireErrorCode(rec, response.CodeInvalidCompanyInput)
}

func (s *APISuite) TestUpdateCompanyMemberStatusMapsCompanyNotFound() {
	companyID := uuid.New()
	requestUserID := uuid.New()
	userID := uuid.New()

	s.service.On("UpdateCompanyMemberStatus", mock.Anything, mock.Anything).
		Return(models.CompanyMember{}, models.ErrCompanyNotFound).
		Once()

	rec, req := s.request(http.MethodPatch, "/api/v1/companies/"+companyID.String()+"/members/"+userID.String()+"/status", `{"status":"left"}`, requestUserID, map[string]string{
		"uuid":      companyID.String(),
		"user_uuid": userID.String(),
	})

	s.api.UpdateCompanyMemberStatus(rec, req)

	s.Require().Equal(http.StatusNotFound, rec.Code)
	s.requireErrorCode(rec, response.CodeCompanyNotFound)
}
