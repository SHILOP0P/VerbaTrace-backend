package analysis_context

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"calllens/monolit/internal/API/response"
	"calllens/monolit/internal/httpserver/middleware"
	"calllens/monolit/internal/models"

	"github.com/google/uuid"
)

type analysisContextService interface {
	Get(ctx context.Context, userID uuid.UUID, scope models.AnalysisPersonalizationScope, ownerID uuid.UUID, companyID uuid.UUID) (models.AnalysisPersonalization, error)
	Save(ctx context.Context, userID uuid.UUID, input models.SaveAnalysisPersonalizationInput, companyID uuid.UUID) (models.AnalysisPersonalization, error)
}

type Handler struct{ service analysisContextService }

func NewHandler(service analysisContextService) *Handler { return &Handler{service: service} }

func (h *Handler) Get(w http.ResponseWriter, r *http.Request)  { h.handle(w, r, false) }
func (h *Handler) Save(w http.ResponseWriter, r *http.Request) { h.handle(w, r, true) }

func (h *Handler) handle(w http.ResponseWriter, r *http.Request, save bool) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}
	scope := models.AnalysisPersonalizationScope(strings.TrimSpace(r.URL.Query().Get("scope")))
	ownerID, err := uuid.Parse(strings.TrimSpace(r.URL.Query().Get("owner_uuid")))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidRequestBody, "invalid owner uuid")
		return
	}
	companyID, _ := uuid.Parse(strings.TrimSpace(r.URL.Query().Get("company_uuid")))
	var item models.AnalysisPersonalization
	if save {
		var body struct {
			Content string `json:"content"`
		}
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err = decoder.Decode(&body); err != nil {
			response.WriteError(w, http.StatusBadRequest, response.CodeInvalidRequestBody, "invalid request body")
			return
		}
		item, err = h.service.Save(r.Context(), userID, models.SaveAnalysisPersonalizationInput{Scope: scope, OwnerUUID: ownerID, Content: body.Content}, companyID)
	} else {
		item, err = h.service.Get(r.Context(), userID, scope, ownerID, companyID)
	}
	if err != nil {
		switch {
		case errors.Is(err, models.ErrForbidden):
			response.WriteError(w, http.StatusForbidden, response.CodeForbidden, "forbidden")
		case errors.Is(err, models.ErrInvalidAnalysisInput):
			response.WriteError(w, http.StatusBadRequest, response.CodeInvalidRequestBody, "invalid analysis personalization")
		default:
			response.WriteError(w, http.StatusInternalServerError, "analysis_personalization_failed", "analysis personalization operation failed")
		}
		return
	}
	_ = response.WriteJSON(w, http.StatusOK, item)
}
