package converter

import (
	"time"

	"verbatrace/monolit/internal/API/dto"
	"verbatrace/monolit/internal/models"
)

func AnalysisRerunRequestModelToAPI(request models.AnalysisRerunRequest) dto.AnalysisRerunRequestResponse {
	item := dto.AnalysisRerunRequestResponse{
		ID:                  request.ID.String(),
		CallUUID:            request.CallUUID.String(),
		CompanyUUID:         request.CompanyUUID.String(),
		DepartmentUUID:      nullUUIDToStringPtr(request.DepartmentUUID),
		RequestedByUserUUID: request.RequestedByUserUUID.String(),
		Reason:              request.Reason,
		Status:              string(request.Status),
		DecidedByUserUUID:   nullUUIDToStringPtr(request.DecidedByUserUUID),
		DecidedAt:           optionalTimestamp(request.DecidedAt),
		Comment:             request.Comment,
		CreatedAt:           request.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:           request.UpdatedAt.UTC().Format(time.RFC3339),
	}

	return item
}

func AnalysisRerunRequestsModelToAPI(requests []models.AnalysisRerunRequest) dto.AnalysisRerunRequestsResponse {
	items := make([]dto.AnalysisRerunRequestResponse, len(requests))
	for i, request := range requests {
		items[i] = AnalysisRerunRequestModelToAPI(request)
	}

	return dto.AnalysisRerunRequestsResponse{Items: items}
}
