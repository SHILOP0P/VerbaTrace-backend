package department

import (
	"net/http"
	"time"

	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
)

func (s *APISuite) TestCreateDepartmentSuccess() {
	companyID := uuid.New()
	departmentID := uuid.New()
	userID := uuid.New()

	s.service.On("CreateDepartment", mock.Anything, models.CreateDepartmentInput{
		CompanyUUID: companyID,
		UserID:      userID,
		Name:        "Sales",
	}).
		Return(models.Department{ID: departmentID, CompanyUUID: companyID, Name: "Sales", CreatedAt: time.Now().UTC()}, nil).
		Once()

	rec, req := s.request(http.MethodPost, "/api/v1/companies/"+companyID.String()+"/departments", `{"name":"Sales"}`, userID, map[string]string{
		"uuid": companyID.String(),
	})

	s.api.CreateDepartment(rec, req)

	s.Require().Equal(http.StatusCreated, rec.Code)
}

func (s *APISuite) TestCreateDepartmentRequiresAuth() {
	companyID := uuid.New()
	rec, req := s.request(http.MethodPost, "/api/v1/companies/"+companyID.String()+"/departments", `{"name":"Sales"}`, uuid.Nil, map[string]string{
		"uuid": companyID.String(),
	})

	s.api.CreateDepartment(rec, req)

	s.Require().Equal(http.StatusUnauthorized, rec.Code)
	s.requireErrorCode(rec, response.CodeUnauthorized)
}

func (s *APISuite) TestCreateDepartmentRejectsInvalidCompanyUUID() {
	rec, req := s.request(http.MethodPost, "/api/v1/companies/bad/departments", `{"name":"Sales"}`, uuid.New(), map[string]string{
		"uuid": "bad",
	})

	s.api.CreateDepartment(rec, req)

	s.Require().Equal(http.StatusBadRequest, rec.Code)
	s.requireErrorCode(rec, response.CodeInvalidCompanyInput)
}

func (s *APISuite) TestCreateDepartmentMapsForbidden() {
	companyID := uuid.New()
	userID := uuid.New()

	s.service.On("CreateDepartment", mock.Anything, models.CreateDepartmentInput{
		CompanyUUID: companyID,
		UserID:      userID,
		Name:        "Sales",
	}).
		Return(models.Department{}, models.ErrForbidden).
		Once()

	rec, req := s.request(http.MethodPost, "/api/v1/companies/"+companyID.String()+"/departments", `{"name":"Sales"}`, userID, map[string]string{
		"uuid": companyID.String(),
	})

	s.api.CreateDepartment(rec, req)

	s.Require().Equal(http.StatusForbidden, rec.Code)
	s.requireErrorCode(rec, response.CodeForbidden)
}
