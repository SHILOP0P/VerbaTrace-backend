package call

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"time"

	"verbatrace/monolit/internal/API/dto"
	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
)

func (s *APISuite) TestListSuccess() {
	userID := uuid.New()

	s.service.On("List", mock.Anything, userID).
		Return([]models.Call{{ID: uuid.New(), Title: "call", Status: models.CallStatusNew, VisibilityScope: models.CallVisibilityScopePersonal, CreatedAt: time.Now().UTC()}}, nil).
		Once()

	rec, req := s.request(http.MethodGet, "/api/v1/calls", "", userID, nil)

	s.api.List(rec, req)

	s.Require().Equal(http.StatusOK, rec.Code)
	var resp []dto.CallResponse
	s.Require().NoError(json.Unmarshal(rec.Body.Bytes(), &resp))
	s.Require().Len(resp, 1)
	s.Require().Equal("call", resp[0].Title)
}

func (s *APISuite) TestListRequiresAuth() {
	rec, req := s.request(http.MethodGet, "/api/v1/calls", "", uuid.Nil, nil)

	s.api.List(rec, req)

	s.Require().Equal(http.StatusUnauthorized, rec.Code)
	s.requireErrorCode(rec, response.CodeUnauthorized)
}

func (s *APISuite) TestListMapsServiceError() {
	userID := uuid.New()

	s.service.On("List", mock.Anything, userID).Return(nil, errors.New("list failed")).Once()

	rec, req := s.request(http.MethodGet, "/api/v1/calls", "", userID, nil)

	s.api.List(rec, req)

	s.Require().Equal(http.StatusInternalServerError, rec.Code)
	s.requireErrorCode(rec, response.CodeFailedToListCalls)
}

func (s *APISuite) TestListFilteredReturnsEnvelope() {
	userID := uuid.New()
	companyID := uuid.New()
	departmentID := uuid.New()
	uploaderID := uuid.New()
	from := "2026-07-01T10:00:00Z"
	to := "2026-07-03"
	callID := uuid.New()
	createdAt := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)

	s.service.On("ListFiltered", mock.Anything, mock.MatchedBy(func(input models.ListCallsInput) bool {
		return input.UserID == userID &&
			input.Q == "sales" &&
			input.Status == models.CallStatusAnalyzed &&
			input.VisibilityScope == models.CallVisibilityScopeDepartment &&
			input.CompanyUUID.Valid && input.CompanyUUID.UUID == companyID &&
			input.DepartmentUUID.Valid && input.DepartmentUUID.UUID == departmentID &&
			input.UploadedByUserUUID.Valid && input.UploadedByUserUUID.UUID == uploaderID &&
			input.From != nil && input.From.Equal(time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)) &&
			input.To != nil && input.To.After(time.Date(2026, 7, 3, 23, 59, 59, 0, time.UTC)) &&
			input.Limit == 10 &&
			input.Offset == 5
	})).
		Return(models.ListCallsResult{
			Items: []models.Call{{
				ID:              callID,
				Title:           "sales call",
				Status:          models.CallStatusAnalyzed,
				VisibilityScope: models.CallVisibilityScopeDepartment,
				CreatedAt:       createdAt,
			}},
			Total:  42,
			Limit:  10,
			Offset: 5,
		}, nil).
		Once()

	values := url.Values{}
	values.Set("q", "sales")
	values.Set("status", "analyzed")
	values.Set("scope", "department")
	values.Set("company_uuid", companyID.String())
	values.Set("department_uuid", departmentID.String())
	values.Set("uploaded_by_user_uuid", uploaderID.String())
	values.Set("from", from)
	values.Set("to", to)
	values.Set("limit", "10")
	values.Set("offset", "5")
	rec, req := s.request(http.MethodGet, "/api/v1/calls?"+values.Encode(), "", userID, nil)

	s.api.List(rec, req)

	s.Require().Equal(http.StatusOK, rec.Code)
	var resp dto.CallsListResponse
	s.Require().NoError(json.Unmarshal(rec.Body.Bytes(), &resp))
	s.Require().Equal(42, resp.Total)
	s.Require().Equal(10, resp.Limit)
	s.Require().Equal(5, resp.Offset)
	s.Require().Len(resp.Items, 1)
	s.Require().Equal(callID.String(), resp.Items[0].ID)
}

func (s *APISuite) TestListFilteredRejectsInvalidFilter() {
	rec, req := s.request(http.MethodGet, "/api/v1/calls?status=done", "", uuid.New(), nil)

	s.api.List(rec, req)

	s.Require().Equal(http.StatusBadRequest, rec.Code)
	s.requireErrorCode(rec, response.CodeInvalidCallFilter)
}

func (s *APISuite) TestGetFilterOptionsSuccess() {
	userID := uuid.New()
	companyID := uuid.New()
	managerID := uuid.New()

	s.service.On("GetFilterOptions", mock.Anything, mock.MatchedBy(func(input models.CallFilterOptionsInput) bool {
		return input.UserID == userID && input.CompanyUUID.Valid && input.CompanyUUID.UUID == companyID && !input.DepartmentUUID.Valid
	})).
		Return(models.CallFilterOptions{
			Statuses: []models.CallStatus{models.CallStatusNew, models.CallStatusFailed},
			Scopes:   []models.CallVisibilityScope{models.CallVisibilityScopePersonal, models.CallVisibilityScopeCompany},
			Managers: []models.CallFilterUser{{
				ID:          managerID,
				FullName:    "Ivan",
				FullSurname: "Petrov",
				Username:    "petrov",
			}},
		}, nil).
		Once()

	rec, req := s.request(http.MethodGet, "/api/v1/calls/filters?company_uuid="+companyID.String(), "", userID, nil)

	s.api.GetFilterOptions(rec, req)

	s.Require().Equal(http.StatusOK, rec.Code)
	s.Require().JSONEq(`{
		"statuses":["new","failed"],
		"scopes":["personal","company"],
		"managers":[{"id":"`+managerID.String()+`","full_name":"Ivan","full_surname":"Petrov","username":"petrov"}]
	}`, rec.Body.String())
}

func (s *APISuite) TestGetFilterOptionsRequiresAuth() {
	rec, req := s.request(http.MethodGet, "/api/v1/calls/filters", "", uuid.Nil, nil)

	s.api.GetFilterOptions(rec, req)

	s.Require().Equal(http.StatusUnauthorized, rec.Code)
	s.requireErrorCode(rec, response.CodeUnauthorized)
}
