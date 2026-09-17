package billing

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

type creditOperationRepository interface {
	ReserveCredits(context.Context, models.Subscription, models.ReserveCreditsInput, time.Time) (models.CreditOperation, error)
	SettleCredits(context.Context, models.SettleCreditsInput, time.Time) (models.CreditOperation, error)
	MaximumTranscriptionCredits(context.Context, int64, string, time.Time) (int64, error)
	TranscriptionProviderCostNanoUSD(context.Context, uuid.UUID, int64) (int64, error)
	MaximumAnalysisCredits(context.Context, int64, int64, time.Time) (int64, error)
	MaximumGenerationCredits(context.Context, int64, int64, string, string, time.Time) (int64, error)
	MaximumEmbeddingCredits(context.Context, int64, string, string, time.Time) (int64, error)
	CreditsForOperationProviderCost(context.Context, uuid.UUID, int64) (int64, error)
	MarkCreditOperationProviderRunning(context.Context, uuid.UUID) error
	MarkCreditOperationReconciling(context.Context, uuid.UUID, string) error
	IntegrationBillingContextForCall(context.Context, uuid.UUID) (uuid.NullUUID, string, error)
	IsSandboxMockCall(context.Context, uuid.UUID) (bool, error)
}

// ReserveAssistantGeneration books what an assistant answer may cost. The
// department matters: a department limit is meant to stop the assistant too, and
// leaving it out was what made the assistant the one operation no department cap
// could reach.
func (s *Service) ReserveAssistantGeneration(ctx context.Context, userID, companyID, departmentID, runID uuid.UUID, inputTokens, maxOutputTokens int64, provider, model string) (uuid.UUID, error) {
	if runID == uuid.Nil || userID == uuid.Nil || inputTokens < 0 || maxOutputTokens <= 0 || provider == "" || model == "" {
		return uuid.Nil, models.ErrInvalidBillingInput
	}
	subscription, err := s.subscriptionForScope(ctx, userID, companyID)
	if err != nil {
		return uuid.Nil, err
	}
	maximum, err := s.creditOperations.MaximumGenerationCredits(ctx, inputTokens, maxOutputTokens, provider, model, s.now())
	if err != nil {
		return uuid.Nil, err
	}
	operationID := uuid.NewSHA1(uuid.NameSpaceOID, []byte("assistant_generation:"+runID.String()))
	_, err = s.creditOperations.ReserveCredits(ctx, subscription, models.ReserveCreditsInput{
		OperationUUID:  operationID,
		CompanyUUID:    uuid.NullUUID{UUID: companyID, Valid: companyID != uuid.Nil},
		DepartmentUUID: uuid.NullUUID{UUID: departmentID, Valid: departmentID != uuid.Nil},
		OperationType:  "assistant_generation", Environment: "production",
		Provider: provider, Model: model, Mode: "chat", IdempotencyKey: "assistant_generation:" + runID.String(), MaximumCharge: maximum,
	}, s.now())
	if err == nil {
		err = s.creditOperations.MarkCreditOperationProviderRunning(ctx, operationID)
	}
	return operationID, err
}

// ReserveEmbedding books an embedding call: indexing a call for semantic search,
// or turning a search query into a vector. Both hit a paid provider.
func (s *Service) ReserveEmbedding(ctx context.Context, userID, companyID, departmentID uuid.UUID, reference string, inputTokens int64, provider, model string) (uuid.UUID, error) {
	if strings.TrimSpace(reference) == "" || inputTokens < 0 || provider == "" || model == "" {
		return uuid.Nil, models.ErrInvalidBillingInput
	}
	subscription, err := s.subscriptionForScope(ctx, userID, companyID)
	if err != nil {
		return uuid.Nil, err
	}
	maximum, err := s.creditOperations.MaximumEmbeddingCredits(ctx, inputTokens, provider, model, s.now())
	if err != nil {
		return uuid.Nil, err
	}
	key := "embedding:" + reference
	operationID := uuid.NewSHA1(uuid.NameSpaceOID, []byte(key))
	_, err = s.creditOperations.ReserveCredits(ctx, subscription, models.ReserveCreditsInput{
		OperationUUID:  operationID,
		CompanyUUID:    uuid.NullUUID{UUID: companyID, Valid: companyID != uuid.Nil},
		DepartmentUUID: uuid.NullUUID{UUID: departmentID, Valid: departmentID != uuid.Nil},
		OperationType:  "embedding", Environment: "production",
		Provider: provider, Model: model, IdempotencyKey: key, MaximumCharge: maximum,
	}, s.now())
	if err == nil {
		err = s.creditOperations.MarkCreditOperationProviderRunning(ctx, operationID)
	}
	return operationID, err
}

// SettleEmbedding charges what the provider actually cost. Embeddings report no
// usage breakdown, so the reserved maximum is the charge.
func (s *Service) SettleEmbedding(ctx context.Context, operationID uuid.UUID, inputTokens int64, provider, model string) error {
	cost, err := s.creditOperations.MaximumEmbeddingCredits(ctx, inputTokens, provider, model, s.now())
	if err != nil {
		return err
	}
	usage, _ := json.Marshal(map[string]any{"input_tokens": inputTokens, "model": model})
	_, err = s.creditOperations.SettleCredits(ctx, models.SettleCreditsInput{OperationUUID: operationID, ActualChargeCredits: cost, ProviderUsageJSON: usage}, s.now())
	return err
}

func (s *Service) subscriptionForScope(ctx context.Context, userID, companyID uuid.UUID) (models.Subscription, error) {
	if companyID != uuid.Nil {
		return s.repository.GetActiveBusinessSubscription(ctx, companyID)
	}
	return s.repository.GetActivePersonalSubscription(ctx, userID)
}

func (s *Service) SettleAssistantGeneration(ctx context.Context, operationID uuid.UUID, usage *models.ProviderUsage) error {
	return s.SettleAnalysis(ctx, operationID, usage)
}

func (s *Service) IsSandboxMockCall(ctx context.Context, callID uuid.UUID) (bool, error) {
	return s.creditOperations.IsSandboxMockCall(ctx, callID)
}

func (s *Service) ReserveTranscription(ctx context.Context, call models.Call, mode models.TranscriptionMode) (uuid.UUID, error) {
	subscription, err := s.subscriptionForCall(ctx, call)
	if err != nil {
		return uuid.Nil, err
	}
	maximum, err := s.creditOperations.MaximumTranscriptionCredits(ctx, int64(call.DurationSeconds), string(mode), s.now())
	if err != nil {
		return uuid.Nil, err
	}
	operationID := uuid.NewSHA1(uuid.NameSpaceOID, []byte("transcription:"+call.ID.String()))
	applicationID, environment, err := s.creditOperations.IntegrationBillingContextForCall(ctx, call.ID)
	if err != nil {
		return uuid.Nil, err
	}
	_, err = s.creditOperations.ReserveCredits(ctx, subscription, models.ReserveCreditsInput{OperationUUID: operationID, ApplicationUUID: applicationID, CallUUID: uuid.NullUUID{UUID: call.ID, Valid: true}, CompanyUUID: call.CompanyUUID, DepartmentUUID: call.DepartmentUUID, OperationType: "transcription", Environment: environment, Provider: "assemblyai", Model: "universal-2", Mode: string(mode), IdempotencyKey: "transcription:" + call.ID.String(), MaximumCharge: maximum}, s.now())
	if err == nil {
		err = s.creditOperations.MarkCreditOperationProviderRunning(ctx, operationID)
	}
	return operationID, err
}

func (s *Service) SettleTranscription(ctx context.Context, operationID uuid.UUID, call models.Call, mode models.TranscriptionMode) error {
	cost, err := s.creditOperations.TranscriptionProviderCostNanoUSD(ctx, operationID, int64(call.DurationSeconds))
	if err != nil {
		return err
	}
	credits, err := s.creditOperations.CreditsForOperationProviderCost(ctx, operationID, cost)
	if err != nil {
		return err
	}
	usage, _ := json.Marshal(map[string]any{"duration_seconds": call.DurationSeconds, "mode": mode})
	_, err = s.creditOperations.SettleCredits(ctx, models.SettleCreditsInput{OperationUUID: operationID, ActualChargeCredits: credits, ProviderCostNanoUSD: cost, ProviderUsageJSON: usage}, s.now())
	return err
}

func (s *Service) ReserveAnalysis(ctx context.Context, call models.Call, analysisID uuid.UUID, transcription string, maxOutputTokens int64) (uuid.UUID, error) {
	subscription, err := s.subscriptionForCall(ctx, call)
	if err != nil {
		return uuid.Nil, err
	}
	inputTokens := (len([]byte(transcription)) + 2) / 3
	if maxOutputTokens <= 0 {
		return uuid.Nil, models.ErrInvalidBillingInput
	}
	maximum, err := s.creditOperations.MaximumAnalysisCredits(ctx, int64(inputTokens), int64(maxOutputTokens), s.now())
	if err != nil {
		return uuid.Nil, err
	}
	operationID := uuid.NewSHA1(uuid.NameSpaceOID, []byte("analysis:"+analysisID.String()))
	applicationID, environment, err := s.creditOperations.IntegrationBillingContextForCall(ctx, call.ID)
	if err != nil {
		return uuid.Nil, err
	}
	_, err = s.creditOperations.ReserveCredits(ctx, subscription, models.ReserveCreditsInput{OperationUUID: operationID, ApplicationUUID: applicationID, CallUUID: uuid.NullUUID{UUID: call.ID, Valid: true}, CompanyUUID: call.CompanyUUID, DepartmentUUID: call.DepartmentUUID, OperationType: "analysis", Environment: environment, Provider: "openrouter", Model: "openai/gpt-5-mini", IdempotencyKey: "analysis:" + analysisID.String(), MaximumCharge: maximum}, s.now())
	if err == nil {
		err = s.creditOperations.MarkCreditOperationProviderRunning(ctx, operationID)
	}
	return operationID, err
}

func (s *Service) MarkCreditOperationReconciling(ctx context.Context, id uuid.UUID, reason string) error {
	return s.creditOperations.MarkCreditOperationReconciling(ctx, id, reason)
}

func (s *Service) SettleAnalysis(ctx context.Context, operationID uuid.UUID, usage *models.ProviderUsage) error {
	if usage == nil {
		return fmt.Errorf("analysis provider usage missing")
	}
	credits, err := s.creditOperations.CreditsForOperationProviderCost(ctx, operationID, usage.CostNanoUSD)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(usage)
	if err != nil {
		return err
	}
	_, err = s.creditOperations.SettleCredits(ctx, models.SettleCreditsInput{OperationUUID: operationID, ActualChargeCredits: credits, ProviderCostNanoUSD: usage.CostNanoUSD, ProviderUsageJSON: payload}, s.now())
	return err
}

func (s *Service) subscriptionForCall(ctx context.Context, call models.Call) (models.Subscription, error) {
	if call.CompanyUUID.Valid {
		return s.repository.GetActiveBusinessSubscription(ctx, call.CompanyUUID.UUID)
	}
	if call.UploadedByUserUUID.Valid {
		return s.repository.GetActivePersonalSubscription(ctx, call.UploadedByUserUUID.UUID)
	}
	return models.Subscription{}, models.ErrSubscriptionNotFound
}
