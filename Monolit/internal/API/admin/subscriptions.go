package admin

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"verbatrace/monolit/internal/API/dto"
	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/httpserver/middleware"
	"verbatrace/monolit/internal/models"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func (h *Handler) ListCompanies(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit := 50
	offset := 0
	var err error
	if q.Get("limit") != "" {
		limit, err = strconv.Atoi(q.Get("limit"))
		if err != nil {
			response.WriteError(w, http.StatusBadRequest, response.CodeInvalidAdminInput, "invalid pagination")
			return
		}
	}
	if q.Get("offset") != "" {
		offset, err = strconv.Atoi(q.Get("offset"))
		if err != nil {
			response.WriteError(w, http.StatusBadRequest, response.CodeInvalidAdminInput, "invalid pagination")
			return
		}
	}
	// Company name/tag/manager are administrative directory metadata. They are
	// needed to identify a support-access subject and do not expose calls,
	// recordings, analyses, or other customer content.
	res, err := h.service.ListCompanies(r.Context(), models.ListAdminCompaniesInput{Query: q.Get("q"), Limit: limit, Offset: offset})
	if err != nil {
		writeAdminError(w, err, response.CodeFailedToListAdminCompanies, "failed to list companies")
		return
	}
	items := make([]dto.AdminCompanyResponse, 0, len(res.Companies))
	for _, c := range res.Companies {
		items = append(items, adminCompanyResponse(c))
	}
	_ = response.WriteJSON(w, http.StatusOK, dto.AdminCompaniesResponse{Items: items, Total: res.Total, Limit: res.Limit, Offset: res.Offset})
}
func (h *Handler) GetCompany(w http.ResponseWriter, r *http.Request) {
	id, ok := adminCompanyID(w, r)
	if !ok {
		return
	}
	c, err := h.service.GetCompany(r.Context(), id)
	if err != nil {
		writeAdminError(w, err, response.CodeFailedToGetAdminCompany, "failed to get company")
		return
	}
	_ = response.WriteJSON(w, http.StatusOK, adminCompanyResponse(c))
}
func (h *Handler) GetPersonalSubscription(w http.ResponseWriter, r *http.Request) {
	id, ok := adminUserID(w, r)
	if !ok {
		return
	}
	if !h.authorizeUser(w, r, id, "billing_summary") {
		return
	}
	sub, err := h.service.GetPersonalSubscription(r.Context(), id)
	if err != nil {
		writeAdminError(w, err, response.CodeFailedToGetAdminSubscription, "failed to get subscription")
		return
	}
	_ = response.WriteJSON(w, http.StatusOK, adminSubscriptionResponse(sub))
}
func (h *Handler) GetCompanySubscription(w http.ResponseWriter, r *http.Request) {
	id, ok := adminCompanyID(w, r)
	if !ok {
		return
	}
	if !h.authorizeCompany(w, r, id, "billing_summary") {
		return
	}
	sub, err := h.service.GetCompanySubscription(r.Context(), id)
	if err != nil {
		writeAdminError(w, err, response.CodeFailedToGetAdminSubscription, "failed to get subscription")
		return
	}
	_ = response.WriteJSON(w, http.StatusOK, adminSubscriptionResponse(sub))
}
func (h *Handler) GrantPersonalSubscription(w http.ResponseWriter, r *http.Request) {
	id, ok := adminUserID(w, r)
	if !ok {
		return
	}
	h.grantSubscription(w, r, models.GrantAdminSubscriptionInput{UserUUID: id})
}
func (h *Handler) GrantCompanySubscription(w http.ResponseWriter, r *http.Request) {
	id, ok := adminCompanyID(w, r)
	if !ok {
		return
	}
	h.grantSubscription(w, r, models.GrantAdminSubscriptionInput{CompanyUUID: id})
}
func (h *Handler) grantSubscription(w http.ResponseWriter, r *http.Request, in models.GrantAdminSubscriptionInput) {
	actor, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}
	var req dto.GrantAdminSubscriptionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidRequestBody, "invalid request body")
		return
	}
	ends, err := time.Parse(time.RFC3339, req.EndsAt)
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidAdminInput, "invalid ends_at")
		return
	}
	starts := time.Now().UTC()
	if req.StartsAt != nil {
		starts, err = time.Parse(time.RFC3339, *req.StartsAt)
		if err != nil {
			response.WriteError(w, http.StatusBadRequest, response.CodeInvalidAdminInput, "invalid starts_at")
			return
		}
	}
	active := make([]uuid.UUID, 0, len(req.ActiveCompanyUUIDs))
	for _, raw := range req.ActiveCompanyUUIDs {
		id, parseErr := uuid.Parse(strings.TrimSpace(raw))
		if parseErr != nil {
			response.WriteError(w, http.StatusBadRequest, response.CodeInvalidAdminInput, "invalid active_company_uuids")
			return
		}
		active = append(active, id)
	}

	in.ActorUserUUID = actor
	in.PlanCode = models.PlanCode(req.PlanCode)
	in.StartsAt = starts
	in.EndsAt = ends
	in.ActiveCompanyUUIDs = active
	in.Metadata = adminMetadata(r, req.Reason)
	sub, err := h.service.GrantSubscription(r.Context(), in)
	if err != nil {
		// The choice of which companies keep working is the interface's to make,
		// so the answer carries what it needs to ask.
		var selection *models.CompanySelectionRequired
		if errors.As(err, &selection) {
			companies := make([]string, 0, len(selection.CompanyUUIDs))
			for _, id := range selection.CompanyUUIDs {
				companies = append(companies, id.String())
			}
			response.WriteErrorWithDetails(w, http.StatusConflict, response.CodeCompanySelectionRequired, "choose which companies stay active", map[string]any{
				"owner_user_uuid": selection.OwnerUserUUID.String(),
				"company_uuids":   companies,
				"company_limit":   selection.CompanyLimit,
			})
			return
		}
		writeAdminError(w, err, response.CodeFailedToGrantAdminSubscription, "failed to grant subscription")
		return
	}
	_ = response.WriteJSON(w, http.StatusOK, adminSubscriptionResponse(sub))
}
func (h *Handler) CancelPersonalSubscription(w http.ResponseWriter, r *http.Request) {
	id, ok := adminUserID(w, r)
	if !ok {
		return
	}
	h.cancelSubscription(w, r, models.CancelAdminSubscriptionInput{UserUUID: id})
}
func (h *Handler) CancelCompanySubscription(w http.ResponseWriter, r *http.Request) {
	id, ok := adminCompanyID(w, r)
	if !ok {
		return
	}
	h.cancelSubscription(w, r, models.CancelAdminSubscriptionInput{CompanyUUID: id})
}

func (h *Handler) ResetPersonalUsage(w http.ResponseWriter, r *http.Request) {
	h.resetUsage(w, r, true)
}
func (h *Handler) ResetCompanyUsage(w http.ResponseWriter, r *http.Request) {
	h.resetUsage(w, r, false)
}
func (h *Handler) resetUsage(w http.ResponseWriter, r *http.Request, personal bool) {
	actor, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}
	var req dto.AdminReasonRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidRequestBody, "invalid request body")
		return
	}
	input := models.ResetAdminUsageInput{ActorUserUUID: actor, Metadata: adminMetadata(r, req.Reason)}
	var err error
	if personal {
		input.UserUUID, err = uuid.Parse(chi.URLParam(r, "user_uuid"))
	} else {
		input.CompanyUUID, err = uuid.Parse(chi.URLParam(r, "company_uuid"))
	}
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidAdminInput, "invalid target uuid")
		return
	}
	if err = h.service.ResetUsage(r.Context(), input); err != nil {
		writeAdminError(w, err, response.CodeFailedToCancelAdminSubscription, "failed to reset usage")
		return
	}
	response.WriteNoContent(w)
}
func (h *Handler) cancelSubscription(w http.ResponseWriter, r *http.Request, in models.CancelAdminSubscriptionInput) {
	actor, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}
	var req dto.AdminReasonRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidRequestBody, "invalid request body")
		return
	}
	in.ActorUserUUID = actor
	in.Metadata = adminMetadata(r, req.Reason)
	sub, err := h.service.CancelSubscription(r.Context(), in)
	if err != nil {
		writeAdminError(w, err, response.CodeFailedToCancelAdminSubscription, "failed to cancel subscription")
		return
	}
	_ = response.WriteJSON(w, http.StatusOK, adminSubscriptionResponse(sub))
}
func adminCompanyID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "company_uuid"))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidAdminInput, "invalid company uuid")
		return uuid.Nil, false
	}
	return id, true
}
func adminCompanyResponse(c models.AdminCompany) dto.AdminCompanyResponse {
	tag := c.Tag
	if tag == "" {
		tag = "@" + c.ID.String()
	}
	return dto.AdminCompanyResponse{ID: c.ID.String(), Name: c.Name, Tag: tag, ManagerUserUUID: c.ManagerUserUUID.String(), CreatedAt: c.CreatedAt.Format(time.RFC3339)}
}
func adminSubscriptionResponse(s models.AdminSubscription) dto.AdminSubscriptionResponse {
	var user, company, ends *string
	if s.UserUUID.Valid {
		v := s.UserUUID.UUID.String()
		user = &v
	}
	if s.CompanyUUID.Valid {
		v := s.CompanyUUID.UUID.String()
		company = &v
	}
	if s.EndsAt != nil {
		v := s.EndsAt.Format(time.RFC3339)
		ends = &v
	}
	return dto.AdminSubscriptionResponse{ID: s.ID.String(), PlanCode: string(s.PlanCode), Type: string(s.Type), Status: string(s.Status), UserUUID: user, CompanyUUID: company, StartsAt: s.StartsAt.Format(time.RFC3339), EndsAt: ends, CreatedAt: s.CreatedAt.Format(time.RFC3339), UpdatedAt: s.UpdatedAt.Format(time.RFC3339)}
}
