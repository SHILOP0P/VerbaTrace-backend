package report

import (
	"encoding/json"
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
	defaultReportsListLimit = 20
	maxReportsListLimit     = 100
)

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}

	input, err := parseListReportsInput(r, userID)
	if err != nil {
		writeReportError(w, err, response.CodeFailedToListReports)
		return
	}

	result, err := h.service.List(r.Context(), input)
	if err != nil {
		writeReportError(w, err, response.CodeFailedToListReports)
		return
	}

	resp, err := converter.GlobalReportsModelToAPI(result)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToConvertReport, "failed to convert reports")
		return
	}

	_ = response.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) CreateGlobal(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}

	var req dto.CreateGlobalReportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidRequestBody, "invalid request body")
		return
	}

	input, err := createGlobalReportInput(req, userID)
	if err != nil {
		writeReportError(w, err, response.CodeFailedToCreateReport)
		return
	}

	report, err := h.service.CreateGlobal(r.Context(), input)
	if err != nil {
		writeReportError(w, err, response.CodeFailedToCreateReport)
		return
	}

	resp, err := converter.ReportModelToAPI(report)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToConvertReport, "failed to convert report")
		return
	}

	_ = response.WriteJSON(w, http.StatusCreated, resp)
}

func parseListReportsInput(r *http.Request, userID uuid.UUID) (models.ListReportsInput, error) {
	query := r.URL.Query()
	input := models.ListReportsInput{
		UserUUID: userID,
		Sort:     models.ReportSortCreatedAt,
		Order:    models.SortOrderDesc,
		Limit:    defaultReportsListLimit,
	}

	if format := query.Get("format"); format != "" {
		input.Format = models.ReportFormat(format)
	}
	if status := query.Get("status"); status != "" {
		input.Status = models.ReportStatus(status)
	}

	var err error
	if input.CompanyUUID, err = parseOptionalUUID(query.Get("company_uuid")); err != nil {
		return models.ListReportsInput{}, err
	}
	if input.DepartmentUUID, err = parseOptionalUUID(query.Get("department_uuid")); err != nil {
		return models.ListReportsInput{}, err
	}
	if input.CallUUID, err = parseOptionalUUID(query.Get("call_uuid")); err != nil {
		return models.ListReportsInput{}, err
	}
	if input.From, err = parseOptionalISOTime(query.Get("from")); err != nil {
		return models.ListReportsInput{}, err
	}
	if input.To, err = parseOptionalISOTime(query.Get("to")); err != nil {
		return models.ListReportsInput{}, err
	}

	if sort := query.Get("sort"); sort != "" {
		input.Sort = models.ReportSortField(sort)
	}
	if order := query.Get("order"); order != "" {
		input.Order = models.SortOrder(order)
	}
	if limit := query.Get("limit"); limit != "" {
		input.Limit, err = parseLimit(limit)
		if err != nil {
			return models.ListReportsInput{}, err
		}
	}
	if offset := query.Get("offset"); offset != "" {
		input.Offset, err = parseOffset(offset)
		if err != nil {
			return models.ListReportsInput{}, err
		}
	}

	return input, nil
}

func createGlobalReportInput(req dto.CreateGlobalReportRequest, userID uuid.UUID) (models.CreateGlobalReportInput, error) {
	callID, err := parseOptionalUUIDValue(req.CallUUID)
	if err != nil {
		return models.CreateGlobalReportInput{}, err
	}
	companyID, err := parseOptionalUUIDValue(req.CompanyUUID)
	if err != nil {
		return models.CreateGlobalReportInput{}, err
	}
	departmentID, err := parseOptionalUUIDValue(req.DepartmentUUID)
	if err != nil {
		return models.CreateGlobalReportInput{}, err
	}
	managerID, err := parseOptionalUUIDValue(req.ManagerUserUUID)
	if err != nil {
		return models.CreateGlobalReportInput{}, err
	}
	periodFrom, err := parseOptionalTimeValue(req.PeriodFrom)
	if err != nil {
		return models.CreateGlobalReportInput{}, err
	}
	periodTo, err := parseOptionalTimeValue(req.PeriodTo)
	if err != nil {
		return models.CreateGlobalReportInput{}, err
	}
	if periodFrom != nil && periodTo != nil && periodFrom.After(*periodTo) {
		return models.CreateGlobalReportInput{}, models.ErrInvalidReportInput
	}

	return models.CreateGlobalReportInput{
		UserUUID:        userID,
		Format:          models.ReportFormat(req.Format),
		Scope:           models.ReportScope(req.Scope),
		CallUUID:        callID,
		CompanyUUID:     companyID,
		DepartmentUUID:  departmentID,
		ManagerUserUUID: managerID,
		PeriodFrom:      periodFrom,
		PeriodTo:        periodTo,
	}, nil
}

func parseOptionalUUID(value string) (uuid.NullUUID, error) {
	if value == "" {
		return uuid.NullUUID{}, nil
	}

	parsed, err := uuid.Parse(value)
	if err != nil {
		return uuid.NullUUID{}, models.ErrInvalidReportInput
	}

	return uuid.NullUUID{UUID: parsed, Valid: true}, nil
}

func parseOptionalUUIDValue(value *string) (uuid.NullUUID, error) {
	if value == nil || *value == "" {
		return uuid.NullUUID{}, nil
	}
	return parseOptionalUUID(*value)
}

func parseOptionalISOTime(value string) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}

	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, models.ErrInvalidReportInput
	}
	utc := parsed.UTC()
	return &utc, nil
}

func parseOptionalTimeValue(value *string) (*time.Time, error) {
	if value == nil || *value == "" {
		return nil, nil
	}
	return parseOptionalISOTime(*value)
}

func parseLimit(value string) (int, error) {
	limit, err := strconv.Atoi(value)
	if err != nil || limit <= 0 || limit > maxReportsListLimit {
		return 0, models.ErrInvalidReportInput
	}
	return limit, nil
}

func parseOffset(value string) (int, error) {
	offset, err := strconv.Atoi(value)
	if err != nil || offset < 0 {
		return 0, models.ErrInvalidReportInput
	}
	return offset, nil
}
