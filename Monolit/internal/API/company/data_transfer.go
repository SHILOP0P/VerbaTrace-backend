package company

import (
	"errors"
	"net/http"
	"strconv"

	"verbatrace/monolit/internal/API/dto"
	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/converter"
	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

// TransferData moves calls and instruction folders out of one of the owner's
// companies into another, before the first one is deleted for good.
func (h *Handler) TransferData(w http.ResponseWriter, r *http.Request) {
	requestUserID, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}

	var req dto.TransferCompanyDataRequest
	if err := decodeOptionalBody(r, &req); err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidRequestBody, "invalid request body")
		return
	}

	source, err := uuid.Parse(req.SourceCompanyUUID)
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidCompanyInput, "invalid source company uuid")
		return
	}
	target, err := uuid.Parse(req.TargetCompanyUUID)
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidCompanyInput, "invalid target company uuid")
		return
	}
	calls, err := parseUUIDList(req.CallUUIDs)
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidCompanyInput, "invalid call uuid")
		return
	}

	// Nothing selected means everything, which is what an owner emptying a
	// company before it is deleted almost always wants.
	includeCalls, includeFolders := true, true
	if req.IncludeCalls != nil {
		includeCalls = *req.IncludeCalls
	}
	if req.IncludeFolders != nil {
		includeFolders = *req.IncludeFolders
	}

	result, err := h.service.TransferCompanyData(r.Context(), models.TransferCompanyDataInput{
		OwnerUserUUID:     requestUserID,
		SourceCompanyUUID: source,
		TargetCompanyUUID: target,
		CallUUIDs:         calls,
		IncludeCalls:      includeCalls,
		IncludeFolders:    includeFolders,
		Reason:            req.Reason,
	})
	if err != nil {
		writeTransferError(w, err)
		return
	}

	_ = response.WriteJSON(w, http.StatusOK, converter.CompanyDataTransferModelToAPI(result))
}

// ListDataTransfers shows the owner what they have already moved.
func (h *Handler) ListDataTransfers(w http.ResponseWriter, r *http.Request) {
	requestUserID, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}

	limit := 0
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 100 {
			response.WriteError(w, http.StatusBadRequest, response.CodeInvalidCompanyInput, "invalid limit")
			return
		}
		limit = parsed
	}

	transfers, err := h.service.ListCompanyDataTransfers(r.Context(), requestUserID, limit)
	if err != nil {
		writeTransferError(w, err)
		return
	}

	items := make([]dto.CompanyDataTransferResponse, 0, len(transfers))
	for _, transfer := range transfers {
		items = append(items, converter.CompanyDataTransferModelToAPI(transfer))
	}

	_ = response.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func writeTransferError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, models.ErrInvalidCompanyInput):
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidCompanyInput, "invalid transfer input")
	case errors.Is(err, models.ErrCompanyNotFound):
		response.WriteError(w, http.StatusNotFound, response.CodeCompanyNotFound, "company not found")
	case errors.Is(err, models.ErrForbidden), errors.Is(err, models.ErrOwnerOnlyAction):
		response.WriteError(w, http.StatusForbidden, response.CodeForbidden, "only the owner of both companies may move data between them")
	case errors.Is(err, models.ErrCompanyFrozen):
		response.WriteError(w, http.StatusConflict, response.CodeCompanyFrozen, "the receiving company is frozen")
	default:
		response.WriteError(w, http.StatusInternalServerError, response.CodeInternalServerError, "failed to transfer company data")
	}
}
