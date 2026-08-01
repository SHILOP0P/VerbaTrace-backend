package department

import (
	"errors"
	"net/http"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
)

func (s *APISuite) TestCreateDepartmentAdditionalErrors() {
	companyID := uuid.New()
	userID := uuid.New()
	for _, tt := range []struct {
		err  error
		code int
	}{
		{models.ErrInvalidDepartmentInput, http.StatusBadRequest},
		{models.ErrCompanyNotFound, http.StatusNotFound},
		{models.ErrSubscriptionRequired, http.StatusPaymentRequired},
		{models.ErrDepartmentLimitExceeded, http.StatusBadRequest},
		{errors.New("db"), http.StatusInternalServerError},
	} {
		s.service.EXPECT().CreateDepartment(mock.Anything, mock.Anything).
			Return(models.Department{}, tt.err).Once()
		rec, req := s.request(http.MethodPost, "/", `{"name":"Sales"}`, userID, map[string]string{"uuid": companyID.String()})
		s.api.CreateDepartment(rec, req)
		s.Equal(tt.code, rec.Code)
	}

	rec, req := s.request(http.MethodPost, "/", `{`, userID, map[string]string{"uuid": companyID.String()})
	s.api.CreateDepartment(rec, req)
	s.Equal(http.StatusBadRequest, rec.Code)
}

func (s *APISuite) TestListDepartmentMembersAdditionalPaths() {
	companyID := uuid.New()
	departmentID := uuid.New()
	userID := uuid.New()
	s.service.EXPECT().ListDepartmentMembers(mock.Anything, companyID, departmentID, userID).
		Return([]models.DepartmentMember{{DepartmentUUID: departmentID, UserUUID: userID}}, nil).Once()
	rec, req := s.request(http.MethodGet, "/", "", userID, map[string]string{
		"uuid": companyID.String(), "department_uuid": departmentID.String(),
	})
	s.api.ListDepartmentMembers(rec, req)
	s.Equal(http.StatusOK, rec.Code)

	for _, tt := range []struct {
		err  error
		code int
	}{
		{models.ErrInvalidDepartmentInput, http.StatusBadRequest},
		{models.ErrCompanyNotFound, http.StatusNotFound},
		{models.ErrDepartmentNotFound, http.StatusNotFound},
		{models.ErrForbidden, http.StatusForbidden},
		{models.ErrSubscriptionRequired, http.StatusPaymentRequired},
		{errors.New("db"), http.StatusInternalServerError},
	} {
		s.service.EXPECT().ListDepartmentMembers(mock.Anything, companyID, departmentID, userID).
			Return(nil, tt.err).Once()
		rec, req = s.request(http.MethodGet, "/", "", userID, map[string]string{
			"uuid": companyID.String(), "department_uuid": departmentID.String(),
		})
		s.api.ListDepartmentMembers(rec, req)
		s.Equal(tt.code, rec.Code)
	}
}

func (s *APISuite) TestListDepartmentsAdditionalPaths() {
	companyID := uuid.New()
	userID := uuid.New()
	s.service.EXPECT().ListCompanyDepartments(mock.Anything, companyID, userID).
		Return([]models.Department{{ID: uuid.New(), CompanyUUID: companyID}}, nil).Once()
	rec, req := s.request(http.MethodGet, "/", "", userID, map[string]string{"uuid": companyID.String()})
	s.api.ListDepartments(rec, req)
	s.Equal(http.StatusOK, rec.Code)

	s.service.EXPECT().ListCompanyDepartments(mock.Anything, companyID, userID).
		Return(nil, models.ErrSubscriptionRequired).Once()
	rec, req = s.request(http.MethodGet, "/", "", userID, map[string]string{"uuid": companyID.String()})
	s.api.ListDepartments(rec, req)
	s.Equal(http.StatusPaymentRequired, rec.Code)
}
