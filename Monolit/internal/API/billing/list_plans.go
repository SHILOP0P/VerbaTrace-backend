package billing

import (
	"net/http"

	"verbatrace/monolit/internal/API/dto"
	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/converter"
)

func (h *Handler) ListPlans(w http.ResponseWriter, r *http.Request) {
	plans, err := h.service.ListPlans(r.Context())
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToListPlans, "failed to list plans")
		return
	}

	resp := dto.PlansResponse{
		Plans: make([]dto.PlanResponse, 0, len(plans)),
	}

	for _, plan := range plans {
		planResponse, err := converter.PlanModelToAPI(plan)
		if err != nil {
			response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToConvertPlan, "failed to convert plan")
			return
		}
		resp.Plans = append(resp.Plans, planResponse)
	}

	if err := response.WriteJSON(w, http.StatusOK, resp); err != nil {
		return
	}
}
