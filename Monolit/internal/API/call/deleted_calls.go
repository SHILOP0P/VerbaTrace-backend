package call

import (
	"errors"
	"net/http"
	"time"

	"verbatrace/monolit/internal/API/dto"
	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/converter"
	"verbatrace/monolit/internal/models"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// ListDeletedCalls shows the bin: calls waiting for their purge date, scoped to
// what the caller may restore.
func (h *CallHandler) ListDeletedCalls(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}

	input := models.ListDeletedCallsInput{UserID: userID, Limit: defaultCallsListLimit}
	query := r.URL.Query()
	if value := query.Get("limit"); value != "" {
		limit, err := parseLimit(value)
		if err != nil {
			response.WriteError(w, http.StatusBadRequest, response.CodeInvalidCallFilter, "invalid call filter")
			return
		}
		input.Limit = limit
	}
	if value := query.Get("offset"); value != "" {
		offset, err := parseOffset(value)
		if err != nil {
			response.WriteError(w, http.StatusBadRequest, response.CodeInvalidCallFilter, "invalid call filter")
			return
		}
		input.Offset = offset
	}

	result, err := h.service.ListDeletedCalls(r.Context(), input)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToListCalls, "failed to list calls")
		return
	}

	items := make([]dto.DeletedCallResponse, len(result.Items))
	for i, deleted := range result.Items {
		callResponse, convertErr := converter.CallModelToAPI(deleted.Call)
		if convertErr != nil {
			response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToConvertCall, "failed to convert call")
			return
		}
		item := dto.DeletedCallResponse{
			Call:       callResponse,
			DeletedAt:  deleted.DeletedAt.UTC().Format(time.RFC3339),
			PurgeAfter: deleted.PurgeAfter.UTC().Format(time.RFC3339),
		}
		if deleted.DeletedByUserUUID.Valid {
			deletedBy := deleted.DeletedByUserUUID.UUID.String()
			item.DeletedByUserUUID = &deletedBy
		}
		items[i] = item
	}

	_ = response.WriteJSON(w, http.StatusOK, dto.DeletedCallsListResponse{
		Items:  items,
		Total:  result.Total,
		Limit:  result.Limit,
		Offset: result.Offset,
	})
}

// RestoreCall takes a call back out of the bin before its purge date.
func (h *CallHandler) RestoreCall(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}

	callUUID, err := uuid.Parse(chi.URLParam(r, "uuid"))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidCallUUID, "invalid call uuid")
		return
	}

	call, err := h.service.RestoreCall(r.Context(), callUUID, userID)
	if err != nil {
		if errors.Is(err, models.ErrCallNotFound) {
			response.WriteError(w, http.StatusNotFound, response.CodeCallNotFound, "call not found")
			return
		}
		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToRestoreCall, "restore call failed")
		return
	}

	callResponse, err := converter.CallModelToAPI(call)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToConvertCall, "failed to convert call")
		return
	}

	_ = response.WriteJSON(w, http.StatusOK, callResponse)
}
