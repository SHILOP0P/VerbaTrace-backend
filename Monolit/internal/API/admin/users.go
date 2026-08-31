package admin

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"verbatrace/monolit/internal/API/dto"
	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/converter"
	"verbatrace/monolit/internal/httpserver/middleware"
	"verbatrace/monolit/internal/models"

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
)

func (h *Handler) ListUsers(w http.ResponseWriter, r *http.Request) {
	input, err := parseListUsers(r)
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidAdminInput, "invalid admin user filters")
		return
	}
	// The admin directory is administrative metadata, not customer business
	// content. It must remain available to an authenticated administrator so
	// they can identify the subject of a future support-access request.
	result, err := h.service.ListUsers(r.Context(), input)
	if err != nil {
		writeAdminError(w, err, response.CodeFailedToListAdminUsers, "failed to list users")
		return
	}
	items := make([]dto.AdminUserResponse, 0, len(result.Users))
	for _, u := range result.Users {
		items = append(items, adminUserResponse(u))
	}
	_ = response.WriteJSON(w, http.StatusOK, dto.AdminUsersResponse{Items: items, Total: result.Total, Limit: result.Limit, Offset: result.Offset})
}
func (h *Handler) GetUser(w http.ResponseWriter, r *http.Request) {
	id, ok := adminUserID(w, r)
	if !ok {
		return
	}
	user, err := h.service.GetUser(r.Context(), id)
	if err != nil {
		writeAdminError(w, err, response.CodeFailedToGetAdminUser, "failed to get user")
		return
	}
	_ = response.WriteJSON(w, http.StatusOK, adminUserResponse(user))
}
func (h *Handler) ListUserCalls(w http.ResponseWriter, r *http.Request) {
	userID, ok := adminUserID(w, r)
	if !ok {
		return
	}
	if !h.authorizeUser(w, r, userID, "calls") {
		return
	}
	limit, offset := 50, 0
	var err error
	if raw := r.URL.Query().Get("limit"); raw != "" {
		limit, err = strconv.Atoi(raw)
		if err != nil {
			response.WriteError(w, http.StatusBadRequest, response.CodeInvalidAdminInput, "invalid pagination")
			return
		}
	}
	if raw := r.URL.Query().Get("offset"); raw != "" {
		offset, err = strconv.Atoi(raw)
		if err != nil {
			response.WriteError(w, http.StatusBadRequest, response.CodeInvalidAdminInput, "invalid pagination")
			return
		}
	}
	result, err := h.service.ListUserCalls(r.Context(), userID, limit, offset)
	if err != nil {
		writeAdminError(w, err, response.CodeFailedToGetAdminUser, "failed to list user calls")
		return
	}
	items := make([]dto.CallResponse, 0, len(result.Items))
	for _, call := range result.Items {
		item, err := converter.CallModelToAPI(call)
		if err != nil {
			response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToConvertCall, "failed to convert call")
			return
		}
		item.AudioURL = "/api/v1/admin/calls/" + call.ID.String() + "/audio"
		items = append(items, item)
	}
	_ = response.WriteJSON(w, http.StatusOK, dto.CallsListResponse{Items: items, Total: result.Total, Limit: result.Limit, Offset: result.Offset})
}
func (h *Handler) ChangeUserRole(w http.ResponseWriter, r *http.Request) {
	actor, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}
	target, ok := adminUserID(w, r)
	if !ok {
		return
	}
	var req dto.ChangeAdminUserRoleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidRequestBody, "invalid request body")
		return
	}
	user, err := h.service.ChangeUserRole(r.Context(), models.ChangeAdminUserRoleInput{ActorUserUUID: actor, TargetUserUUID: target, ExpectedRole: models.UserRole(req.ExpectedRole), Role: models.UserRole(req.Role), Metadata: adminMetadata(r, req.Reason)})
	if err != nil {
		writeAdminError(w, err, response.CodeFailedToChangeAdminUserRole, "failed to change user role")
		return
	}
	_ = response.WriteJSON(w, http.StatusOK, adminUserResponse(user))
}
func (h *Handler) UpdateUserProfile(w http.ResponseWriter, r *http.Request) {
	actor, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}
	target, ok := adminUserID(w, r)
	if !ok {
		return
	}
	var req dto.UpdateAdminUserProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidRequestBody, "invalid request body")
		return
	}
	user, err := h.service.UpdateUserProfile(r.Context(), models.UpdateAdminUserProfileInput{
		ActorUserUUID: actor, TargetUserUUID: target, FullName: req.FullName, FullSurname: req.FullSurname,
		Username: req.Username, Post: req.Headline,
		Metadata: adminMetadata(r, req.Reason),
	})
	if err != nil {
		writeAdminError(w, err, response.CodeFailedToGetAdminUser, "failed to update user profile")
		return
	}
	_ = response.WriteJSON(w, http.StatusOK, adminUserResponse(user))
}
func (h *Handler) ListUserSessions(w http.ResponseWriter, r *http.Request) {
	actor, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}
	target, ok := adminUserID(w, r)
	if !ok {
		return
	}
	sessions, err := h.service.ListUserSessions(r.Context(), actor, target)
	if err != nil {
		writeAdminError(w, err, response.CodeFailedToListAdminSessions, "failed to list user sessions")
		return
	}
	items := make([]dto.AdminSessionResponse, 0, len(sessions))
	for _, s := range sessions {
		var last *string
		if s.LastSeenAt != nil {
			v := s.LastSeenAt.Format(time.RFC3339)
			last = &v
		}
		items = append(items, dto.AdminSessionResponse{ID: s.ID.String(), UserAgent: s.UserAgent, IP: s.IPAddress, CreatedAt: s.CreatedAt.Format(time.RFC3339), LastSeenAt: last, ExpiresAt: s.ExpiresAt.Format(time.RFC3339)})
	}
	_ = response.WriteJSON(w, http.StatusOK, dto.AdminSessionsResponse{UserUUID: target.String(), Sessions: items})
}
func (h *Handler) RevokeUserSession(w http.ResponseWriter, r *http.Request) {
	h.revokeSession(w, r, false)
}
func (h *Handler) RevokeAllUserSessions(w http.ResponseWriter, r *http.Request) {
	h.revokeSession(w, r, true)
}
func (h *Handler) revokeSession(w http.ResponseWriter, r *http.Request, all bool) {
	actor, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}
	target, ok := adminUserID(w, r)
	if !ok {
		return
	}
	var req dto.AdminReasonRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidRequestBody, "invalid request body")
		return
	}
	input := models.AdminSessionMutationInput{ActorUserUUID: actor, TargetUserUUID: target, Metadata: adminMetadata(r, req.Reason)}
	if !all {
		id, err := uuid.Parse(chi.URLParam(r, "session_uuid"))
		if err != nil {
			response.WriteError(w, http.StatusBadRequest, response.CodeInvalidAdminInput, "invalid session uuid")
			return
		}
		input.SessionUUID = id
	}
	var err error
	if all {
		err = h.service.RevokeAllUserSessions(r.Context(), input)
	} else {
		err = h.service.RevokeUserSession(r.Context(), input)
	}
	if err != nil {
		writeAdminError(w, err, response.CodeFailedToRevokeAdminSession, "failed to revoke user session")
		return
	}
	response.WriteNoContent(w)
}

func parseListUsers(r *http.Request) (models.ListAdminUsersInput, error) {
	q := r.URL.Query()
	limit := 50
	offset := 0
	var err error
	if q.Get("limit") != "" {
		limit, err = strconv.Atoi(q.Get("limit"))
		if err != nil {
			return models.ListAdminUsersInput{}, err
		}
	}
	if q.Get("offset") != "" {
		offset, err = strconv.Atoi(q.Get("offset"))
		if err != nil {
			return models.ListAdminUsersInput{}, err
		}
	}
	in := models.ListAdminUsersInput{Query: q.Get("q"), Limit: limit, Offset: offset}
	if v := q.Get("role"); v != "" {
		r := models.UserRole(v)
		in.Role = &r
	}
	if v := q.Get("subscription_status"); v != "" {
		s := models.AdminSubscriptionStatusFilter(v)
		switch s {
		case models.AdminSubscriptionStatusActive, models.AdminSubscriptionStatusCanceled, models.AdminSubscriptionStatusExpired, models.AdminSubscriptionStatusNone:
			in.SubscriptionStatus = &s
		default:
			return in, errors.New("invalid subscription status")
		}
	}
	if v := q.Get("plan_code"); v != "" {
		p := models.PlanCode(v)
		in.PlanCode = &p
	}
	for _, pair := range []struct {
		raw  string
		dest **time.Time
	}{{q.Get("created_from"), &in.CreatedFrom}, {q.Get("created_to"), &in.CreatedTo}} {
		if pair.raw != "" {
			t, e := time.Parse(time.RFC3339, pair.raw)
			if e != nil {
				return in, e
			}
			*pair.dest = &t
		}
	}
	return in, nil
}
func adminUserID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "user_uuid"))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidAdminInput, "invalid user uuid")
		return uuid.Nil, false
	}
	return id, true
}
func adminUserResponse(u models.AdminUser) dto.AdminUserResponse {
	return dto.AdminUserResponse{ID: u.ID.String(), Email: u.Email, FullName: u.FullName, FullSurname: u.FullSurname, Username: u.Username, Role: string(u.Role), Headline: u.Post, Phone: u.Phone, Timezone: u.Timezone, CreatedAt: u.CreatedAt.Format(time.RFC3339)}
}
func adminMetadata(r *http.Request, reason string) models.AdminMutationMetadata {
	var ip *string
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		ip = &host
	} else if net.ParseIP(strings.TrimSpace(r.RemoteAddr)) != nil {
		v := strings.TrimSpace(r.RemoteAddr)
		ip = &v
	}
	var agent *string
	if v := strings.TrimSpace(r.UserAgent()); v != "" {
		agent = &v
	}
	rid := chiMiddleware.GetReqID(r.Context())
	var requestID *string
	if rid != "" {
		requestID = &rid
	}
	return models.AdminMutationMetadata{Reason: reason, RequestID: requestID, IPAddress: ip, UserAgent: agent}
}
func writeAdminError(w http.ResponseWriter, err error, fallbackCode, fallbackMessage string) {
	switch {
	case errors.Is(err, models.ErrInvalidAdminInput), errors.Is(err, models.ErrInvalidUserRole), errors.Is(err, models.ErrAdminReasonRequired):
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidAdminInput, "invalid admin input")
	case errors.Is(err, models.ErrUserNotFound):
		response.WriteError(w, http.StatusNotFound, response.CodeUserNotFound, "user not found")
	case errors.Is(err, models.ErrCompanyNotFound):
		response.WriteError(w, http.StatusNotFound, response.CodeCompanyNotFound, "company not found")
	case errors.Is(err, models.ErrSubscriptionNotFound):
		response.WriteError(w, http.StatusNotFound, response.CodeSubscriptionNotFound, "subscription not found")
	case errors.Is(err, models.ErrPlanNotFound), errors.Is(err, models.ErrInvalidBillingInput):
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidBillingInput, "invalid billing input")
	case errors.Is(err, models.ErrForbidden), errors.Is(err, models.ErrRoleTransitionForbidden), errors.Is(err, models.ErrProtectedSuperAdmin), errors.Is(err, models.ErrCannotChangeOwnRole), errors.Is(err, models.ErrAdminSessionManagementForbidden):
		response.WriteError(w, http.StatusForbidden, response.CodeForbidden, "forbidden")
	case errors.Is(err, models.ErrUserRoleChanged):
		response.WriteError(w, http.StatusConflict, response.CodeAdminUserRoleChanged, "user role changed")
	case errors.Is(err, models.ErrRefreshSessionNotFound):
		response.WriteError(w, http.StatusNotFound, response.CodeRefreshSessionNotFound, "session not found")
	default:
		response.WriteError(w, http.StatusInternalServerError, fallbackCode, fallbackMessage)
	}
}
