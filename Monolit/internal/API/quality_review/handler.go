package quality_review

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/httpserver/middleware"
	"verbatrace/monolit/internal/models"
	"verbatrace/monolit/internal/service/qualityreview"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type Handler struct{ service *qualityreview.Service }

func NewHandler(service *qualityreview.Service) *Handler { return &Handler{service: service} }

type createRequest struct {
	AnalysisUUID            string     `json:"analysis_uuid"`
	ReviewedSubjectUserUUID *string    `json:"reviewed_subject_user_uuid"`
	AssigneeUserUUID        *string    `json:"assignee_user_uuid"`
	DueAt                   *time.Time `json:"due_at"`
}
type claimRequest struct {
	ExpectedVersion int64 `json:"expected_version"`
}
type draftRequest struct {
	OverallComment string                         `json:"overall_comment"`
	Payload        json.RawMessage                `json:"payload"`
	Criteria       []qualityreview.CriterionInput `json:"criteria"`
}
type publishRequest struct {
	DraftRevisionUUID string `json:"draft_revision_uuid"`
}
type appealRequest struct {
	Reason string `json:"reason"`
}
type challengeRequest struct {
	AnalysisUUID string `json:"analysis_uuid"`
	Reason       string `json:"reason"`
}
type resolveRequest struct {
	Status                  models.QualityReviewAppealStatus `json:"status"`
	Comment                 string                           `json:"comment"`
	ReplacementRevisionUUID *string                          `json:"replacement_revision_uuid"`
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	actor, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		writeUnauthorized(w)
		return
	}
	callID, err := uuid.Parse(chi.URLParam(r, "uuid"))
	if err != nil {
		writeError(w, qualityreview.ErrInvalidInput)
		return
	}
	var req createRequest
	if decode(r, &req) != nil {
		writeError(w, qualityreview.ErrInvalidInput)
		return
	}
	analysisID, err := uuid.Parse(req.AnalysisUUID)
	if err != nil {
		writeError(w, qualityreview.ErrInvalidInput)
		return
	}
	subject, err := optionalUUID(req.ReviewedSubjectUserUUID)
	if err != nil {
		writeError(w, qualityreview.ErrInvalidInput)
		return
	}
	assignee, err := optionalUUID(req.AssigneeUserUUID)
	if err != nil {
		writeError(w, qualityreview.ErrInvalidInput)
		return
	}
	item, err := h.service.Create(r.Context(), qualityreview.CreateInput{CallUUID: callID, AnalysisUUID: analysisID, SubjectUserUUID: subject, AssigneeUserUUID: assignee, DueAt: req.DueAt, ActorUserUUID: actor})
	if err != nil {
		writeError(w, err)
		return
	}
	_ = response.WriteJSON(w, http.StatusCreated, item)
}

func (h *Handler) GetAnalysisContext(w http.ResponseWriter, r *http.Request) {
	actor, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		writeUnauthorized(w)
		return
	}
	callID, err := uuid.Parse(chi.URLParam(r, "uuid"))
	if err != nil {
		writeError(w, qualityreview.ErrInvalidInput)
		return
	}
	analysisID, err := uuid.Parse(r.URL.Query().Get("analysis_uuid"))
	if err != nil {
		writeError(w, qualityreview.ErrInvalidInput)
		return
	}
	item, err := h.service.GetAnalysisContext(r.Context(), callID, analysisID, actor)
	if err != nil {
		writeError(w, err)
		return
	}
	_ = response.WriteJSON(w, http.StatusOK, item)
}

func (h *Handler) ChallengeAnalysis(w http.ResponseWriter, r *http.Request) {
	actor, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		writeUnauthorized(w)
		return
	}
	callID, err := uuid.Parse(chi.URLParam(r, "uuid"))
	if err != nil {
		writeError(w, qualityreview.ErrInvalidInput)
		return
	}
	var req challengeRequest
	if decode(r, &req) != nil {
		writeError(w, qualityreview.ErrInvalidInput)
		return
	}
	analysisID, err := uuid.Parse(req.AnalysisUUID)
	if err != nil {
		writeError(w, qualityreview.ErrInvalidInput)
		return
	}
	item, err := h.service.ChallengeAnalysis(r.Context(), qualityreview.ChallengeInput{CallUUID: callID, AnalysisUUID: analysisID, ActorUserUUID: actor, Reason: req.Reason})
	if err != nil {
		writeError(w, err)
		return
	}
	_ = response.WriteJSON(w, http.StatusCreated, item)
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	actor, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		writeUnauthorized(w)
		return
	}
	company, _ := queryUUID(r, "company_uuid")
	department, _ := queryUUID(r, "department_uuid")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	items, err := h.service.List(r.Context(), qualityreview.ListInput{ActorUserUUID: actor, CompanyUUID: company, DepartmentUUID: department, Status: models.QualityReviewStatus(r.URL.Query().Get("status")), Limit: limit, Offset: offset})
	if err != nil {
		writeError(w, err)
		return
	}
	_ = response.WriteJSON(w, http.StatusOK, map[string]any{"items": items, "limit": limitOrDefault(limit), "offset": offset})
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	actor, id, ok := ids(r)
	if !ok {
		writeError(w, qualityreview.ErrInvalidInput)
		return
	}
	item, err := h.service.Get(r.Context(), id, actor)
	if err != nil {
		writeError(w, err)
		return
	}
	_ = response.WriteJSON(w, http.StatusOK, item)
}
func (h *Handler) Claim(w http.ResponseWriter, r *http.Request) {
	actor, id, ok := ids(r)
	if !ok {
		writeError(w, qualityreview.ErrInvalidInput)
		return
	}
	var req claimRequest
	if decode(r, &req) != nil {
		writeError(w, qualityreview.ErrInvalidInput)
		return
	}
	item, err := h.service.Claim(r.Context(), id, actor, req.ExpectedVersion)
	if err != nil {
		writeError(w, err)
		return
	}
	_ = response.WriteJSON(w, http.StatusOK, item)
}
func (h *Handler) SaveDraft(w http.ResponseWriter, r *http.Request) {
	actor, id, ok := ids(r)
	if !ok {
		writeError(w, qualityreview.ErrInvalidInput)
		return
	}
	version, ok := ifMatch(r)
	if !ok {
		writeError(w, qualityreview.ErrVersionConflict)
		return
	}
	var req draftRequest
	if decode(r, &req) != nil {
		writeError(w, qualityreview.ErrInvalidInput)
		return
	}
	item, err := h.service.SaveDraft(r.Context(), qualityreview.DraftInput{ReviewUUID: id, ActorUserUUID: actor, ExpectedVersion: version, OverallComment: req.OverallComment, Payload: req.Payload, Criteria: req.Criteria})
	if err != nil {
		writeError(w, err)
		return
	}
	_ = response.WriteJSON(w, http.StatusOK, item)
}
func (h *Handler) DiscardDraft(w http.ResponseWriter, r *http.Request) {
	actor, id, ok := ids(r)
	if !ok {
		writeError(w, qualityreview.ErrInvalidInput)
		return
	}
	version, ok := ifMatch(r)
	if !ok {
		writeError(w, qualityreview.ErrVersionConflict)
		return
	}
	item, err := h.service.DiscardDraft(r.Context(), id, actor, version)
	if err != nil {
		writeError(w, err)
		return
	}
	_ = response.WriteJSON(w, http.StatusOK, item)
}
func (h *Handler) Publish(w http.ResponseWriter, r *http.Request) {
	actor, id, ok := ids(r)
	if !ok {
		writeError(w, qualityreview.ErrInvalidInput)
		return
	}
	version, ok := ifMatch(r)
	if !ok {
		writeError(w, qualityreview.ErrVersionConflict)
		return
	}
	var req publishRequest
	if decode(r, &req) != nil {
		writeError(w, qualityreview.ErrInvalidInput)
		return
	}
	draft, err := uuid.Parse(req.DraftRevisionUUID)
	if err != nil {
		writeError(w, qualityreview.ErrInvalidInput)
		return
	}
	item, err := h.service.Publish(r.Context(), id, draft, actor, version)
	if err != nil {
		writeError(w, err)
		return
	}
	_ = response.WriteJSON(w, http.StatusOK, item)
}
func (h *Handler) CreateAppeal(w http.ResponseWriter, r *http.Request) {
	actor, id, ok := ids(r)
	if !ok {
		writeError(w, qualityreview.ErrInvalidInput)
		return
	}
	var req appealRequest
	if decode(r, &req) != nil {
		writeError(w, qualityreview.ErrInvalidInput)
		return
	}
	item, err := h.service.CreateAppeal(r.Context(), qualityreview.AppealInput{ReviewUUID: id, ActorUserUUID: actor, Reason: req.Reason})
	if err != nil {
		writeError(w, err)
		return
	}
	_ = response.WriteJSON(w, http.StatusCreated, item)
}
func (h *Handler) ResolveAppeal(w http.ResponseWriter, r *http.Request) {
	actor, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		writeUnauthorized(w)
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "appeal_uuid"))
	if err != nil {
		writeError(w, qualityreview.ErrInvalidInput)
		return
	}
	var req resolveRequest
	if decode(r, &req) != nil {
		writeError(w, qualityreview.ErrInvalidInput)
		return
	}
	replacement, parseErr := optionalUUID(req.ReplacementRevisionUUID)
	if parseErr != nil {
		writeError(w, qualityreview.ErrInvalidInput)
		return
	}
	item, err := h.service.ResolveAppeal(r.Context(), qualityreview.ResolveAppealInput{AppealUUID: id, ActorUserUUID: actor, Status: req.Status, Comment: req.Comment, ReplacementRevisionUUID: replacement})
	if err != nil {
		writeError(w, err)
		return
	}
	_ = response.WriteJSON(w, http.StatusOK, item)
}

func (h *Handler) ListEvents(w http.ResponseWriter, r *http.Request) {
	actor, id, ok := ids(r)
	if !ok {
		writeError(w, qualityreview.ErrInvalidInput)
		return
	}
	items, err := h.service.ListEvents(r.Context(), id, actor)
	if err != nil {
		writeError(w, err)
		return
	}
	_ = response.WriteJSON(w, http.StatusOK, map[string]any{"events": items})
}

func ids(r *http.Request) (uuid.UUID, uuid.UUID, bool) {
	actor, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		return uuid.Nil, uuid.Nil, false
	}
	id, err := uuid.Parse(chi.URLParam(r, "review_uuid"))
	return actor, id, err == nil
}
func decode(r *http.Request, v any) error {
	d := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	d.DisallowUnknownFields()
	return d.Decode(v)
}
func optionalUUID(v *string) (uuid.NullUUID, error) {
	if v == nil || strings.TrimSpace(*v) == "" {
		return uuid.NullUUID{}, nil
	}
	id, err := uuid.Parse(*v)
	return uuid.NullUUID{UUID: id, Valid: err == nil}, err
}
func queryUUID(r *http.Request, key string) (uuid.NullUUID, error) {
	v := r.URL.Query().Get(key)
	return optionalUUID(&v)
}
func ifMatch(r *http.Request) (int64, bool) {
	v := strings.Trim(r.Header.Get("If-Match"), "\"")
	n, err := strconv.ParseInt(v, 10, 64)
	return n, err == nil && n > 0
}
func limitOrDefault(v int) int {
	if v <= 0 {
		return 25
	}
	if v > 100 {
		return 100
	}
	return v
}
func writeUnauthorized(w http.ResponseWriter) {
	response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
}
func writeError(w http.ResponseWriter, err error) {
	status, code, message := http.StatusInternalServerError, "quality_review_failed", "failed to process quality review"
	switch {
	case errors.Is(err, qualityreview.ErrNotFound):
		status, code, message = http.StatusNotFound, "quality_review_not_found", "quality review not found"
	case errors.Is(err, qualityreview.ErrForbidden):
		status, code, message = http.StatusForbidden, "quality_review_forbidden", "quality review action is forbidden"
	case errors.Is(err, qualityreview.ErrInvalidInput):
		status, code, message = http.StatusUnprocessableEntity, "quality_review_invalid_input", "invalid quality review input"
	case errors.Is(err, qualityreview.ErrAlreadyExists):
		status, code, message = http.StatusConflict, "quality_review_already_exists", "quality review already exists"
	case errors.Is(err, qualityreview.ErrAlreadyClaimed):
		status, code, message = http.StatusConflict, "quality_review_already_claimed", "quality review already claimed"
	case errors.Is(err, qualityreview.ErrVersionConflict):
		status, code, message = http.StatusConflict, "quality_review_version_conflict", "quality review was changed"
	case errors.Is(err, qualityreview.ErrSourceOutdated):
		status, code, message = http.StatusConflict, "quality_review_source_outdated", "source analysis is outdated"
	case errors.Is(err, qualityreview.ErrPublicationBlocked):
		status, code, message = http.StatusUnprocessableEntity, "quality_review_publication_blocked", "quality review is incomplete"
	case errors.Is(err, qualityreview.ErrConflictOfInterest):
		status, code, message = http.StatusForbidden, "quality_review_conflict_of_interest", "reviewer cannot review their own call or revision"
	case errors.Is(err, qualityreview.ErrReviewLimitReached):
		status, code, message = http.StatusConflict, "quality_review_limit_reached", "quality review limit reached"
	case errors.Is(err, qualityreview.ErrReviewerMustDiffer):
		status, code, message = http.StatusConflict, "quality_review_reviewer_must_differ", "the next review requires another reviewer"
	case errors.Is(err, qualityreview.ErrActiveAppealExists):
		status, code, message = http.StatusConflict, "quality_review_active_appeal_exists", "an appeal for the active revision already exists"
	}
	response.WriteError(w, status, code, message)
}
