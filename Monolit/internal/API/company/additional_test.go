package company

import (
	"errors"
	"net/http"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
)

func (s *APISuite) TestCreateAdditionalErrors() {
	userID := uuid.New()
	for _, tt := range []struct {
		err  error
		code int
	}{
		{models.ErrInvalidCompanyInput, http.StatusBadRequest},
		{models.ErrSubscriptionRequired, http.StatusPaymentRequired},
		{models.ErrCompanyLimitExceeded, http.StatusBadRequest},
		{models.ErrPlanLimitExceeded, http.StatusBadRequest},
		{errors.New("db"), http.StatusInternalServerError},
	} {
		s.service.EXPECT().CreateCompany(mock.Anything, mock.Anything).
			Return(models.Company{}, tt.err).Once()
		rec, req := s.request(http.MethodPost, "/", `{"name":"VerbaTrace"}`, userID, nil)
		s.api.Create(rec, req)
		s.Equal(tt.code, rec.Code)
	}
}

func (s *APISuite) TestOverviewAdditionalErrors() {
	companyID := uuid.New()
	userID := uuid.New()
	for _, tt := range []struct {
		err  error
		code int
	}{
		{models.ErrInvalidCompanyInput, http.StatusBadRequest},
		{models.ErrSubscriptionRequired, http.StatusPaymentRequired},
		{errors.New("db"), http.StatusInternalServerError},
	} {
		s.service.EXPECT().ListCompanyMembers(mock.Anything, models.ListCompanyMembersInput{
			CompanyUUID: companyID,
			RequestUser: userID,
			Limit:       20,
			Offset:      0,
		}).Return(models.CompanyMembersResult{}, tt.err).Once()
		rec, req := s.request(http.MethodGet, "/", "", userID, map[string]string{"uuid": companyID.String()})
		s.api.GetCompanyMembersOverview(rec, req)
		s.Equal(tt.code, rec.Code)
	}
}
