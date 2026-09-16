package converter

import (
	"time"

	"verbatrace/monolit/internal/API/dto"
	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func CreditSpendingModelToAPI(spending models.CreditSpending) dto.CreditSpendingResponse {
	return dto.CreditSpendingResponse{
		ID:              spending.SubjectUUID.String(),
		Name:            spending.SubjectName,
		LimitCredits:    spending.LimitCredits,
		UsedCredits:     spending.UsedCredits,
		ForecastCredits: spending.ForecastCredits,
		PeriodStart:     spending.PeriodStart.UTC().Format(time.RFC3339),
		PeriodEnd:       spending.PeriodEnd.UTC().Format(time.RFC3339),
	}
}

func CompanyCreditForecastModelToAPI(forecast models.CompanyCreditForecast) dto.CompanyCreditForecastResponse {
	departments := make([]dto.CreditSpendingResponse, len(forecast.Departments))
	for i, department := range forecast.Departments {
		departments[i] = CreditSpendingModelToAPI(department)
	}

	payload := dto.CompanyCreditForecastResponse{Departments: departments}
	// A leader sees their own departments only, so there is no company line.
	if forecast.Company.SubjectUUID != uuid.Nil {
		company := CreditSpendingModelToAPI(forecast.Company)
		payload.Company = &company
	}

	return payload
}

func CompanyLifecycleModelToAPI(lifecycle models.CompanyLifecycle) dto.CompanyLifecycleResponse {
	return dto.CompanyLifecycleResponse{
		CompanyUUID:   lifecycle.CompanyUUID.String(),
		State:         string(lifecycle.State),
		FrozenAt:      optionalTimestamp(lifecycle.FrozenAt),
		SoftDeletedAt: optionalTimestamp(lifecycle.SoftDeletedAt),
		PurgeAfter:    optionalTimestamp(lifecycle.PurgeAfter),
		RestoreUsed:   lifecycle.RestoreUsed,
	}
}

func optionalTimestamp(value *time.Time) *string {
	if value == nil {
		return nil
	}
	formatted := value.UTC().Format(time.RFC3339)
	return &formatted
}
