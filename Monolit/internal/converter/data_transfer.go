package converter

import (
	"time"

	"verbatrace/monolit/internal/API/dto"
	"verbatrace/monolit/internal/models"
)

// CompanyDataTransferModelToAPI describes what a move between two of the owner's
// companies did. The counts are the point of it: an owner emptying a company
// before it is deleted has to see that the data actually arrived.
func CompanyDataTransferModelToAPI(transfer models.TransferCompanyDataResult) dto.CompanyDataTransferResponse {
	return dto.CompanyDataTransferResponse{
		ID:                transfer.ID.String(),
		SourceCompanyUUID: transfer.SourceCompanyUUID.String(),
		TargetCompanyUUID: transfer.TargetCompanyUUID.String(),
		Calls:             transfer.Calls,
		Folders:           transfer.Folders,
		Reason:            transfer.Reason,
		CreatedAt:         transfer.CreatedAt.UTC().Format(time.RFC3339),
	}
}
