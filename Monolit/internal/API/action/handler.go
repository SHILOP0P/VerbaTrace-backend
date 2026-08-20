package action

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/httpserver/middleware"
	actionservice "verbatrace/monolit/internal/service/action"
)

type Handler struct{ service *actionservice.Service }

func NewHandler(service *actionservice.Service) *Handler { return &Handler{service: service} }

type createRequest struct {
	AnalysisUUID         string                        `json:"analysis_uuid"`
	SourceDepartmentUUID string                        `json:"source_department_uuid"`
	TargetDepartmentUUID string                        `json:"target_department_uuid"`
	AssigneeUserUUID     string                        `json:"assignee_user_uuid"`
	Title                string                        `json:"title"`
	Description          string                        `json:"description"`
	DueAt                string                        `json:"due_at"`
	Evidence             []actionservice.EvidenceInput `json:"evidence"`
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	actor, ok := user(r)
	if !ok {
		writeErr(w, actionservice.ErrForbidden)
		return
	}
	callID, err := uuid.Parse(chi.URLParam(r, "uuid"))
	if err != nil {
		writeErr(w, actionservice.ErrInvalidInput)
		return
	}
	var req createRequest
	if decode(r, &req) != nil {
		writeErr(w, actionservice.ErrInvalidInput)
		return
	}
	analysis, err := uuid.Parse(req.AnalysisUUID)
	if err != nil {
		writeErr(w, actionservice.ErrInvalidInput)
		return
	}
	var source, target uuid.UUID
	if strings.TrimSpace(req.SourceDepartmentUUID) != "" {
		source, err = uuid.Parse(req.SourceDepartmentUUID)
		if err != nil {
			writeErr(w, actionservice.ErrInvalidInput)
			return
		}
	}
	if strings.TrimSpace(req.TargetDepartmentUUID) != "" {
		target, err = uuid.Parse(req.TargetDepartmentUUID)
		if err != nil {
			writeErr(w, actionservice.ErrInvalidInput)
			return
		}
	}
	var assignee uuid.UUID
	if strings.TrimSpace(req.AssigneeUserUUID) != "" {
		assignee, err = uuid.Parse(req.AssigneeUserUUID)
		if err != nil {
			writeErr(w, actionservice.ErrInvalidInput)
			return
		}
	}
	due, err := time.Parse(time.RFC3339, req.DueAt)
	if err != nil {
		writeErr(w, actionservice.ErrInvalidInput)
		return
	}
	item, err := h.service.Create(r.Context(), actionservice.CreateInput{ActorUserUUID: actor, CallUUID: callID, AnalysisUUID: analysis, SourceDepartment: source, TargetDepartment: target, AssigneeUserUUID: assignee, Title: req.Title, Description: req.Description, DueAt: due, Evidence: req.Evidence, IdempotencyKey: r.Header.Get("Idempotency-Key")})
	if err != nil {
		writeErr(w, err)
		return
	}
	_ = response.WriteJSON(w, http.StatusCreated, item)
}

func (h *Handler) SetDisposition(w http.ResponseWriter, r *http.Request) {
	actor, ok := user(r)
	if !ok {
		writeErr(w, actionservice.ErrForbidden)
		return
	}
	callID, e1 := uuid.Parse(chi.URLParam(r, "uuid"))
	analysisID, e2 := uuid.Parse(chi.URLParam(r, "analysis_uuid"))
	var req struct {
		Kind   string `json:"kind"`
		Reason string `json:"reason"`
	}
	if e1 != nil || e2 != nil || decode(r, &req) != nil || req.Kind != "no_action_required" {
		writeErr(w, actionservice.ErrInvalidInput)
		return
	}
	if err := h.service.SetNoActionRequired(r.Context(), actor, callID, analysisID, req.Reason); err != nil {
		writeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request)      { h.get(w, r, false) }
func (h *Handler) GetAdmin(w http.ResponseWriter, r *http.Request) { h.get(w, r, true) }
func (h *Handler) get(w http.ResponseWriter, r *http.Request, admin bool) {
	actor, ok := user(r)
	id, err := uuid.Parse(chi.URLParam(r, "action_uuid"))
	if !ok || err != nil {
		writeErr(w, actionservice.ErrInvalidInput)
		return
	}
	item, err := h.service.Get(r.Context(), id, actor, admin)
	if err != nil {
		writeErr(w, err)
		return
	}
	_ = response.WriteJSON(w, http.StatusOK, item)
}
func (h *Handler) List(w http.ResponseWriter, r *http.Request)      { h.list(w, r, false) }
func (h *Handler) ListAdmin(w http.ResponseWriter, r *http.Request) { h.list(w, r, true) }
func (h *Handler) list(w http.ResponseWriter, r *http.Request, admin bool) {
	actor, ok := user(r)
	if !ok {
		writeErr(w, actionservice.ErrForbidden)
		return
	}
	q := r.URL.Query()
	in := actionservice.ListInput{ActorUserUUID: actor, Status: q.Get("status"), Query: strings.TrimSpace(q.Get("q")), Mine: q.Get("mine") == "true", Limit: intQuery(q.Get("limit"), 25), Offset: intQuery(q.Get("offset"), 0), Admin: admin}
	in.CompanyTag = strings.TrimSpace(q.Get("company_tag"))
	in.Department = strings.TrimSpace(q.Get("department"))
	in.CompanyUUID = parseNullUUID(q.Get("company_uuid"))
	in.CallUUID = parseNullUUID(q.Get("call_uuid"))
	in.DepartmentUUID = parseNullUUID(q.Get("department_uuid"))
	in.AssigneeUUID = parseNullUUID(q.Get("assignee_uuid"))
	result, err := h.service.List(r.Context(), in)
	if err != nil {
		writeErr(w, err)
		return
	}
	_ = response.WriteJSON(w, http.StatusOK, result)
}

func (h *Handler) ListAssignees(w http.ResponseWriter, r *http.Request) {
	h.listAssignees(w, r, false)
}

func (h *Handler) ListAssigneesAdmin(w http.ResponseWriter, r *http.Request) {
	h.listAssignees(w, r, true)
}

func (h *Handler) listAssignees(w http.ResponseWriter, r *http.Request, admin bool) {
	actor, ok := user(r)
	company, err := uuid.Parse(chi.URLParam(r, "uuid"))
	if !ok || err != nil {
		writeErr(w, actionservice.ErrInvalidInput)
		return
	}
	items, err := h.service.ListAssignees(r.Context(), actor, company, r.URL.Query().Get("q"), parseNullUUID(r.URL.Query().Get("department_uuid")), admin)
	if err != nil {
		writeErr(w, err)
		return
	}
	_ = response.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

type mutationRequest struct {
	ExpectedLockVersion  int64  `json:"expected_lock_version"`
	Reason               string `json:"reason"`
	DueAt                string `json:"due_at"`
	AssigneeUserUUID     string `json:"assignee_user_uuid"`
	TargetDepartmentUUID string `json:"target_department_uuid"`
}

func (h *Handler) Start(w http.ResponseWriter, r *http.Request)      { h.mutate(w, r, "start") }
func (h *Handler) Complete(w http.ResponseWriter, r *http.Request)   { h.mutate(w, r, "complete") }
func (h *Handler) Cancel(w http.ResponseWriter, r *http.Request)     { h.mutate(w, r, "cancel") }
func (h *Handler) Reschedule(w http.ResponseWriter, r *http.Request) { h.mutate(w, r, "reschedule") }
func (h *Handler) Reassign(w http.ResponseWriter, r *http.Request)   { h.mutate(w, r, "reassign") }
func (h *Handler) Reopen(w http.ResponseWriter, r *http.Request)     { h.mutate(w, r, "reopen") }
func (h *Handler) CompleteAdmin(w http.ResponseWriter, r *http.Request) {
	h.mutateWithAdmin(w, r, "complete", true)
}
func (h *Handler) CancelAdmin(w http.ResponseWriter, r *http.Request) {
	h.mutateWithAdmin(w, r, "cancel", true)
}
func (h *Handler) RescheduleAdmin(w http.ResponseWriter, r *http.Request) {
	h.mutateWithAdmin(w, r, "reschedule", true)
}
func (h *Handler) ReassignAdmin(w http.ResponseWriter, r *http.Request) {
	h.mutateWithAdmin(w, r, "reassign", true)
}
func (h *Handler) ReopenAdmin(w http.ResponseWriter, r *http.Request) {
	h.mutateWithAdmin(w, r, "reopen", true)
}
func (h *Handler) mutate(w http.ResponseWriter, r *http.Request, kind string) {
	h.mutateWithAdmin(w, r, kind, false)
}
func (h *Handler) mutateWithAdmin(w http.ResponseWriter, r *http.Request, kind string, admin bool) {
	actor, ok := user(r)
	id, err := uuid.Parse(chi.URLParam(r, "action_uuid"))
	var req mutationRequest
	if !ok || err != nil || decode(r, &req) != nil || req.ExpectedLockVersion <= 0 {
		writeErr(w, actionservice.ErrInvalidInput)
		return
	}
	base := actionservice.UpdateInput{ActorUserUUID: actor, ActionUUID: id, ExpectedVersion: req.ExpectedLockVersion, Reason: req.Reason, Admin: admin}
	var item actionservice.Item
	switch kind {
	case "start":
		item, err = h.service.Start(r.Context(), base)
	case "complete":
		item, err = h.service.Complete(r.Context(), base)
	case "cancel":
		item, err = h.service.Cancel(r.Context(), base)
	case "reschedule":
		var due time.Time
		due, err = time.Parse(time.RFC3339, req.DueAt)
		if err == nil {
			item, err = h.service.Reschedule(r.Context(), actionservice.RescheduleInput{UpdateInput: base, DueAt: due})
		}
	case "reassign":
		var assignee, department uuid.UUID
		assignee, err = uuid.Parse(req.AssigneeUserUUID)
		if err == nil {
			department, err = uuid.Parse(req.TargetDepartmentUUID)
		}
		if err == nil {
			item, err = h.service.Reassign(r.Context(), actionservice.ReassignInput{UpdateInput: base, AssigneeUserUUID: assignee, TargetDepartmentUUID: department})
		}
	case "reopen":
		item, err = h.service.Reopen(r.Context(), base)
	}
	if err != nil {
		writeErr(w, err)
		return
	}
	_ = response.WriteJSON(w, http.StatusOK, item)
}

func (h *Handler) CreateTransfer(w http.ResponseWriter, r *http.Request) {
	actor, ok := user(r)
	id, err := uuid.Parse(chi.URLParam(r, "action_uuid"))
	var req struct {
		Reason             string `json:"reason"`
		ProposedAssignee   string `json:"proposed_assignee_user_uuid"`
		ProposedDepartment string `json:"proposed_department_uuid"`
	}
	if !ok || err != nil || decode(r, &req) != nil {
		writeErr(w, actionservice.ErrInvalidInput)
		return
	}
	item, err := h.service.CreateTransfer(r.Context(), actionservice.TransferInput{ActorUserUUID: actor, ActionUUID: id, Reason: req.Reason, ProposedAssignee: parseNullUUID(req.ProposedAssignee), ProposedDepartment: parseNullUUID(req.ProposedDepartment)})
	if err != nil {
		writeErr(w, err)
		return
	}
	_ = response.WriteJSON(w, http.StatusCreated, item)
}
func (h *Handler) ApproveTransfer(w http.ResponseWriter, r *http.Request) {
	h.resolveTransfer(w, r, true)
}
func (h *Handler) RejectTransfer(w http.ResponseWriter, r *http.Request) {
	h.resolveTransfer(w, r, false)
}
func (h *Handler) resolveTransfer(w http.ResponseWriter, r *http.Request, approve bool) {
	actor, ok := user(r)
	actionID, e1 := uuid.Parse(chi.URLParam(r, "action_uuid"))
	requestID, e2 := uuid.Parse(chi.URLParam(r, "request_uuid"))
	var req struct {
		ExpectedLockVersion int64  `json:"expected_lock_version"`
		Comment             string `json:"comment"`
	}
	if !ok || e1 != nil || e2 != nil || decode(r, &req) != nil {
		writeErr(w, actionservice.ErrInvalidInput)
		return
	}
	item, err := h.service.ResolveTransfer(r.Context(), actionservice.ResolveTransferInput{ActorUserUUID: actor, ActionUUID: actionID, RequestUUID: requestID, Approve: approve, Comment: req.Comment, ExpectedVersion: req.ExpectedLockVersion})
	if err != nil {
		writeErr(w, err)
		return
	}
	_ = response.WriteJSON(w, http.StatusOK, item)
}

func user(r *http.Request) (uuid.UUID, bool) { return middleware.UserIDFromContext(r.Context()) }
func decode(r *http.Request, v any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}
func parseNullUUID(v string) uuid.NullUUID {
	id, err := uuid.Parse(strings.TrimSpace(v))
	return uuid.NullUUID{UUID: id, Valid: err == nil}
}
func intQuery(v string, d int) int {
	n, err := strconv.Atoi(v)
	if err != nil {
		return d
	}
	return n
}
func writeErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, actionservice.ErrInvalidInput):
		response.WriteError(w, http.StatusUnprocessableEntity, response.CodeInvalidActionInput, "invalid action input")
	case errors.Is(err, actionservice.ErrNotFound):
		response.WriteError(w, http.StatusNotFound, response.CodeActionNotFound, "action not found")
	case errors.Is(err, actionservice.ErrForbidden):
		response.WriteError(w, http.StatusForbidden, response.CodeActionForbidden, "action forbidden")
	case errors.Is(err, actionservice.ErrConflict):
		response.WriteError(w, http.StatusConflict, response.CodeActionConflict, "action conflict")
	default:
		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToProcessAction, "failed to process action")
	}
}
