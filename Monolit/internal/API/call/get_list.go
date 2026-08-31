package call

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"verbatrace/monolit/internal/API/dto"
	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/converter"
	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

const (
	defaultCallsListLimit = 20
	maxCallsListLimit     = 100
)

func (h *CallHandler) List(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}

	input, filtered, err := parseListCallsInput(r, userID)
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidCallFilter, "invalid call filter")
		return
	}
	if filtered {
		result, err := h.service.ListFiltered(r.Context(), input)
		if err != nil {
			if errors.Is(err, models.ErrCallFolderNotFound) {
				response.WriteError(w, http.StatusNotFound, response.CodeCallFolderNotFound, "call folder not found")
				return
			}
			response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToListCalls, "failed to list calls")
			return
		}

		resp, err := callsListResultToAPI(result)
		if err != nil {
			response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToConvertCall, "failed to convert call")
			return
		}

		if err := response.WriteJSON(w, http.StatusOK, resp); err != nil {
			return
		}
		return
	}

	calls, err := h.service.List(r.Context(), userID)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToListCalls, "failed to list calls")
		return
	}

	resp := make([]dto.CallResponse, len(calls))

	for i, call := range calls {
		callResponse, err := converter.CallModelToAPI(call)
		if err != nil {
			response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToConvertCall, "failed to convert call")
			return
		}
		resp[i] = callResponse
	}

	if err := response.WriteJSON(w, http.StatusOK, resp); err != nil {
		return
	}
}

func (h *CallHandler) GetFilterOptions(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}

	input, err := parseCallFilterOptionsInput(r, userID)
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidCallFilter, "invalid call filter")
		return
	}

	options, err := h.service.GetFilterOptions(r.Context(), input)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToListCalls, "failed to list call filters")
		return
	}

	if err := response.WriteJSON(w, http.StatusOK, callFilterOptionsToAPI(options)); err != nil {
		return
	}
}

func parseListCallsInput(r *http.Request, userID uuid.UUID) (models.ListCallsInput, bool, error) {
	query := r.URL.Query()
	filtered := hasCallsListQuery(query)
	input := models.ListCallsInput{
		UserID:                userID,
		Limit:                 defaultCallsListLimit,
		Sort:                  "occurred_at",
		Order:                 "desc",
		IncludeUploadFallback: true,
	}
	if !filtered {
		return input, false, nil
	}

	input.Q = query.Get("q")

	if values := queryValues(query, "status"); len(values) > 0 {
		for _, value := range values {
			parsed := models.CallStatus(value)
			if !isValidCallStatus(parsed) {
				return models.ListCallsInput{}, false, models.ErrInvalidCallFilter
			}
			input.Statuses = append(input.Statuses, parsed)
		}
		if len(input.Statuses) == 1 {
			input.Status = input.Statuses[0]
			input.Statuses = nil
		}
	}
	if values := queryValues(query, "scope"); len(values) > 0 {
		for _, value := range values {
			parsed := models.CallVisibilityScope(value)
			if !isValidCallVisibilityScope(parsed) {
				return models.ListCallsInput{}, false, models.ErrInvalidCallFilter
			}
			input.VisibilityScopes = append(input.VisibilityScopes, parsed)
		}
		if len(input.VisibilityScopes) == 1 {
			input.VisibilityScope = input.VisibilityScopes[0]
			input.VisibilityScopes = nil
		}
	}

	var err error
	if input.CompanyUUID, err = parseOptionalUUID(query.Get("company_uuid")); err != nil {
		return models.ListCallsInput{}, false, err
	}
	if input.DepartmentUUIDs, err = parseUUIDValues(query, "department_uuid"); err != nil {
		return models.ListCallsInput{}, false, err
	}
	if len(input.DepartmentUUIDs) == 1 {
		input.DepartmentUUID = uuid.NullUUID{UUID: input.DepartmentUUIDs[0], Valid: true}
		input.DepartmentUUIDs = nil
	}
	if input.UploadedByUserUUID, err = parseOptionalUUID(query.Get("uploaded_by_user_uuid")); err != nil {
		return models.ListCallsInput{}, false, err
	}
	if input.From, err = parseOptionalISOTime(query.Get("from"), false); err != nil {
		return models.ListCallsInput{}, false, err
	}
	if input.To, err = parseOptionalISOTime(query.Get("to"), true); err != nil {
		return models.ListCallsInput{}, false, err
	}
	if input.FolderUUIDs, err = parseUUIDValues(query, "folder_uuid"); err != nil {
		return models.ListCallsInput{}, false, err
	}
	if len(input.FolderUUIDs) == 1 {
		input.FolderUUID = uuid.NullUUID{UUID: input.FolderUUIDs[0], Valid: true}
		input.FolderUUIDs = nil
	}
	if input.From != nil && input.To != nil && input.From.After(*input.To) {
		return models.ListCallsInput{}, false, models.ErrInvalidCallFilter
	}
	if input.ParticipantUserUUIDs, err = parseUUIDValues(query, "participant_user_uuid"); err != nil {
		return models.ListCallsInput{}, false, err
	}
	if input.ConnectionUUIDs, err = parseUUIDValues(query, "connection_uuid"); err != nil {
		return models.ListCallsInput{}, false, err
	}
	if input.OccurredFrom, err = parseOptionalRangeTime(query.Get("occurred_from"), false); err != nil {
		return models.ListCallsInput{}, false, err
	}
	if input.OccurredTo, err = parseOptionalRangeTime(query.Get("occurred_to"), true); err != nil {
		return models.ListCallsInput{}, false, err
	}
	if input.ImportedFrom, err = parseOptionalRangeTime(query.Get("imported_from"), false); err != nil {
		return models.ListCallsInput{}, false, err
	}
	if input.ImportedTo, err = parseOptionalRangeTime(query.Get("imported_to"), true); err != nil {
		return models.ListCallsInput{}, false, err
	}
	if invalidRange(input.OccurredFrom, input.OccurredTo) || invalidRange(input.ImportedFrom, input.ImportedTo) {
		return models.ListCallsInput{}, false, models.ErrInvalidCallFilter
	}
	if input.DurationMinSeconds, err = parseOptionalNonNegativeInt(query.Get("duration_min_seconds")); err != nil {
		return models.ListCallsInput{}, false, err
	}
	if input.DurationMaxSeconds, err = parseOptionalNonNegativeInt(query.Get("duration_max_seconds")); err != nil {
		return models.ListCallsInput{}, false, err
	}
	if input.DurationMinSeconds != nil && input.DurationMaxSeconds != nil && *input.DurationMinSeconds > *input.DurationMaxSeconds {
		return models.ListCallsInput{}, false, models.ErrInvalidCallFilter
	}
	if input.HasAnalysis, err = parseOptionalBoolFilter(query.Get("has_analysis")); err != nil {
		return models.ListCallsInput{}, false, err
	}
	if input.HasActions, err = parseOptionalBoolFilter(query.Get("has_actions")); err != nil {
		return models.ListCallsInput{}, false, err
	}
	if input.HasProcessingError, err = parseOptionalBoolFilter(query.Get("has_processing_error")); err != nil {
		return models.ListCallsInput{}, false, err
	}
	if value := query.Get("favorite_only"); value != "" {
		parsed, parseErr := strconv.ParseBool(value)
		if parseErr != nil {
			return models.ListCallsInput{}, false, models.ErrInvalidCallFilter
		}
		input.FavoriteOnly = parsed
	}
	if value := query.Get("include_upload_fallback"); value != "" {
		parsed, parseErr := strconv.ParseBool(value)
		if parseErr != nil {
			return models.ListCallsInput{}, false, models.ErrInvalidCallFilter
		}
		input.IncludeUploadFallback = parsed
	}
	input.SourceProvider = query.Get("source_provider")
	if input.SourceProvider != "" && input.SourceProvider != "manual" && input.SourceProvider != "generic_api" && input.SourceProvider != "bitrix24" {
		return models.ListCallsInput{}, false, models.ErrInvalidCallFilter
	}
	if value := query.Get("sort"); value != "" {
		if value != "occurred_at" && value != "created_at" && value != "duration" {
			return models.ListCallsInput{}, false, models.ErrInvalidCallFilter
		}
		input.Sort = value
	}
	if value := query.Get("order"); value != "" {
		value = strings.ToLower(value)
		if value != "asc" && value != "desc" {
			return models.ListCallsInput{}, false, models.ErrInvalidCallFilter
		}
		input.Order = value
	}
	if value := query.Get("cursor"); value != "" {
		input.Cursor, err = parseCallCursor(value, input.Sort, input.Order)
		if err != nil {
			return models.ListCallsInput{}, false, err
		}
	}

	if limit := query.Get("limit"); limit != "" {
		input.Limit, err = parseLimit(limit)
		if err != nil {
			return models.ListCallsInput{}, false, err
		}
	}
	if offset := query.Get("offset"); offset != "" {
		input.Offset, err = parseOffset(offset)
		if err != nil {
			return models.ListCallsInput{}, false, err
		}
	}

	return input, true, nil
}

func parseCallFilterOptionsInput(r *http.Request, userID uuid.UUID) (models.CallFilterOptionsInput, error) {
	query := r.URL.Query()
	companyID, err := parseOptionalUUID(query.Get("company_uuid"))
	if err != nil {
		return models.CallFilterOptionsInput{}, err
	}
	departmentID, err := parseOptionalUUID(query.Get("department_uuid"))
	if err != nil {
		return models.CallFilterOptionsInput{}, err
	}

	return models.CallFilterOptionsInput{
		UserID:         userID,
		CompanyUUID:    companyID,
		DepartmentUUID: departmentID,
	}, nil
}

func hasCallsListQuery(query map[string][]string) bool {
	for _, key := range []string{"q", "status", "scope", "company_uuid", "department_uuid", "participant_user_uuid", "uploaded_by_user_uuid", "from", "to", "folder_uuid", "source_provider", "connection_uuid", "occurred_from", "occurred_to", "imported_from", "imported_to", "duration_min_seconds", "duration_max_seconds", "has_analysis", "has_actions", "has_processing_error", "favorite_only", "include_upload_fallback", "sort", "order", "cursor", "limit", "offset"} {
		if _, ok := query[key]; ok {
			return true
		}
	}
	return false
}

func queryValues(query map[string][]string, key string) []string {
	var result []string
	for _, raw := range query[key] {
		for _, value := range strings.Split(raw, ",") {
			if value = strings.TrimSpace(value); value != "" {
				result = append(result, value)
			}
		}
	}
	return result
}

func parseUUIDValues(query map[string][]string, key string) ([]uuid.UUID, error) {
	values := queryValues(query, key)
	result := make([]uuid.UUID, 0, len(values))
	seen := make(map[uuid.UUID]struct{}, len(values))
	for _, value := range values {
		parsed, err := uuid.Parse(value)
		if err != nil {
			return nil, models.ErrInvalidCallFilter
		}
		if _, ok := seen[parsed]; ok {
			continue
		}
		seen[parsed] = struct{}{}
		result = append(result, parsed)
	}
	return result, nil
}

func parseOptionalUUID(value string) (uuid.NullUUID, error) {
	if value == "" {
		return uuid.NullUUID{}, nil
	}

	parsed, err := uuid.Parse(value)
	if err != nil {
		return uuid.NullUUID{}, models.ErrInvalidCallFilter
	}

	return uuid.NullUUID{UUID: parsed, Valid: true}, nil
}

func parseOptionalISOTime(value string, endOfDate bool) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}

	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		utc := parsed.UTC()
		return &utc, nil
	}

	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		return nil, models.ErrInvalidCallFilter
	}
	parsed = parsed.UTC()
	if endOfDate {
		parsed = parsed.AddDate(0, 0, 1).Add(-time.Nanosecond)
	}

	return &parsed, nil
}

func parseOptionalRangeTime(value string, upperExclusive bool) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		utc := parsed.UTC()
		return &utc, nil
	}
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		return nil, models.ErrInvalidCallFilter
	}
	parsed = parsed.UTC()
	if upperExclusive {
		parsed = parsed.AddDate(0, 0, 1)
	}
	return &parsed, nil
}

func invalidRange(from, to *time.Time) bool { return from != nil && to != nil && !from.Before(*to) }

func parseOptionalNonNegativeInt(value string) (*int, error) {
	if value == "" {
		return nil, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 {
		return nil, models.ErrInvalidCallFilter
	}
	return &parsed, nil
}

func parseOptionalBoolFilter(value string) (*bool, error) {
	if value == "" {
		return nil, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return nil, models.ErrInvalidCallFilter
	}
	return &parsed, nil
}

func parseCallCursor(value, sort, order string) (*models.CallListCursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return nil, models.ErrInvalidCallFilter
	}
	var cursor models.CallListCursor
	if err = json.Unmarshal(raw, &cursor); err != nil || cursor.CallID == uuid.Nil || cursor.SortValue == "" || cursor.Sort != sort || !strings.EqualFold(cursor.Order, order) {
		return nil, models.ErrInvalidCallFilter
	}
	if sort == "duration" {
		parsed, parseErr := strconv.Atoi(cursor.SortValue)
		if parseErr != nil || parsed < 0 {
			return nil, models.ErrInvalidCallFilter
		}
		cursor.DurationValue = &parsed
	} else {
		parsed, parseErr := time.Parse(time.RFC3339Nano, cursor.SortValue)
		if parseErr != nil {
			return nil, models.ErrInvalidCallFilter
		}
		parsed = parsed.UTC()
		cursor.TimeValue = &parsed
	}
	return &cursor, nil
}

func parseLimit(value string) (int, error) {
	limit, err := strconv.Atoi(value)
	if err != nil || limit <= 0 || limit > maxCallsListLimit {
		return 0, models.ErrInvalidCallFilter
	}
	return limit, nil
}

func parseOffset(value string) (int, error) {
	offset, err := strconv.Atoi(value)
	if err != nil || offset < 0 {
		return 0, models.ErrInvalidCallFilter
	}
	return offset, nil
}

func isValidCallStatus(status models.CallStatus) bool {
	switch status {
	case models.CallStatusNew,
		models.CallStatusProcessing,
		models.CallStatusTranscribed,
		models.CallStatusAnalyzed,
		models.CallStatusFailed:
		return true
	default:
		return false
	}
}

func isValidCallVisibilityScope(scope models.CallVisibilityScope) bool {
	switch scope {
	case models.CallVisibilityScopePersonal,
		models.CallVisibilityScopeCompany,
		models.CallVisibilityScopeDepartment:
		return true
	default:
		return false
	}
}

func callsListResultToAPI(result models.ListCallsResult) (dto.CallsListResponse, error) {
	items := make([]dto.CallResponse, len(result.Items))
	for i, call := range result.Items {
		callResponse, err := converter.CallModelToAPI(call)
		if err != nil {
			return dto.CallsListResponse{}, err
		}
		items[i] = callResponse
	}

	responseValue := dto.CallsListResponse{
		Items:  items,
		Total:  result.Total,
		Limit:  result.Limit,
		Offset: result.Offset,
	}
	if result.NextCursor != nil {
		raw, err := json.Marshal(result.NextCursor)
		if err != nil {
			return dto.CallsListResponse{}, err
		}
		encoded := base64.RawURLEncoding.EncodeToString(raw)
		responseValue.NextCursor = &encoded
	}
	return responseValue, nil
}

func callFilterOptionsToAPI(options models.CallFilterOptions) dto.CallFilterOptionsResponse {
	statuses := make([]string, len(options.Statuses))
	for i, status := range options.Statuses {
		statuses[i] = string(status)
	}

	scopes := make([]string, len(options.Scopes))
	for i, scope := range options.Scopes {
		scopes[i] = string(scope)
	}

	managers := make([]dto.CallFilterUserResponse, len(options.Managers))
	for i, manager := range options.Managers {
		managers[i] = dto.CallFilterUserResponse{
			ID:          manager.ID.String(),
			FullName:    manager.FullName,
			FullSurname: manager.FullSurname,
			Username:    manager.Username,
		}
	}
	connections := make([]dto.CallFilterConnectionResponse, len(options.Connections))
	for i, connection := range options.Connections {
		connections[i] = dto.CallFilterConnectionResponse{
			ID:       connection.ID.String(),
			Name:     connection.Name,
			Provider: connection.Provider,
		}
	}

	return dto.CallFilterOptionsResponse{
		Statuses:    statuses,
		Scopes:      scopes,
		Managers:    managers,
		Connections: connections,
	}
}
