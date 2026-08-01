package converter

import (
	"time"

	"verbatrace/monolit/internal/API/dto"
	"verbatrace/monolit/internal/models"
)

func SearchModelToAPI(result models.SearchResult) dto.SearchResponse {
	resp := dto.SearchResponse{
		Calls:        make([]dto.SearchCallResponse, len(result.Calls)),
		Companies:    make([]dto.SearchCompanyResponse, len(result.Companies)),
		Reports:      make([]dto.SearchReportResponse, len(result.Reports)),
		Instructions: make([]dto.SearchInstructionResponse, len(result.Instructions)),
	}
	for i, item := range result.Calls {
		resp.Calls[i] = dto.SearchCallResponse{
			ID:        item.ID.String(),
			Title:     item.Title,
			Status:    string(item.Status),
			CreatedAt: item.CreatedAt.UTC().Format(time.RFC3339),
		}
	}
	for i, item := range result.Companies {
		resp.Companies[i] = dto.SearchCompanyResponse{
			ID:   item.ID.String(),
			Name: item.Name,
		}
	}
	for i, item := range result.Reports {
		resp.Reports[i] = dto.SearchReportResponse{
			ID:       item.ID.String(),
			CallUUID: item.CallUUID.String(),
			FileName: item.FileName,
			Status:   string(item.Status),
		}
	}
	for i, item := range result.Instructions {
		resp.Instructions[i] = dto.SearchInstructionResponse{
			ID:    item.ID.String(),
			Title: item.Title,
			Scope: string(item.Scope),
		}
	}
	return resp
}
