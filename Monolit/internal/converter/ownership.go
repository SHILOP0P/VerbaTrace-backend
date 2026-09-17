package converter

import (
	"time"

	"verbatrace/monolit/internal/API/dto"
	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

// OwnershipTransferModelToAPI describes an offer to hand over a company. The
// scope matters to the interface: a single-company offer names its company,
// while an "all" offer only makes sense as the list it resolves to.
func OwnershipTransferModelToAPI(transfer models.CompanyOwnershipTransfer) dto.CompanyOwnershipTransferResponse {
	response := dto.CompanyOwnershipTransferResponse{
		ID:               transfer.ID.String(),
		Scope:            string(transfer.Scope),
		CompanyUUIDs:     uuidsToStrings(transfer.CompanyUUIDs),
		StayCompanyUUIDs: uuidsToStrings(transfer.StayCompanyUUIDs),
		FromUser:         transfer.FromUserUUID.String(),
		ToUser:           transfer.ToUserUUID.String(),
		Status:           string(transfer.Status),
		Reason:           transfer.Reason,
		CreatedAt:        transfer.CreatedAt.Format(time.RFC3339),
		ExpiresAt:        transfer.ExpiresAt.Format(time.RFC3339),
	}
	if transfer.CompanyUUID.Valid {
		value := transfer.CompanyUUID.UUID.String()
		response.CompanyUUID = &value
	}

	return response
}

func uuidsToStrings(ids []uuid.UUID) []string {
	values := make([]string, 0, len(ids))
	for _, id := range ids {
		values = append(values, id.String())
	}

	return values
}
