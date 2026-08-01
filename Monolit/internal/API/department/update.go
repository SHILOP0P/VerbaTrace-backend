package department

import (
	"encoding/json"
	"errors"
	"net/http"

	"verbatrace/monolit/internal/API/dto"
	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/converter"
	"verbatrace/monolit/internal/models"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func (h *Handler) UpdateDepartment(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}

	companyID, departmentID, ok := departmentIDsFromRequest(w, r)
	if !ok {
		return
	}

	var req dto.UpdateDepartmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidDepartmentInput, "invalid request body")
		return
	}

	department, err := h.service.UpdateDepartment(r.Context(), models.UpdateDepartmentInput{
		CompanyUUID:    companyID,
		DepartmentUUID: departmentID,
		RequestUser:    userID,
		Name:           req.Name,
	})
	if err != nil {
		writeDepartmentError(w, err, response.CodeFailedToConvertDepartment, "failed to update department")
		return
	}

	resp, err := converter.DepartmentModelToAPI(department)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToConvertDepartment, "failed to convert department")
		return
	}

	_ = response.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) DeleteDepartment(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}

	companyID, departmentID, ok := departmentIDsFromRequest(w, r)
	if !ok {
		return
	}

	err := h.service.DeleteDepartment(r.Context(), models.DeleteDepartmentInput{
		CompanyUUID:    companyID,
		DepartmentUUID: departmentID,
		RequestUser:    userID,
	})
	if err != nil {
		writeDepartmentError(w, err, response.CodeFailedToConvertDepartment, "failed to delete department")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func departmentIDsFromRequest(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, bool) {
	companyID, err := uuid.Parse(chi.URLParam(r, "uuid"))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidDepartmentInput, "invalid company uuid")
		return uuid.Nil, uuid.Nil, false
	}

	departmentID, err := uuid.Parse(chi.URLParam(r, "department_uuid"))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidDepartmentInput, "invalid department uuid")
		return uuid.Nil, uuid.Nil, false
	}

	return companyID, departmentID, true
}

func writeDepartmentError(w http.ResponseWriter, err error, code string, message string) {
	if errors.Is(err, models.ErrInvalidDepartmentInput) {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidDepartmentInput, "invalid department input")
		return
	}
	if errors.Is(err, models.ErrDepartmentNotFound) {
		response.WriteError(w, http.StatusNotFound, response.CodeDepartmentNotFound, "department not found")
		return
	}
	if errors.Is(err, models.ErrForbidden) {
		response.WriteError(w, http.StatusForbidden, response.CodeForbidden, "forbidden")
		return
	}
	if errors.Is(err, models.ErrSubscriptionRequired) {
		response.WriteError(w, http.StatusPaymentRequired, response.CodeSubscriptionRequired, "subscription required")
		return
	}

	response.WriteError(w, http.StatusInternalServerError, code, message)
}
