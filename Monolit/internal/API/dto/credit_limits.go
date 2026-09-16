package dto

// SetCreditLimitRequest carries the new cap. A null limit removes the cap,
// zero forbids spending.
type SetCreditLimitRequest struct {
	LimitCredits *int64 `json:"limit_credits"`
}

type CreditSpendingResponse struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	LimitCredits    *int64 `json:"limit_credits"`
	UsedCredits     int64  `json:"used_credits"`
	ForecastCredits int64  `json:"forecast_credits"`
	PeriodStart     string `json:"period_start"`
	PeriodEnd       string `json:"period_end"`
}

type CompanyCreditForecastResponse struct {
	Company     *CreditSpendingResponse  `json:"company,omitempty"`
	Departments []CreditSpendingResponse `json:"departments"`
}
