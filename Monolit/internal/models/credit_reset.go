package models

import (
	"time"

	"github.com/google/uuid"
)

type AllowanceResetBatch struct {
	ID                     uuid.UUID     `json:"batch_uuid"`
	OwnerType              string        `json:"owner_type"`
	Status                 string        `json:"status"`
	Reason                 string        `json:"reason"`
	Total                  int           `json:"total_items"`
	Succeeded              int           `json:"succeeded_items"`
	Failed                 int           `json:"failed_items"`
	RequiresSecondApproval bool          `json:"requires_second_approval"`
	RequestedBy            uuid.UUID     `json:"requested_by"`
	ApprovedBy             uuid.NullUUID `json:"approved_by"`
	CreatedAt              time.Time     `json:"created_at"`
	CompletedAt            *time.Time    `json:"completed_at,omitempty"`
}
