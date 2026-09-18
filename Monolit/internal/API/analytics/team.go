package analytics

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/httpserver/middleware"
	"verbatrace/monolit/internal/models"
	"verbatrace/monolit/internal/service/teamanalytics"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// SetTeamAnalytics adds the analytics pages built on facts: summary, criteria,
// employees, departments, the matrix, profiles and drill-downs.
func (h *Handler) SetTeamAnalytics(service *teamanalytics.Service) { h.team = service }

func (h *Handler) teamRequest(w http.ResponseWriter, r *http.Request) (teamanalytics.Request, bool) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return teamanalytics.Request{}, false
	}
	if h.team == nil {
		response.WriteError(w, http.StatusNotImplemented, response.CodeNotImplemented, "team analytics is not configured")
		return teamanalytics.Request{}, false
	}
	query := r.URL.Query()
	req := teamanalytics.Request{UserID: userID, Personal: query.Get("scope") == "personal",
		IncludeInternal: query.Get("include_internal") == "true", ExcludeShared: query.Get("exclude_shared") == "true"}
	var err error
	for name, target := range map[string]*uuid.NullUUID{
		"company_uuid": &req.CompanyID, "department_uuid": &req.DepartmentID, "employee_uuid": &req.EmployeeID,
		"instruction_uuid": &req.InstructionID, "folder_uuid": &req.FolderID,
	} {
		if *target, err = parseOptionalUUID(query.Get(name)); err != nil {
			response.WriteError(w, http.StatusBadRequest, response.CodeInvalidAnalyticsFilter, "invalid "+name)
			return teamanalytics.Request{}, false
		}
	}
	for name, target := range map[string]**time.Time{"from": &req.From, "to": &req.To} {
		if raw := query.Get(name); raw != "" {
			parsed, err := time.Parse(time.RFC3339, raw)
			if err != nil {
				response.WriteError(w, http.StatusBadRequest, response.CodeInvalidAnalyticsFilter, "invalid "+name)
				return teamanalytics.Request{}, false
			}
			*target = &parsed
		}
	}
	return req, true
}

func writeTeamError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, teamanalytics.ErrTeamAnalyticsDenied):
		response.WriteError(w, http.StatusForbidden, response.CodeTeamAnalyticsAccessDenied, "Командная аналитика не входит в тариф")
	case errors.Is(err, teamanalytics.ErrPersonalProgressDenied):
		response.WriteError(w, http.StatusForbidden, response.CodePersonalProgressAccessDenied, "Личный прогресс доступен на тарифах Plus и Pro")
	case errors.Is(err, teamanalytics.ErrForbidden):
		response.WriteError(w, http.StatusForbidden, response.CodeForbidden, "forbidden")
	case errors.Is(err, teamanalytics.ErrNotFound):
		response.WriteError(w, http.StatusNotFound, response.CodeAnalyticsNotFound, "not found")
	case errors.Is(err, teamanalytics.ErrInvalidRequest):
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidAnalyticsFilter, "invalid analytics request")
	case errors.Is(err, teamanalytics.ErrSettingsVersionConflict):
		response.WriteError(w, http.StatusConflict, response.CodeAnalyticsSettingsConflict, "Настройки изменили в другом окне")
	case errors.Is(err, models.ErrCompanyFrozen):
		response.WriteError(w, http.StatusConflict, response.CodeCompanyFrozen, "company is frozen")
	default:
		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToGetAnalyticsOverview, "failed to read analytics")
	}
}

func respondTeam[T any](w http.ResponseWriter, value T, err error) {
	if err != nil {
		writeTeamError(w, err)
		return
	}
	_ = response.WriteJSON(w, http.StatusOK, value)
}

func (h *Handler) GetCapabilities(w http.ResponseWriter, r *http.Request) {
	if req, ok := h.teamRequest(w, r); ok {
		value, err := h.team.Capabilities(r.Context(), req)
		respondTeam(w, value, err)
	}
}

func (h *Handler) GetSummary(w http.ResponseWriter, r *http.Request) {
	if req, ok := h.teamRequest(w, r); ok {
		value, err := h.team.Summary(r.Context(), req)
		respondTeam(w, value, err)
	}
}

func (h *Handler) GetCriteria(w http.ResponseWriter, r *http.Request) {
	if req, ok := h.teamRequest(w, r); ok {
		value, err := h.team.Criteria(r.Context(), req, r.URL.Query().Get("sort"), r.URL.Query().Get("order"))
		respondTeam(w, value, err)
	}
}

func (h *Handler) GetEmployees(w http.ResponseWriter, r *http.Request) {
	if req, ok := h.teamRequest(w, r); ok {
		value, err := h.team.Employees(r.Context(), req)
		respondTeam(w, value, err)
	}
}

func (h *Handler) GetDepartments(w http.ResponseWriter, r *http.Request) {
	if req, ok := h.teamRequest(w, r); ok {
		value, err := h.team.Departments(r.Context(), req)
		respondTeam(w, value, err)
	}
}

func (h *Handler) GetMatrix(w http.ResponseWriter, r *http.Request) {
	if req, ok := h.teamRequest(w, r); ok {
		value, err := h.team.Matrix(r.Context(), req)
		respondTeam(w, value, err)
	}
}

// GetEmployeeProfile serves /analytics/employees/{user_uuid}; "me" is the
// viewer's own profile.
func (h *Handler) GetEmployeeProfile(w http.ResponseWriter, r *http.Request) {
	req, ok := h.teamRequest(w, r)
	if !ok {
		return
	}
	target := uuid.Nil
	if raw := chi.URLParam(r, "user_uuid"); raw != "me" {
		parsed, err := uuid.Parse(raw)
		if err != nil {
			response.WriteError(w, http.StatusBadRequest, response.CodeInvalidAnalyticsFilter, "invalid user uuid")
			return
		}
		target = parsed
	}
	value, err := h.team.Profile(r.Context(), req, target)
	respondTeam(w, value, err)
}

// GetCallProgress serves /calls/{uuid}/progress: the work on mistakes of one
// call against the employee's previous calls.
func (h *Handler) GetCallProgress(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}
	if h.team == nil {
		response.WriteError(w, http.StatusNotImplemented, response.CodeNotImplemented, "team analytics is not configured")
		return
	}
	callID, err := uuid.Parse(chi.URLParam(r, "uuid"))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidCallUUID, "invalid call uuid")
		return
	}
	value, err := h.team.CallProgress(r.Context(), userID, callID)
	if errors.Is(err, teamanalytics.ErrNotFound) {
		response.WriteError(w, http.StatusNotFound, response.CodeCallNotFound, "call not found")
		return
	}
	respondTeam(w, value, err)
}

// GetEmployeeProgress serves /analytics/employees/{user_uuid}/progress; "me"
// is the viewer.
func (h *Handler) GetEmployeeProgress(w http.ResponseWriter, r *http.Request) {
	req, ok := h.teamRequest(w, r)
	if !ok {
		return
	}
	target := uuid.Nil
	if raw := chi.URLParam(r, "user_uuid"); raw != "me" {
		parsed, err := uuid.Parse(raw)
		if err != nil {
			response.WriteError(w, http.StatusBadRequest, response.CodeInvalidAnalyticsFilter, "invalid user uuid")
			return
		}
		target = parsed
	}
	value, err := h.team.EmployeeProgress(r.Context(), req, target)
	respondTeam(w, value, err)
}

func (h *Handler) GetCriterionCalls(w http.ResponseWriter, r *http.Request) {
	req, ok := h.teamRequest(w, r)
	if !ok {
		return
	}
	key, err := uuid.Parse(chi.URLParam(r, "criterion_key"))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidAnalyticsFilter, "invalid criterion key")
		return
	}
	query := r.URL.Query()
	limit, _ := strconv.Atoi(query.Get("limit"))
	offset, _ := strconv.Atoi(query.Get("offset"))
	value, err := h.team.CriterionCalls(r.Context(), req, key, query.Get("status"), query.Get("sort"), limit, offset)
	respondTeam(w, value, err)
}

func (h *Handler) companySettingsTarget(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, bool) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return uuid.Nil, uuid.Nil, false
	}
	if h.team == nil {
		response.WriteError(w, http.StatusNotImplemented, response.CodeNotImplemented, "team analytics is not configured")
		return uuid.Nil, uuid.Nil, false
	}
	companyID, err := uuid.Parse(chi.URLParam(r, "uuid"))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidCompanyUUID, "invalid company uuid")
		return uuid.Nil, uuid.Nil, false
	}
	return companyID, userID, true
}

// GetCompanySettings and UpdateCompanySettings serve
// /companies/{uuid}/analytics-settings for the owner and the deputy.
func (h *Handler) GetCompanySettings(w http.ResponseWriter, r *http.Request) {
	if companyID, userID, ok := h.companySettingsTarget(w, r); ok {
		value, err := h.team.GetSettings(r.Context(), companyID, userID)
		respondTeam(w, value, err)
	}
}

func (h *Handler) UpdateCompanySettings(w http.ResponseWriter, r *http.Request) {
	companyID, userID, ok := h.companySettingsTarget(w, r)
	if !ok {
		return
	}
	var body struct {
		LockVersion            int   `json:"lock_version"`
		CriticalAlertThreshold *int  `json:"critical_alert_threshold"`
		GrowthAreasEnabled     *bool `json:"growth_areas_enabled"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidRequestBody, "invalid request body")
		return
	}
	value, err := h.team.UpdateSettings(r.Context(), companyID, userID, teamanalytics.SettingsPatch{
		LockVersion: body.LockVersion, CriticalAlertThreshold: body.CriticalAlertThreshold, GrowthAreasEnabled: body.GrowthAreasEnabled})
	respondTeam(w, value, err)
}
