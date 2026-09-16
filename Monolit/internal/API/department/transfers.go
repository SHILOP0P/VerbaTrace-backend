package department

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"verbatrace/monolit/internal/API/dto"
	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/models"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// CreateDepartmentTransfer is how a department leader asks the deputy for a
// colleague from another department instead of taking them directly.
func (h *Handler) CreateDepartmentTransfer(w http.ResponseWriter, r *http.Request) {
	requestUserID, companyID, ok := h.companyScope(w, r)
	if !ok {
		return
	}

	departmentID, err := uuid.Parse(chi.URLParam(r, "department_uuid"))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidDepartmentInput, "invalid department uuid")
		return
	}

	var req dto.CreateDepartmentTransferRequest
	if err := decodeOptionalBody(r, &req); err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidRequestBody, "invalid request body")
		return
	}

	userID, err := uuid.Parse(strings.TrimSpace(req.UserUUID))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidDepartmentInput, "invalid user uuid")
		return
	}

	request, err := h.service.RequestTransfer(r.Context(), models.CreateDepartmentTransferInput{
		CompanyUUID:      companyID,
		ToDepartmentUUID: departmentID,
		UserUUID:         userID,
		RequestUser:      requestUserID,
		Reason:           req.Reason,
	})
	if err != nil {
		writeDepartmentError(w, err, response.CodeFailedToAddDepartmentMember, "failed to request transfer")
		return
	}

	_ = response.WriteJSON(w, http.StatusCreated, departmentTransferToAPI(request))
}

// ListDepartmentTransfers shows the owner and the deputy the pending requests.
func (h *Handler) ListDepartmentTransfers(w http.ResponseWriter, r *http.Request) {
	requestUserID, companyID, ok := h.companyScope(w, r)
	if !ok {
		return
	}

	status := models.DepartmentTransferStatus(strings.TrimSpace(r.URL.Query().Get("status")))
	requests, err := h.service.ListTransfers(r.Context(), companyID, requestUserID, status)
	if err != nil {
		writeDepartmentError(w, err, response.CodeFailedToListDepartments, "failed to list transfer requests")
		return
	}

	items := make([]dto.DepartmentTransferRequestResponse, 0, len(requests))
	for _, request := range requests {
		items = append(items, departmentTransferToAPI(request))
	}

	_ = response.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

// ApproveDepartmentTransfer performs the move; RejectDepartmentTransfer refuses.
func (h *Handler) ApproveDepartmentTransfer(w http.ResponseWriter, r *http.Request) {
	h.decideDepartmentTransfer(w, r, true)
}

func (h *Handler) RejectDepartmentTransfer(w http.ResponseWriter, r *http.Request) {
	h.decideDepartmentTransfer(w, r, false)
}

func (h *Handler) decideDepartmentTransfer(w http.ResponseWriter, r *http.Request, approve bool) {
	requestUserID, _, ok := h.companyScope(w, r)
	if !ok {
		return
	}

	requestID, err := uuid.Parse(chi.URLParam(r, "request_uuid"))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidDepartmentInput, "invalid request uuid")
		return
	}

	var req dto.DecideDepartmentTransferRequest
	if err := decodeOptionalBody(r, &req); err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidRequestBody, "invalid request body")
		return
	}

	decided, err := h.service.DecideTransfer(r.Context(), models.DecideDepartmentTransferInput{
		RequestUUID: requestID,
		RequestUser: requestUserID,
		Approve:     approve,
		Comment:     req.Comment,
	})
	if err != nil {
		writeDepartmentError(w, err, response.CodeFailedToAddDepartmentMember, "failed to decide transfer request")
		return
	}

	_ = response.WriteJSON(w, http.StatusOK, departmentTransferToAPI(decided))
}

func (h *Handler) companyScope(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, bool) {
	requestUserID, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return uuid.Nil, uuid.Nil, false
	}

	companyID, err := uuid.Parse(chi.URLParam(r, "uuid"))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidCompanyInput, "invalid company uuid")
		return uuid.Nil, uuid.Nil, false
	}

	return requestUserID, companyID, true
}

func decodeOptionalBody(r *http.Request, target any) error {
	if r.Body == nil {
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(target); err != nil && !errors.Is(err, io.EOF) {
		return err
	}

	return nil
}

func departmentTransferToAPI(request models.DepartmentTransferRequest) dto.DepartmentTransferRequestResponse {
	item := dto.DepartmentTransferRequestResponse{
		ID:               request.ID.String(),
		CompanyUUID:      request.CompanyUUID.String(),
		UserUUID:         request.UserUUID.String(),
		ToDepartmentUUID: request.ToDepartmentUUID.String(),
		RequestedBy:      request.RequestedByUserUUID.String(),
		Reason:           request.Reason,
		Status:           string(request.Status),
		DecisionComment:  request.DecisionComment,
		CreatedAt:        request.CreatedAt.Format(time.RFC3339),
		ExpiresAt:        request.ExpiresAt.Format(time.RFC3339),
	}
	if request.FromDepartmentUUID.Valid {
		value := request.FromDepartmentUUID.UUID.String()
		item.FromDepartmentUUID = &value
	}
	if request.DecidedByUserUUID.Valid {
		value := request.DecidedByUserUUID.UUID.String()
		item.DecidedBy = &value
	}

	return item
}
