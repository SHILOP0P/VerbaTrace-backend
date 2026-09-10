package billing

import (
	"context"
	"encoding/json"
	"fmt"
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
	CreditsForOperationProviderCost(context.Context, uuid.UUID, int64) (int64, error)
	MarkCreditOperationProviderRunning(context.Context, uuid.UUID) error
	MarkCreditOperationReconciling(context.Context, uuid.UUID, string) error
	IntegrationBillingContextForCall(context.Context, uuid.UUID) (uuid.NullUUID, string, error)
	IsSandboxMockCall(context.Context, uuid.UUID) (bool, error)
}

func (s *Service) ReserveAssistantGeneration(ctx context.Context, userID, companyID, runID uuid.UUID, inputTokens, maxOutputTokens int64, provider, model string) (uuid.UUID, error) {
	if runID == uuid.Nil || userID == uuid.Nil || inputTokens < 0 || maxOutputTokens <= 0 || provider == "" || model == "" {
		return uuid.Nil, models.ErrInvalidBillingInput
	}
	var subscription models.Subscription
	var err error
	if companyID != uuid.Nil {
		subscription, err = s.repository.GetActiveBusinessSubscription(ctx, companyID)
	} else {
		subscription, err = s.repository.GetActivePersonalSubscription(ctx, userID)
	}
	if err != nil {
		return uuid.Nil, err
	}
	maximum, err := s.creditRepository.(creditOperationRepository).MaximumGenerationCredits(ctx, inputTokens, maxOutputTokens, provider, model, s.now())
	if err != nil {
		return uuid.Nil, err
	}
	operationID := uuid.NewSHA1(uuid.NameSpaceOID, []byte("assistant_generation:"+runID.String()))
	_, err = s.creditRepository.(creditOperationRepository).ReserveCredits(ctx, subscription, models.ReserveCreditsInput{
		OperationUUID: operationID, OperationType: "assistant_generation", Environment: "production",
		Provider: provider, Model: model, Mode: "chat", IdempotencyKey: "assistant_generation:" + runID.String(), MaximumCharge: maximum,
	}, s.now())
	if err == nil {
		err = s.creditRepository.(creditOperationRepository).MarkCreditOperationProviderRunning(ctx, operationID)
	}
	return operationID, err
}

func (s *Service) SettleAssistantGeneration(ctx context.Context, operationID uuid.UUID, usage *models.ProviderUsage) error {
	return s.SettleAnalysis(ctx, operationID, usage)
}

func (s *Service) IsSandboxMockCall(ctx context.Context, callID uuid.UUID) (bool, error) {
	return s.creditRepository.(creditOperationRepository).IsSandboxMockCall(ctx, callID)
}

func (s *Service) ReserveTranscription(ctx context.Context, call models.Call, mode models.TranscriptionMode) (uuid.UUID, error) {
	subscription, err := s.subscriptionForCall(ctx, call)
	if err != nil {
		return uuid.Nil, err
	}
	maximum, err := s.creditRepository.(creditOperationRepository).MaximumTranscriptionCredits(ctx, int64(call.DurationSeconds), string(mode), s.now())
	if err != nil {
		return uuid.Nil, err
	}
	operationID := uuid.NewSHA1(uuid.NameSpaceOID, []byte("transcription:"+call.ID.String()))
	applicationID, environment, err := s.creditRepository.(creditOperationRepository).IntegrationBillingContextForCall(ctx, call.ID)
	if err != nil {
		return uuid.Nil, err
	}
	_, err = s.creditRepository.(creditOperationRepository).ReserveCredits(ctx, subscription, models.ReserveCreditsInput{OperationUUID: operationID, ApplicationUUID: applicationID, CallUUID: uuid.NullUUID{UUID: call.ID, Valid: true}, OperationType: "transcription", Environment: environment, Provider: "assemblyai", Model: "universal-2", Mode: string(mode), IdempotencyKey: "transcription:" + call.ID.String(), MaximumCharge: maximum}, s.now())
	if err == nil {
		err = s.creditRepository.(creditOperationRepository).MarkCreditOperationProviderRunning(ctx, operationID)
	}
	return operationID, err
}

func (s *Service) SettleTranscription(ctx context.Context, operationID uuid.UUID, call models.Call, mode models.TranscriptionMode) error {
	cost, err := s.creditRepository.(creditOperationRepository).TranscriptionProviderCostNanoUSD(ctx, operationID, int64(call.DurationSeconds))
	if err != nil {
		return err
	}
	credits, err := s.creditRepository.(creditOperationRepository).CreditsForOperationProviderCost(ctx, operationID, cost)
	if err != nil {
		return err
	}
	usage, _ := json.Marshal(map[string]any{"duration_seconds": call.DurationSeconds, "mode": mode})
	_, err = s.creditRepository.(creditOperationRepository).SettleCredits(ctx, models.SettleCreditsInput{OperationUUID: operationID, ActualChargeCredits: credits, ProviderCostNanoUSD: cost, ProviderUsageJSON: usage}, s.now())
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
	maximum, err := s.creditRepository.(creditOperationRepository).MaximumAnalysisCredits(ctx, int64(inputTokens), int64(maxOutputTokens), s.now())
	if err != nil {
		return uuid.Nil, err
	}
	operationID := uuid.NewSHA1(uuid.NameSpaceOID, []byte("analysis:"+analysisID.String()))
	applicationID, environment, err := s.creditRepository.(creditOperationRepository).IntegrationBillingContextForCall(ctx, call.ID)
	if err != nil {
		return uuid.Nil, err
	}
	_, err = s.creditRepository.(creditOperationRepository).ReserveCredits(ctx, subscription, models.ReserveCreditsInput{OperationUUID: operationID, ApplicationUUID: applicationID, CallUUID: uuid.NullUUID{UUID: call.ID, Valid: true}, OperationType: "analysis", Environment: environment, Provider: "openrouter", Model: "openai/gpt-5-mini", IdempotencyKey: "analysis:" + analysisID.String(), MaximumCharge: maximum}, s.now())
	if err == nil {
		err = s.creditRepository.(creditOperationRepository).MarkCreditOperationProviderRunning(ctx, operationID)
	}
	return operationID, err
}

func (s *Service) MarkCreditOperationReconciling(ctx context.Context, id uuid.UUID, reason string) error {
	return s.creditRepository.(creditOperationRepository).MarkCreditOperationReconciling(ctx, id, reason)
}

func (s *Service) SettleAnalysis(ctx context.Context, operationID uuid.UUID, usage *models.ProviderUsage) error {
	if usage == nil {
		return fmt.Errorf("analysis provider usage missing")
	}
	credits, err := s.creditRepository.(creditOperationRepository).CreditsForOperationProviderCost(ctx, operationID, usage.CostNanoUSD)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(usage)
	if err != nil {
		return err
	}
	_, err = s.creditRepository.(creditOperationRepository).SettleCredits(ctx, models.SettleCreditsInput{OperationUUID: operationID, ActualChargeCredits: credits, ProviderCostNanoUSD: usage.CostNanoUSD, ProviderUsageJSON: payload}, s.now())
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
