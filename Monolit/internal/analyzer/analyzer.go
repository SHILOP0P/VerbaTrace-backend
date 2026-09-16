package analyzer

import (
	"context"

	"verbatrace/monolit/internal/models"
)

type Analyzer interface {
	Provider() string
	Analyze(ctx context.Context, request models.AnalysisRequest) (models.AnalysisResult, error)
}

// CompletionTokenBudgeter exposes the same completion ceiling that an
// analyzer sends to its provider. Billing uses it to reserve a realistic
// worst-case amount instead of charging every analysis against the largest
// model context supported anywhere in the application.
type CompletionTokenBudgeter interface {
	MaximumCompletionTokens(models.AnalysisRequest) int64
}

func MaximumCompletionTokens(value Analyzer, request models.AnalysisRequest) int64 {
	if budgeter, ok := value.(CompletionTokenBudgeter); ok {
		if maximum := budgeter.MaximumCompletionTokens(request); maximum > 0 {
			return maximum
		}
	}
	return 32_768
}
