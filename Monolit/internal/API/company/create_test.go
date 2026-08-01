package company

import (
	"net/http"
	"time"

	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
)

func (s *APISuite) TestCreateSuccess() {
	userID := uuid.New()
	companyID := uuid.New()

	s.service.On("CreateCompany", mock.Anything, models.CreateCompanyInput{
		Name:          "VerbaTrace",
		ManagerUserID: userID,
	}).
		Return(models.Company{ID: companyID, Name: "VerbaTrace", ManagerUserUUID: userID, MemberLimit: 1, CreatedAt: time.Now().UTC()}, nil).
		Once()

	rec, req := s.request(http.MethodPost, "/api/v1/companies", `{"name":"VerbaTrace"}`, userID, nil)

	s.api.Create(rec, req)

	s.Require().Equal(http.StatusCreated, rec.Code)
}

func (s *APISuite) TestCreateRequiresAuth() {
	rec, req := s.request(http.MethodPost, "/api/v1/companies", `{"name":"VerbaTrace"}`, uuid.Nil, nil)

	s.api.Create(rec, req)

	s.Require().Equal(http.StatusUnauthorized, rec.Code)
	s.requireErrorCode(rec, response.CodeUnauthorized)
}

func (s *APISuite) TestCreateRejectsInvalidBody() {
	rec, req := s.request(http.MethodPost, "/api/v1/companies", `{`, uuid.New(), nil)

	s.api.Create(rec, req)

	s.Require().Equal(http.StatusBadRequest, rec.Code)
	s.requireErrorCode(rec, response.CodeInvalidRequestBody)
}

func (s *APISuite) TestCreateMapsAlreadyManagedCompany() {
	userID := uuid.New()

	s.service.On("CreateCompany", mock.Anything, models.CreateCompanyInput{
		Name:          "VerbaTrace",
		ManagerUserID: userID,
	}).
		Return(models.Company{}, models.ErrUserAlreadyManagesCompany).
		Once()

	rec, req := s.request(http.MethodPost, "/api/v1/companies", `{"name":"VerbaTrace"}`, userID, nil)

	s.api.Create(rec, req)

	s.Require().Equal(http.StatusConflict, rec.Code)
	s.requireErrorCode(rec, response.CodeUserAlreadyManagesCompany)
}
