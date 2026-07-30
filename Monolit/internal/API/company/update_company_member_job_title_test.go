package company

import (
	"net/http"

	"calllens/monolit/internal/models"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
)

func (s *APISuite) TestUpdateCompanyMemberJobTitle() {
	companyID := uuid.New()
	userID := uuid.New()
	requestUserID := uuid.New()
	title := "Backend developer"

	s.service.EXPECT().
		UpdateCompanyMemberJobTitle(mock.Anything, models.UpdateCompanyMemberJobTitleInput{
			CompanyUUID: companyID,
			RequestUser: requestUserID,
			UserUUID:    userID,
			JobTitle:    &title,
		}).
		Return(models.CompanyMember{
			CompanyUUID: companyID,
			UserUUID:    userID,
			Username:    "@developer",
			FullName:    "Ivan",
			FullSurname: "Petrov",
			JobTitle:    &title,
			Role:        models.CompanyMemberRoleEmployee,
			Status:      models.MembershipStatusActive,
		}, nil).
		Once()

	rec, req := s.request(
		http.MethodPatch,
		"/api/v1/companies/"+companyID.String()+"/members/"+userID.String()+"/job-title",
		`{"job_title":"Backend developer"}`,
		requestUserID,
		map[string]string{"uuid": companyID.String(), "user_uuid": userID.String()},
	)
	s.api.UpdateCompanyMemberJobTitle(rec, req)

	s.Require().Equal(http.StatusOK, rec.Code)
	s.Require().Contains(rec.Body.String(), `"job_title":"Backend developer"`)
}
