package department

import (
	"errors"
	"net/http"
	"time"

	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
)

func (s *APISuite) TestListDepartmentsSuccess() {
	companyID := uuid.New()
	userID := uuid.New()

	s.service.On("ListCompanyDepartments", mock.Anything, companyID, userID).
		Return([]models.Department{{ID: uuid.New(), CompanyUUID: companyID, Name: "Sales", CreatedAt: time.Now().UTC()}}, nil).
		Once()

	rec, req := s.request(http.MethodGet, "/api/v1/companies/"+companyID.String()+"/departments", "", userID, map[string]string{
		"uuid": companyID.String(),
	})

	s.api.ListDepartments(rec, req)

	s.Require().Equal(http.StatusOK, rec.Code)
}

func (s *APISuite) TestListDepartmentsRejectsInvalidCompanyUUID() {
	rec, req := s.request(http.MethodGet, "/api/v1/companies/bad/departments", "", uuid.New(), map[string]string{
		"uuid": "bad",
	})

	s.api.ListDepartments(rec, req)

	s.Require().Equal(http.StatusBadRequest, rec.Code)
	s.requireErrorCode(rec, response.CodeInvalidCompanyInput)
}

func (s *APISuite) TestListDepartmentsMapsServiceError() {
	companyID := uuid.New()
	userID := uuid.New()

	s.service.On("ListCompanyDepartments", mock.Anything, companyID, userID).
		Return(nil, errors.New("list failed")).
		Once()

	rec, req := s.request(http.MethodGet, "/api/v1/companies/"+companyID.String()+"/departments", "", userID, map[string]string{
		"uuid": companyID.String(),
	})

	s.api.ListDepartments(rec, req)

	s.Require().Equal(http.StatusInternalServerError, rec.Code)
	s.requireErrorCode(rec, response.CodeFailedToListDepartments)
}
