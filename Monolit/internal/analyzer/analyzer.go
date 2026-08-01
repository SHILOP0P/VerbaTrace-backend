package analyzer

import (
	"context"

	"verbatrace/monolit/internal/models"
)

type Analyzer interface {
	Provider() string
	Analyze(ctx context.Context, request models.AnalysisRequest) (models.AnalysisResult, error)
	AnalyzeAggregate(ctx context.Context, request models.AggregateAnalysisRequest) (models.AnalysisResult, error)
}
