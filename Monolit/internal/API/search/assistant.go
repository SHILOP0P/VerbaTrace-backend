package search

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"verbatrace/monolit/internal/API/response"
	assistantservice "verbatrace/monolit/internal/assistant"
	"verbatrace/monolit/internal/httpserver/middleware"
	"verbatrace/monolit/internal/models"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func (h *Handler) AssistantCapabilities(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.WriteError(w, 401, response.CodeUnauthorized, "unauthorized")
		return
	}
	company, err := parseAssistantCompany(r.URL.Query().Get("company_uuid"))
	if err != nil || h.assistant == nil {
		response.WriteError(w, 400, "invalid_assistant_input", "invalid assistant input")
		return
	}
	result, err := h.assistant.Capabilities(r.Context(), user, company)
	if err != nil {
		writeAssistantError(w, err)
		return
	}
	_ = response.WriteJSON(w, 200, result)
}

func (h *Handler) ContentSearch(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.WriteError(w, 401, response.CodeUnauthorized, "unauthorized")
		return
	}
	q := r.URL.Query()
	company, err := parseAssistantCompany(q.Get("company_uuid"))
	if err != nil {
		writeAssistantError(w, assistantservice.ErrInvalidInput)
		return
	}
	limit, _ := strconv.Atoi(q.Get("limit"))
	input := models.ContentSearchInput{UserUUID: user, CompanyUUID: company, Query: q.Get("q"), Limit: limit}
	input.CallIDs, err = parseUUIDList(q.Get("call_uuids"))
	if err != nil {
		writeAssistantError(w, assistantservice.ErrInvalidInput)
		return
	}
	input.DepartmentIDs, err = parseUUIDList(q.Get("department_uuids"))
	if err != nil {
		writeAssistantError(w, assistantservice.ErrInvalidInput)
		return
	}
	input.FolderIDs, err = parseUUIDList(q.Get("folder_uuids"))
	if err != nil {
		writeAssistantError(w, assistantservice.ErrInvalidInput)
		return
	}
	if input.From, err = parseTime(q.Get("from")); err != nil {
		writeAssistantError(w, assistantservice.ErrInvalidInput)
		return
	}
	if input.To, err = parseTime(q.Get("to")); err != nil {
		writeAssistantError(w, assistantservice.ErrInvalidInput)
		return
	}
	result, err := h.assistant.ContentSearch(r.Context(), input)
	if err != nil {
		writeAssistantError(w, err)
		return
	}
	_ = response.WriteJSON(w, 200, result)
}

func (h *Handler) ListAssistantChats(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.WriteError(w, 401, response.CodeUnauthorized, "unauthorized")
		return
	}
	company, err := parseAssistantCompany(r.URL.Query().Get("company_uuid"))
	if err != nil {
		writeAssistantError(w, assistantservice.ErrInvalidInput)
		return
	}
	items, err := h.assistant.ListChats(r.Context(), user, company)
	if err != nil {
		writeAssistantError(w, err)
		return
	}
	_ = response.WriteJSON(w, 200, map[string]any{"items": items})
}

func (h *Handler) CreateAssistantChat(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.WriteError(w, 401, response.CodeUnauthorized, "unauthorized")
		return
	}
	var body struct {
		CompanyUUID    string `json:"company_uuid"`
		Title          string `json:"title"`
		ResponseDetail string `json:"response_detail"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeAssistantError(w, assistantservice.ErrInvalidInput)
		return
	}
	company, err := parseAssistantCompany(body.CompanyUUID)
	if err != nil {
		writeAssistantError(w, assistantservice.ErrInvalidInput)
		return
	}
	item, err := h.assistant.CreateChat(r.Context(), user, company, body.Title, body.ResponseDetail)
	if err != nil {
		writeAssistantError(w, err)
		return
	}
	_ = response.WriteJSON(w, 201, item)
}

func (h *Handler) ListAssistantMessages(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.WriteError(w, 401, response.CodeUnauthorized, "unauthorized")
		return
	}
	chat, err := uuid.Parse(chi.URLParam(r, "chat_uuid"))
	if err != nil {
		writeAssistantError(w, assistantservice.ErrNotFound)
		return
	}
	items, err := h.assistant.ListMessages(r.Context(), user, chat)
	if err != nil {
		writeAssistantError(w, err)
		return
	}
	_ = response.WriteJSON(w, 200, map[string]any{"items": items})
}

func (h *Handler) CreateAssistantMessage(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.WriteError(w, 401, response.CodeUnauthorized, "unauthorized")
		return
	}
	chat, err := uuid.Parse(chi.URLParam(r, "chat_uuid"))
	if err != nil {
		writeAssistantError(w, assistantservice.ErrNotFound)
		return
	}
	var body struct {
		CompanyUUID     string     `json:"company_uuid"`
		Text            string     `json:"text"`
		ClientMessageID string     `json:"client_message_id"`
		IdempotencyKey  string     `json:"idempotency_key"`
		ResponseDetail  string     `json:"response_detail"`
		CallIDs         []string   `json:"call_uuids"`
		DepartmentIDs   []string   `json:"department_uuids"`
		FolderIDs       []string   `json:"folder_uuids"`
		ContextLabels   []string   `json:"context_labels"`
		From            *time.Time `json:"from"`
		To              *time.Time `json:"to"`
	}
	if err = decodeBody(r, &body); err != nil {
		writeAssistantError(w, assistantservice.ErrInvalidInput)
		return
	}
	company, err := parseAssistantCompany(body.CompanyUUID)
	if err != nil {
		writeAssistantError(w, assistantservice.ErrInvalidInput)
		return
	}
	calls, err := parseUUIDSlice(body.CallIDs)
	if err != nil {
		writeAssistantError(w, assistantservice.ErrInvalidInput)
		return
	}
	deps, err := parseUUIDSlice(body.DepartmentIDs)
	if err != nil {
		writeAssistantError(w, assistantservice.ErrInvalidInput)
		return
	}
	folders, err := parseUUIDSlice(body.FolderIDs)
	if err != nil {
		writeAssistantError(w, assistantservice.ErrInvalidInput)
		return
	}
	run, err := h.assistant.CreateMessage(r.Context(), models.CreateAssistantMessageInput{UserUUID: user, CompanyUUID: company, ChatUUID: chat, Text: body.Text, ClientMessageID: body.ClientMessageID, IdempotencyKey: body.IdempotencyKey, ResponseDetail: body.ResponseDetail, CallIDs: calls, DepartmentIDs: deps, FolderIDs: folders, ContextLabels: body.ContextLabels, From: body.From, To: body.To})
	if err != nil {
		writeAssistantError(w, err)
		return
	}
	_ = response.WriteJSON(w, 202, run)
}

func (h *Handler) GetAssistantRun(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.WriteError(w, 401, response.CodeUnauthorized, "unauthorized")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "run_uuid"))
	if err != nil {
		writeAssistantError(w, assistantservice.ErrNotFound)
		return
	}
	run, err := h.assistant.GetRun(r.Context(), user, id)
	if err != nil {
		writeAssistantError(w, err)
		return
	}
	_ = response.WriteJSON(w, 200, run)
}

func parseUUIDList(raw string) ([]uuid.UUID, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	return parseUUIDSlice(strings.Split(raw, ","))
}
func parseUUIDSlice(raw []string) ([]uuid.UUID, error) {
	out := make([]uuid.UUID, 0, len(raw))
	for _, s := range raw {
		id, err := uuid.Parse(strings.TrimSpace(s))
		if err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, nil
}
func parseTime(raw string) (*time.Time, error) {
	if raw == "" {
		return nil, nil
	}
	v, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil, err
	}
	return &v, nil
}
func decodeBody(r *http.Request, target any) error {
	defer func() { _ = r.Body.Close() }()
	decoder := json.NewDecoder(io.LimitReader(r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}
func writeAssistantError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, assistantservice.ErrInvalidInput):
		response.WriteError(w, 400, "invalid_assistant_input", "invalid assistant input")
	case errors.Is(err, assistantservice.ErrForbidden):
		response.WriteError(w, 403, "assistant_role_required", "assistant access is not available")
	case errors.Is(err, assistantservice.ErrNotFound):
		response.WriteError(w, 404, "resource_not_found", "resource not found")
	case errors.Is(err, assistantservice.ErrRunInProgress):
		response.WriteError(w, 409, "run_in_progress", "an assistant run is already in progress")
	case errors.Is(err, assistantservice.ErrVersionConflict):
		response.WriteError(w, 409, "assistant_draft_conflict", "assistant draft was changed in another session")
	case errors.Is(err, assistantservice.ErrProviderUnavailable):
		response.WriteError(w, 503, "assistant_unavailable", "assistant provider is unavailable")
	default:
		response.WriteError(w, 500, "assistant_failed", "assistant request failed")
	}
}

func (h *Handler) GetAssistantDraft(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.WriteError(w, 401, response.CodeUnauthorized, "unauthorized")
		return
	}
	company, err := parseAssistantCompany(r.URL.Query().Get("company_uuid"))
	if err != nil {
		writeAssistantError(w, assistantservice.ErrInvalidInput)
		return
	}
	chat := uuid.Nil
	if raw := r.URL.Query().Get("chat_uuid"); raw != "" {
		chat, err = uuid.Parse(raw)
		if err != nil {
			writeAssistantError(w, assistantservice.ErrInvalidInput)
			return
		}
	}
	draft, err := h.assistant.GetDraft(r.Context(), user, company, chat)
	if err != nil {
		writeAssistantError(w, err)
		return
	}
	_ = response.WriteJSON(w, 200, draft)
}

func (h *Handler) SaveAssistantDraft(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.WriteError(w, 401, response.CodeUnauthorized, "unauthorized")
		return
	}
	var body struct {
		CompanyUUID string          `json:"company_uuid"`
		ChatUUID    string          `json:"chat_uuid"`
		Text        string          `json:"text"`
		Context     json.RawMessage `json:"context"`
		LockVersion int64           `json:"lock_version"`
	}
	if decodeBody(r, &body) != nil {
		writeAssistantError(w, assistantservice.ErrInvalidInput)
		return
	}
	company, err := parseAssistantCompany(body.CompanyUUID)
	if err != nil {
		writeAssistantError(w, assistantservice.ErrInvalidInput)
		return
	}
	chat := uuid.Nil
	if body.ChatUUID != "" {
		chat, err = uuid.Parse(body.ChatUUID)
		if err != nil {
			writeAssistantError(w, assistantservice.ErrInvalidInput)
			return
		}
	}
	if len(body.Context) == 0 {
		body.Context = json.RawMessage(`{}`)
	}
	draft, err := h.assistant.SaveDraft(r.Context(), user, company, chat, body.Text, body.Context, body.LockVersion)
	if err != nil {
		writeAssistantError(w, err)
		return
	}
	_ = response.WriteJSON(w, 200, draft)
}

func (h *Handler) DeleteAssistantDraft(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.WriteError(w, 401, response.CodeUnauthorized, "unauthorized")
		return
	}
	company, err := parseAssistantCompany(r.URL.Query().Get("company_uuid"))
	if err != nil {
		writeAssistantError(w, assistantservice.ErrInvalidInput)
		return
	}
	chat := uuid.Nil
	if raw := r.URL.Query().Get("chat_uuid"); raw != "" {
		chat, err = uuid.Parse(raw)
		if err != nil {
			writeAssistantError(w, assistantservice.ErrInvalidInput)
			return
		}
	}
	if err = h.assistant.DeleteDraft(r.Context(), user, company, chat); err != nil {
		writeAssistantError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func parseAssistantCompany(raw string) (uuid.UUID, error) {
	if raw == "" {
		return uuid.Nil, nil
	}
	id, err := uuid.Parse(raw)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, assistantservice.ErrInvalidInput
	}
	return id, nil
}

func (h *Handler) DeleteAssistantChat(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.WriteError(w, 401, response.CodeUnauthorized, "unauthorized")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "chat_uuid"))
	if err != nil {
		writeAssistantError(w, assistantservice.ErrNotFound)
		return
	}
	if err = h.assistant.DeleteChat(r.Context(), user, id); err != nil {
		writeAssistantError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) ExportAssistantChat(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.WriteError(w, 401, response.CodeUnauthorized, "unauthorized")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "chat_uuid"))
	if err != nil {
		writeAssistantError(w, assistantservice.ErrNotFound)
		return
	}
	data, filename, err := h.assistant.ExportChatMarkdown(r.Context(), user, id)
	if err != nil {
		writeAssistantError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}
