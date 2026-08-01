package department

import (
	"net/http"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
)

func (s *APISuite) TestUpdateDepartmentSuccess() {
	companyID := uuid.New()
	departmentID := uuid.New()
	userID := uuid.New()

	s.service.EXPECT().UpdateDepartment(mock.Anything, models.UpdateDepartmentInput{
		CompanyUUID:    companyID,
		DepartmentUUID: departmentID,
		RequestUser:    userID,
		Name:           "Sales",
	}).Return(models.Department{ID: departmentID, CompanyUUID: companyID, Name: "Sales"}, nil).Once()

	rec, req := s.request(http.MethodPatch, "/", `{"name":"Sales"}`, userID, map[string]string{
		"uuid":            companyID.String(),
		"department_uuid": departmentID.String(),
	})
	s.api.UpdateDepartment(rec, req)

	s.Require().Equal(http.StatusOK, rec.Code)
}

func (s *APISuite) TestDeleteDepartmentSuccess() {
	companyID := uuid.New()
	departmentID := uuid.New()
	userID := uuid.New()

	s.service.EXPECT().DeleteDepartment(mock.Anything, models.DeleteDepartmentInput{
		CompanyUUID:    companyID,
		DepartmentUUID: departmentID,
		RequestUser:    userID,
	}).Return(nil).Once()

	rec, req := s.request(http.MethodDelete, "/", "", userID, map[string]string{
		"uuid":            companyID.String(),
		"department_uuid": departmentID.String(),
	})
	s.api.DeleteDepartment(rec, req)

	s.Require().Equal(http.StatusNoContent, rec.Code)
}
