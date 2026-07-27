package models

import (
	"time"

	"github.com/google/uuid"
)

type AnalysisPersonalizationScope string

const (
	AnalysisPersonalizationScopePersonal   AnalysisPersonalizationScope = "personal"
	AnalysisPersonalizationScopeCompany    AnalysisPersonalizationScope = "company"
	AnalysisPersonalizationScopeDepartment AnalysisPersonalizationScope = "department"
)

type AnalysisPersonalization struct {
	Scope     AnalysisPersonalizationScope `json:"scope"`
	OwnerUUID uuid.UUID                    `json:"owner_uuid"`
	Content   string                       `json:"content"`
	UpdatedAt time.Time                    `json:"updated_at"`
}

type SaveAnalysisPersonalizationInput struct {
	Scope     AnalysisPersonalizationScope
	OwnerUUID uuid.UUID
	Content   string
}
