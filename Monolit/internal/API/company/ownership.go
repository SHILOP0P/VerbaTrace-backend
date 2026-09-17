package company

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"verbatrace/monolit/internal/API/dto"
	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/converter"
	"verbatrace/monolit/internal/models"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// OfferOwnership proposes this one company. It is refused when the owner's plan
// covers more than one, because the plan cannot be split.
func (h *Handler) OfferOwnership(w http.ResponseWriter, r *http.Request) {
	companyID, err := uuid.Parse(chi.URLParam(r, "uuid"))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidCompanyInput, "invalid company uuid")
		return
	}

	h.offerOwnership(w, r, models.CompanyOwnershipTransferScopeCompany, companyID)
}

// OfferAllOwnership hands over every company the owner has, together with the
// plan that covers them.
func (h *Handler) OfferAllOwnership(w http.ResponseWriter, r *http.Request) {
	h.offerOwnership(w, r, models.CompanyOwnershipTransferScopeAll, uuid.Nil)
}

func (h *Handler) offerOwnership(w http.ResponseWriter, r *http.Request, scope models.CompanyOwnershipTransferScope, companyID uuid.UUID) {
	requestUserID, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
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

	stay, err := parseUUIDList(req.StayCompanyUUIDs)
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidCompanyInput, "invalid stay company uuid")
		return
	}

	transfer, err := h.service.OfferOwnership(r.Context(), models.CreateCompanyOwnershipTransferInput{
		CompanyUUID:      companyID,
		Scope:            scope,
		RequestUser:      requestUserID,
		ToUserUUID:       targetID,
		StayCompanyUUIDs: stay,
		Reason:           req.Reason,
	})
	if err != nil {
		writeCompanyMemberError(w, err, response.CodeFailedToUpdateCompanyMember, "failed to offer ownership")
		return
	}

	_ = response.WriteJSON(w, http.StatusCreated, converter.OwnershipTransferModelToAPI(transfer))
}

func parseUUIDList(raw []string) ([]uuid.UUID, error) {
	ids := make([]uuid.UUID, 0, len(raw))
	for _, value := range raw {
		id, err := uuid.Parse(strings.TrimSpace(value))
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}

	return ids, nil
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
		items = append(items, converter.OwnershipTransferModelToAPI(transfer))
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

	_ = response.WriteJSON(w, http.StatusOK, converter.OwnershipTransferModelToAPI(transfer))
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

	_ = response.WriteJSON(w, http.StatusOK, converter.OwnershipTransferModelToAPI(transfer))
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
