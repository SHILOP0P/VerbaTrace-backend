package converter

import (
	"time"

	"verbatrace/monolit/internal/API/dto"
	"verbatrace/monolit/internal/models"
)

func CallFolderModelToAPI(folder models.CallFolder) dto.CallFolderResponse {
	instructions := make([]dto.AnalysisInstruction, 0, len(folder.Instructions))
	for _, instruction := range folder.Instructions {
		item, err := AnalysisInstructionModelToAPI(instruction)
		if err == nil {
			instructions = append(instructions, item)
		}
	}
	return dto.CallFolderResponse{
		ID:                folder.ID.String(),
		Scope:             string(folder.Scope),
		UserUUID:          nullUUIDToStringPtr(folder.UserUUID),
		CompanyUUID:       nullUUIDToStringPtr(folder.CompanyUUID),
		DepartmentUUID:    nullUUIDToStringPtr(folder.DepartmentUUID),
		Name:              folder.Name,
		Description:       folder.Description,
		Color:             folder.Color,
		CallsCount:        folder.CallsCount,
		CreatedByUserUUID: folder.CreatedByUserUUID.String(),
		CreatedAt:         folder.CreatedAt.Format(time.RFC3339),
		UpdatedAt:         folder.UpdatedAt.Format(time.RFC3339),
		Instructions:      instructions,
	}
}

func CallFoldersListModelToAPI(result models.ListCallFoldersResult) dto.CallFoldersListResponse {
	items := make([]dto.CallFolderResponse, len(result.Items))
	for i, folder := range result.Items {
		items[i] = CallFolderModelToAPI(folder)
	}
	return dto.CallFoldersListResponse{Items: items, Total: result.Total, Limit: result.Limit, Offset: result.Offset}
}
