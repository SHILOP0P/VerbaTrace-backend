package company

import (
	"net/http"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
)

func (s *APISuite) TestUpdateCompanySuccess() {
	companyID := uuid.New()
	userID := uuid.New()

	s.service.EXPECT().UpdateCompany(mock.Anything, models.UpdateCompanyInput{
		CompanyUUID: companyID,
		RequestUser: userID,
		Name:        "New",
	}).Return(models.Company{ID: companyID, Name: "New"}, nil).Once()

	rec, req := s.request(http.MethodPatch, "/", `{"name":"New"}`, userID, map[string]string{"uuid": companyID.String()})
	s.api.Update(rec, req)

	s.Require().Equal(http.StatusOK, rec.Code)
}

func (s *APISuite) TestUpdateCompanyTagSuccess() {
	companyID := uuid.New()
	userID := uuid.New()
	s.service.On("UpdateCompanyTag", mock.Anything, models.UpdateCompanyTagInput{CompanyUUID: companyID, RequestUser: userID, Tag: "@verbatrace_team"}).
		Return(models.Company{ID: companyID, Tag: "@verbatrace_team"}, nil).Once()

	rec, req := s.request(http.MethodPatch, "/", `{"tag":"@verbatrace_team"}`, userID, map[string]string{"uuid": companyID.String()})
	s.api.UpdateTag(rec, req)
	s.Require().Equal(http.StatusOK, rec.Code)
}

func (s *APISuite) TestUpdateCompanyTagAsAdminSuccess() {
	companyID := uuid.New()
	s.service.On("UpdateCompanyTagAsAdmin", mock.Anything, companyID, "@verbatrace_team").
		Return(models.Company{ID: companyID, Tag: "@verbatrace_team"}, nil).Once()

	rec, req := s.request(http.MethodPatch, "/", `{"tag":"@verbatrace_team"}`, uuid.Nil, map[string]string{"uuid": companyID.String()})
	s.api.UpdateTagAsAdmin(rec, req)

	s.Require().Equal(http.StatusOK, rec.Code)
}

func (s *APISuite) TestDeleteCompanySuccess() {
	companyID := uuid.New()
	userID := uuid.New()

	s.service.EXPECT().DeleteCompany(mock.Anything, models.DeleteCompanyInput{
		CompanyUUID: companyID,
		RequestUser: userID,
	}).Return(nil).Once()

	rec, req := s.request(http.MethodDelete, "/", "", userID, map[string]string{"uuid": companyID.String()})
	s.api.Delete(rec, req)

	s.Require().Equal(http.StatusNoContent, rec.Code)
}
