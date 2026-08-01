package call

import (
	"errors"
	"net/http"
	"strconv"
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
		UserID: userID,
		Limit:  defaultCallsListLimit,
	}
	if !filtered {
		return input, false, nil
	}

	input.Q = query.Get("q")

	if status := query.Get("status"); status != "" {
		parsed := models.CallStatus(status)
		if !isValidCallStatus(parsed) {
			return models.ListCallsInput{}, false, models.ErrInvalidCallFilter
		}
		input.Status = parsed
	}
	if scope := query.Get("scope"); scope != "" {
		parsed := models.CallVisibilityScope(scope)
		if !isValidCallVisibilityScope(parsed) {
			return models.ListCallsInput{}, false, models.ErrInvalidCallFilter
		}
		input.VisibilityScope = parsed
	}

	var err error
	if input.CompanyUUID, err = parseOptionalUUID(query.Get("company_uuid")); err != nil {
		return models.ListCallsInput{}, false, err
	}
	if input.DepartmentUUID, err = parseOptionalUUID(query.Get("department_uuid")); err != nil {
		return models.ListCallsInput{}, false, err
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
	if input.FolderUUID, err = parseOptionalUUID(query.Get("folder_uuid")); err != nil {
		return models.ListCallsInput{}, false, err
	}
	if input.From != nil && input.To != nil && input.From.After(*input.To) {
		return models.ListCallsInput{}, false, models.ErrInvalidCallFilter
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
	for _, key := range []string{"q", "status", "scope", "company_uuid", "department_uuid", "uploaded_by_user_uuid", "from", "to", "folder_uuid", "limit", "offset"} {
		if _, ok := query[key]; ok {
			return true
		}
	}
	return false
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

	return dto.CallsListResponse{
		Items:  items,
		Total:  result.Total,
		Limit:  result.Limit,
		Offset: result.Offset,
	}, nil
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

	return dto.CallFilterOptionsResponse{
		Statuses: statuses,
		Scopes:   scopes,
		Managers: managers,
	}
}
