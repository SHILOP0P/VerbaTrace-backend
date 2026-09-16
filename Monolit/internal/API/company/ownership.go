package company

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

// OfferOwnership proposes the company to another member of the same company.
func (h *Handler) OfferOwnership(w http.ResponseWriter, r *http.Request) {
	requestUserID, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}

	companyID, err := uuid.Parse(chi.URLParam(r, "uuid"))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidCompanyInput, "invalid company uuid")
		return
	}

	var req dto.CreateOwnershipTransferRequest
	if err := decodeOptionalBody(r, &req); err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidRequestBody, "invalid request body")
		return
	}

	targetID, err := uuid.Parse(strings.TrimSpace(req.UserUUID))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidCompanyInput, "invalid user uuid")
		return
	}

	transfer, err := h.service.OfferOwnership(r.Context(), models.CreateCompanyOwnershipTransferInput{
		CompanyUUID: companyID,
		RequestUser: requestUserID,
		ToUserUUID:  targetID,
		Reason:      req.Reason,
	})
	if err != nil {
		writeCompanyMemberError(w, err, response.CodeFailedToUpdateCompanyMember, "failed to offer ownership")
		return
	}

	_ = response.WriteJSON(w, http.StatusCreated, ownershipTransferToAPI(transfer))
}

// ListIncomingOwnership shows the offers waiting for the current user.
func (h *Handler) ListIncomingOwnership(w http.ResponseWriter, r *http.Request) {
	requestUserID, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}

	transfers, err := h.service.ListIncomingOwnershipOffers(r.Context(), requestUserID)
	if err != nil {
		writeCompanyMemberError(w, err, response.CodeFailedToUpdateCompanyMember, "failed to list ownership transfers")
		return
	}

	items := make([]dto.CompanyOwnershipTransferResponse, 0, len(transfers))
	for _, transfer := range transfers {
		items = append(items, ownershipTransferToAPI(transfer))
	}

	_ = response.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

// AcceptOwnership and DeclineOwnership are the answers of the invited owner.
func (h *Handler) AcceptOwnership(w http.ResponseWriter, r *http.Request) {
	h.decideOwnership(w, r, true)
}

func (h *Handler) DeclineOwnership(w http.ResponseWriter, r *http.Request) {
	h.decideOwnership(w, r, false)
}

func (h *Handler) decideOwnership(w http.ResponseWriter, r *http.Request, accept bool) {
	requestUserID, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}

	transferID, err := uuid.Parse(chi.URLParam(r, "transfer_uuid"))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidCompanyInput, "invalid transfer uuid")
		return
	}

	transfer, err := h.service.DecideOwnership(r.Context(), models.DecideCompanyOwnershipTransferInput{
		TransferUUID: transferID,
		RequestUser:  requestUserID,
		Accept:       accept,
	})
	if err != nil {
		writeCompanyMemberError(w, err, response.CodeFailedToUpdateCompanyMember, "failed to decide ownership transfer")
		return
	}

	_ = response.WriteJSON(w, http.StatusOK, ownershipTransferToAPI(transfer))
}

// CancelOwnershipOffer withdraws an offer the owner no longer wants.
func (h *Handler) CancelOwnershipOffer(w http.ResponseWriter, r *http.Request) {
	requestUserID, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}

	transferID, err := uuid.Parse(chi.URLParam(r, "transfer_uuid"))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidCompanyInput, "invalid transfer uuid")
		return
	}

	transfer, err := h.service.CancelOwnershipOffer(r.Context(), transferID, requestUserID)
	if err != nil {
		writeCompanyMemberError(w, err, response.CodeFailedToUpdateCompanyMember, "failed to cancel ownership transfer")
		return
	}

	_ = response.WriteJSON(w, http.StatusOK, ownershipTransferToAPI(transfer))
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

func ownershipTransferToAPI(transfer models.CompanyOwnershipTransfer) dto.CompanyOwnershipTransferResponse {
	return dto.CompanyOwnershipTransferResponse{
		ID:          transfer.ID.String(),
		CompanyUUID: transfer.CompanyUUID.String(),
		FromUser:    transfer.FromUserUUID.String(),
		ToUser:      transfer.ToUserUUID.String(),
		Status:      string(transfer.Status),
		Reason:      transfer.Reason,
		CreatedAt:   transfer.CreatedAt.Format(time.RFC3339),
		ExpiresAt:   transfer.ExpiresAt.Format(time.RFC3339),
	}
}
