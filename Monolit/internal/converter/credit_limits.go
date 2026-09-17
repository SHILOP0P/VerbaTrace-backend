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
		FreezeReason:  optionalFreezeReason(lifecycle.FreezeReason),
		FrozenAt:      optionalTimestamp(lifecycle.FrozenAt),
		SoftDeletedAt: optionalTimestamp(lifecycle.SoftDeletedAt),
		PurgeAfter:    optionalTimestamp(lifecycle.PurgeAfter),
		RestoreUsed:   lifecycle.RestoreUsed,
	}
}

// optionalFreezeReason tells the interface whether a frozen company is waiting
// for its plan or on its way out; the two look the same otherwise.
func optionalFreezeReason(value models.CompanyFreezeReason) *string {
	if value == "" {
		return nil
	}
	formatted := string(value)
	return &formatted
}

func optionalTimestamp(value *time.Time) *string {
	if value == nil {
		return nil
	}
	formatted := value.UTC().Format(time.RFC3339)
	return &formatted
}
