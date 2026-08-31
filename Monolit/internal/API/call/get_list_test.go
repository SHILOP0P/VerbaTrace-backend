package call

import (
	"encoding/base64"
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

func (s *APISuite) TestListFilteredParsesAdvancedRepeatableFilters() {
	userID := uuid.New()
	departmentA, departmentB := uuid.New(), uuid.New()
	participantID, connectionID := uuid.New(), uuid.New()
	s.service.On("ListFiltered", mock.Anything, mock.MatchedBy(func(input models.ListCallsInput) bool {
		return input.UserID == userID &&
			len(input.Statuses) == 2 && input.Statuses[0] == models.CallStatusAnalyzed && input.Statuses[1] == models.CallStatusFailed &&
			len(input.DepartmentUUIDs) == 2 && input.DepartmentUUIDs[0] == departmentA && input.DepartmentUUIDs[1] == departmentB &&
			len(input.ParticipantUserUUIDs) == 1 && input.ParticipantUserUUIDs[0] == participantID &&
			len(input.ConnectionUUIDs) == 1 && input.ConnectionUUIDs[0] == connectionID &&
			input.SourceProvider == "bitrix24" && input.OccurredFrom != nil && input.OccurredTo != nil &&
			input.DurationMinSeconds != nil && *input.DurationMinSeconds == 30 && input.DurationMaxSeconds != nil && *input.DurationMaxSeconds == 900 &&
			input.HasAnalysis != nil && *input.HasAnalysis && input.HasActions != nil && !*input.HasActions && input.HasProcessingError != nil && *input.HasProcessingError &&
			input.FavoriteOnly && input.Sort == "duration" && input.Order == "asc"
	})).Return(models.ListCallsResult{Items: []models.Call{}, Limit: 20}, nil).Once()

	values := url.Values{}
	values.Add("status", "analyzed")
	values.Add("status", "failed")
	values.Add("department_uuid", departmentA.String())
	values.Add("department_uuid", departmentB.String())
	values.Set("participant_user_uuid", participantID.String())
	values.Set("connection_uuid", connectionID.String())
	values.Set("source_provider", "bitrix24")
	values.Set("occurred_from", "2026-08-01T00:00:00Z")
	values.Set("occurred_to", "2026-09-01T00:00:00Z")
	values.Set("duration_min_seconds", "30")
	values.Set("duration_max_seconds", "900")
	values.Set("has_analysis", "true")
	values.Set("has_actions", "false")
	values.Set("has_processing_error", "true")
	values.Set("favorite_only", "true")
	values.Set("sort", "duration")
	values.Set("order", "asc")
	rec, req := s.request(http.MethodGet, "/api/v1/calls?"+values.Encode(), "", userID, nil)
	s.api.List(rec, req)
	s.Require().Equal(http.StatusOK, rec.Code)
}

func (s *APISuite) TestListFilteredRejectsInvertedAdvancedRanges() {
	rec, req := s.request(http.MethodGet, "/api/v1/calls?occurred_from=2026-09-01T00:00:00Z&occurred_to=2026-08-01T00:00:00Z", "", uuid.New(), nil)
	s.api.List(rec, req)
	s.Require().Equal(http.StatusBadRequest, rec.Code)
	s.requireErrorCode(rec, response.CodeInvalidCallFilter)
}

func (s *APISuite) TestListCursorIsBoundToSortAndOrder() {
	callID := uuid.New()
	raw, err := json.Marshal(models.CallListCursor{SortValue: "2026-08-01T00:00:00Z", CallID: callID, Sort: "occurred_at", Order: "desc"})
	s.Require().NoError(err)
	cursor := base64.RawURLEncoding.EncodeToString(raw)
	parsed, err := parseCallCursor(cursor, "occurred_at", "desc")
	s.Require().NoError(err)
	s.Require().Equal(callID, parsed.CallID)
	_, err = parseCallCursor(cursor, "created_at", "desc")
	s.Require().ErrorIs(err, models.ErrInvalidCallFilter)
	_, err = parseCallCursor(cursor, "occurred_at", "asc")
	s.Require().ErrorIs(err, models.ErrInvalidCallFilter)
}

func (s *APISuite) TestGetFilterOptionsSuccess() {
	userID := uuid.New()
	companyID := uuid.New()
	managerID := uuid.New()
	connectionID := uuid.New()

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
			Connections: []models.CallFilterConnection{{
				ID:       connectionID,
				Name:     "Bitrix24 sales",
				Provider: "bitrix24",
			}},
		}, nil).
		Once()

	rec, req := s.request(http.MethodGet, "/api/v1/calls/filters?company_uuid="+companyID.String(), "", userID, nil)

	s.api.GetFilterOptions(rec, req)

	s.Require().Equal(http.StatusOK, rec.Code)
	s.Require().JSONEq(`{
		"statuses":["new","failed"],
		"scopes":["personal","company"],
		"managers":[{"id":"`+managerID.String()+`","full_name":"Ivan","full_surname":"Petrov","username":"petrov"}],
		"connections":[{"id":"`+connectionID.String()+`","name":"Bitrix24 sales","provider":"bitrix24"}]
	}`, rec.Body.String())
}

func (s *APISuite) TestGetFilterOptionsRequiresAuth() {
	rec, req := s.request(http.MethodGet, "/api/v1/calls/filters", "", uuid.Nil, nil)

	s.api.GetFilterOptions(rec, req)

	s.Require().Equal(http.StatusUnauthorized, rec.Code)
	s.requireErrorCode(rec, response.CodeUnauthorized)
}
