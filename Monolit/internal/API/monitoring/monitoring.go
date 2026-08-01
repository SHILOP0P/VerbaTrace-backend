package monitoring

import (
	"errors"
	"net/http"
	"time"

	"verbatrace/monolit/internal/API/dto"
	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/httpserver/middleware"
	"verbatrace/monolit/internal/models"
	"verbatrace/monolit/internal/service"

	"github.com/google/uuid"
)

type Handler struct {
	service service.MonitoringService
}

func NewHandler(service service.MonitoringService) *Handler {
	return &Handler{service: service}
}

func (h *Handler) GetProcessing(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}
	role, ok := middleware.UserRoleFromContext(r.Context())
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}

	input, err := parseProcessingInput(r, userID, models.UserRole(role))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidCallFilter, "invalid monitoring filter")
		return
	}

	monitoring, err := h.service.GetProcessing(r.Context(), input)
	if err != nil {
		if errors.Is(err, models.ErrForbidden) || errors.Is(err, models.ErrCompanyNotFound) {
			response.WriteError(w, http.StatusForbidden, response.CodeForbidden, "forbidden")
			return
		}
		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToGetProcessingMonitoring, "failed to get processing monitoring")
		return
	}

	if err := response.WriteJSON(w, http.StatusOK, monitoringToAPI(monitoring)); err != nil {
		return
	}
}

func parseProcessingInput(r *http.Request, userID uuid.UUID, role models.UserRole) (models.ProcessingMonitoringInput, error) {
	query := r.URL.Query()
	input := models.ProcessingMonitoringInput{
		UserID:   userID,
		UserRole: role,
	}

	var err error
	if input.CompanyUUID, err = parseOptionalUUID(query.Get("company_uuid")); err != nil {
		return models.ProcessingMonitoringInput{}, err
	}
	if input.From, err = parseOptionalISOTime(query.Get("from"), false); err != nil {
		return models.ProcessingMonitoringInput{}, err
	}
	if input.To, err = parseOptionalISOTime(query.Get("to"), true); err != nil {
		return models.ProcessingMonitoringInput{}, err
	}
	if input.From != nil && input.To != nil && input.From.After(*input.To) {
		return models.ProcessingMonitoringInput{}, models.ErrInvalidCallFilter
	}

	return input, nil
}

func monitoringToAPI(monitoring models.ProcessingMonitoring) dto.ProcessingMonitoringResponse {
	return dto.ProcessingMonitoringResponse{
		Queue: dto.ProcessingQueueResponse{
			Pending: monitoring.Queue.Pending,
			Running: monitoring.Queue.Running,
			Done:    monitoring.Queue.Done,
			Failed:  monitoring.Queue.Failed,
			Retry:   monitoring.Queue.Retry,
		},
		AverageProcessingSeconds: monitoring.AverageProcessingSeconds,
	}
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
