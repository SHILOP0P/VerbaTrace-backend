package billing

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"verbatrace/monolit/internal/billingcredits"
)

func (r *Repository) MaximumTranscriptionCredits(ctx context.Context, durationSeconds int64, mode string, at time.Time) (int64, error) {
	var rate, credit, numerator, denominator int64
	err := r.db.QueryRowContext(ctx, `SELECT r.provider_cost_nano_usd_per_unit,c.credit_micro_usd,c.multiplier_numerator,c.multiplier_denominator FROM pricing_rates r JOIN pricing_catalog_versions c USING(pricing_catalog_version_uuid) WHERE c.status='active' AND c.effective_from<=$1 AND (c.effective_until IS NULL OR c.effective_until>$1) AND r.operation_type='transcription' AND r.provider='assemblyai' AND r.model='universal-2' AND r.mode=$2`, at.UTC(), mode).Scan(&rate, &credit, &numerator, &denominator)
	if err != nil {
		return 0, fmt.Errorf("get transcription pricing rate: %w", err)
	}
	return billingcredits.CreditsForAudioWithPolicy(durationSeconds, rate, credit, numerator, denominator)
}

func (r *Repository) TranscriptionProviderCostNanoUSD(ctx context.Context, operationID uuid.UUID, durationSeconds int64) (int64, error) {
	var rate int64
	err := r.db.QueryRowContext(ctx, `SELECT r.provider_cost_nano_usd_per_unit FROM usage_operations o JOIN pricing_rates r ON r.pricing_catalog_version_uuid=o.pricing_catalog_version_uuid AND r.operation_type=o.operation_type AND r.provider=o.provider AND r.model=o.model AND r.mode=o.mode WHERE o.usage_operation_uuid=$1`, operationID).Scan(&rate)
	if err != nil {
		return 0, err
	}
	return (durationSeconds*rate + 3599) / 3600, nil
}

func (r *Repository) MaximumAnalysisCredits(ctx context.Context, inputTokens, outputTokens int64, at time.Time) (int64, error) {
	return r.MaximumGenerationCredits(ctx, inputTokens, outputTokens, "openrouter", "openai/gpt-5-mini", at)
}

func (r *Repository) MaximumGenerationCredits(ctx context.Context, inputTokens, outputTokens int64, provider, model string, at time.Time) (int64, error) {
	return r.maximumTokenCredits(ctx, "analysis", inputTokens, outputTokens, provider, model, at)
}

// MaximumEmbeddingCredits prices indexing and semantic search queries. They call
// a paid provider like any other model operation and used to sit outside the
// credit system entirely.
func (r *Repository) MaximumEmbeddingCredits(ctx context.Context, inputTokens int64, provider, model string, at time.Time) (int64, error) {
	return r.maximumTokenCredits(ctx, "embedding", inputTokens, 0, provider, model, at)
}

func (r *Repository) maximumTokenCredits(ctx context.Context, operationType string, inputTokens, outputTokens int64, provider, model string, at time.Time) (int64, error) {
	var inputRate, outputRate, credit, numerator, denominator int64
	err := r.db.QueryRowContext(ctx, `SELECT r.input_cost_nano_usd_per_token,r.output_cost_nano_usd_per_token,c.credit_micro_usd,c.multiplier_numerator,c.multiplier_denominator FROM pricing_rates r JOIN pricing_catalog_versions c USING(pricing_catalog_version_uuid) WHERE c.status='active' AND c.effective_from<=$1 AND (c.effective_until IS NULL OR c.effective_until>$1) AND r.operation_type=$4 AND r.provider=$2 AND r.model=$3`, at.UTC(), provider, model, operationType).Scan(&inputRate, &outputRate, &credit, &numerator, &denominator)
	if err != nil {
		return 0, fmt.Errorf("get %s pricing rate for %s/%s: %w", operationType, provider, model, err)
	}
	return billingcredits.CreditsFromProviderCostNanoUSDWithPolicy(inputTokens*inputRate+outputTokens*outputRate, credit, numerator, denominator)
}

func (r *Repository) CreditsForOperationProviderCost(ctx context.Context, operationID uuid.UUID, costNanoUSD int64) (int64, error) {
	var credit, numerator, denominator int64
	err := r.db.QueryRowContext(ctx, `SELECT c.credit_micro_usd,c.multiplier_numerator,c.multiplier_denominator FROM usage_operations o JOIN pricing_catalog_versions c USING(pricing_catalog_version_uuid) WHERE o.usage_operation_uuid=$1`, operationID).Scan(&credit, &numerator, &denominator)
	if err != nil {
		return 0, err
	}
	return billingcredits.CreditsFromProviderCostNanoUSDWithPolicy(costNanoUSD, credit, numerator, denominator)
}
