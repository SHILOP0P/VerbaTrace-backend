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
	var inputRate, outputRate, credit, numerator, denominator int64
	err := r.db.QueryRowContext(ctx, `SELECT r.input_cost_nano_usd_per_token,r.output_cost_nano_usd_per_token,c.credit_micro_usd,c.multiplier_numerator,c.multiplier_denominator FROM pricing_rates r JOIN pricing_catalog_versions c USING(pricing_catalog_version_uuid) WHERE c.status='active' AND c.effective_from<=$1 AND (c.effective_until IS NULL OR c.effective_until>$1) AND r.operation_type='analysis' AND r.provider='openrouter' AND r.model='openai/gpt-5-mini'`, at.UTC()).Scan(&inputRate, &outputRate, &credit, &numerator, &denominator)
	if err != nil {
		return 0, fmt.Errorf("get analysis pricing rate: %w", err)
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
