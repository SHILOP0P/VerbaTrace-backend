package converter

import (
	"time"

	"verbatrace/monolit/internal/API/dto"
	"verbatrace/monolit/internal/models"
)

func DeletedCallModelToAPI(deleted models.DeletedCall) (dto.DeletedCallResponse, error) {
	call, err := CallModelToAPI(deleted.Call)
	if err != nil {
		return dto.DeletedCallResponse{}, err
	}

	return dto.DeletedCallResponse{
		Call:              call,
		DeletedAt:         deleted.DeletedAt.UTC().Format(time.RFC3339),
		PurgeAfter:        deleted.PurgeAfter.UTC().Format(time.RFC3339),
		DeletedByUserUUID: nullUUIDToStringPtr(deleted.DeletedByUserUUID),
	}, nil
}

func DeletedCallsListModelToAPI(result models.ListDeletedCallsResult) (dto.DeletedCallsListResponse, error) {
	items := make([]dto.DeletedCallResponse, len(result.Items))
	for i, deleted := range result.Items {
		item, err := DeletedCallModelToAPI(deleted)
		if err != nil {
			return dto.DeletedCallsListResponse{}, err
		}
		items[i] = item
	}

	return dto.DeletedCallsListResponse{
		Items:  items,
		Total:  result.Total,
		Limit:  result.Limit,
		Offset: result.Offset,
	}, nil
}
