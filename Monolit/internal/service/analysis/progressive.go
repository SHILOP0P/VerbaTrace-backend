package analysis

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"verbatrace/monolit/internal/analyzer"
	"verbatrace/monolit/internal/analyzer/analysisflow"
	"verbatrace/monolit/internal/models"
)

type progressiveRepository interface {
	BeginPipeline(context.Context, uuid.UUID, string, int) error
	SaveProgress(context.Context, uuid.UUID, string, int, json.RawMessage) error
	ClaimAnalysisTask(context.Context, uuid.UUID, uuid.UUID) (*models.AnalysisResult, error)
	SaveAnalysisTask(context.Context, uuid.UUID, models.AnalysisResult) error
	ReleaseAnalysisTask(context.Context, uuid.UUID) error
}
type revisionReader interface {
	GetReportTranscription(context.Context, uuid.UUID, int) (models.Transcription, int, error)
}

type processingJobHeartbeat interface {
	HeartbeatByEntity(context.Context, uuid.UUID) error
}
type speakerContextReader interface {
	GetAnalysisSpeakerContext(context.Context, uuid.UUID) ([]string, error)
}

func (s *Service) analyzeProgressively(ctx context.Context, call models.Call, analysis models.CallAnalysis, transcription models.Transcription, request models.AnalysisRequest, provider analyzer.Analyzer, schema map[string]any) (models.AnalysisResult, error) {
	repository, ok := s.analysisRepository.(progressiveRepository)
	if !ok {
		return models.AnalysisResult{}, fmt.Errorf("progressive analysis storage is unavailable")
	}
	revision := 1
	runID := "upload"
	if attempts, ok := s.analysisRepository.(attemptRepository); ok {
		var err error
		revision, err = attempts.CurrentTranscriptionRevision(ctx, call.ID)
		if err != nil {
			return models.AnalysisResult{}, err
		}
		if active, err := attempts.ActiveAttempt(ctx, call.ID); err == nil {
			revision = active.TranscriptionRevision
			runID = active.ID.String()
		}
	}
	if reader, ok := s.transcriptionRepository.(revisionReader); ok {
		var err error
		transcription, revision, err = reader.GetReportTranscription(ctx, call.ID, revision)
		if err != nil {
			return models.AnalysisResult{}, err
		}
		request.Transcription = *transcription.Text
	}
	segments := analysisflow.SourceSegments(transcription)
	if reader, ok := s.transcriptionRepository.(speakerContextReader); ok {
		labels, err := reader.GetAnalysisSpeakerContext(ctx, call.ID)
		if err != nil {
			return models.AnalysisResult{}, err
		}
		request.Personalization = append(append([]string{}, request.Personalization...), labels...)
	}
	input, _ := json.Marshal(struct {
		Request  models.AnalysisRequest
		Segments []analysisflow.Segment
		Revision int
		RunID    string
		Version  string
	}{request, segments, revision, runID, analysisflow.Version})
	runKey := fmt.Sprintf("%x", sha256.Sum256(input))
	if err := repository.BeginPipeline(ctx, analysis.ID, runKey, revision); err != nil {
		return models.AnalysisResult{}, err
	}
	runner := analysisflow.Runner{Request: request, Segments: segments, Schema: schema}
	runner.Publish = func(ctx context.Context, raw json.RawMessage) error {
		normalized, err := normalizeAnalysisResult(models.AnalysisResult{ResultJSON: raw})
		if err != nil {
			return err
		}
		if err = repository.SaveProgress(ctx, analysis.ID, runKey, revision, normalized.ResultJSON); err != nil {
			return err
		}
		if jobs, ok := s.processingJobRepository.(processingJobHeartbeat); ok {
			return jobs.HeartbeatByEntity(ctx, call.ID)
		}
		return nil
	}
	runner.Execute = func(ctx context.Context, key string, task models.AnalysisTask) (models.AnalysisResult, error) {
		taskJSON, _ := json.Marshal(task)
		taskID := uuid.NewSHA1(analysis.ID, []byte(runKey+"/"+key+"/"+string(taskJSON)))
		cached, err := repository.ClaimAnalysisTask(ctx, taskID, analysis.ID)
		if err != nil {
			return models.AnalysisResult{}, err
		}
		if cached != nil {
			if s.creditMeter != nil && cached.Usage != nil {
				if err = s.creditMeter.SettleAnalysis(ctx, cached.CreditOperationID, cached.Usage); err != nil {
					return models.AnalysisResult{}, err
				}
			}
			if len(cached.ResultJSON) == 0 {
				return *cached, fmt.Errorf("incomplete_coverage: cached provider output was invalid")
			}
			return *cached, nil
		}
		var operationID uuid.UUID
		if s.creditMeter != nil {
			if operationID, err = s.creditMeter.ReserveAnalysis(ctx, call, taskID, task.System+"\n"+task.Context+"\n"+task.Input, int64(task.MaxTokens)); err != nil {
				_ = repository.ReleaseAnalysisTask(ctx, taskID)
				return models.AnalysisResult{}, err
			}
		}
		stepCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		result, providerErr := provider.Analyze(stepCtx, models.AnalysisRequest{CallUUID: call.ID, Task: &task})
		cancel()
		result.CreditOperationID = operationID
		// Persist the actual provider response before settlement or semantic validation.
		if providerErr == nil || result.Usage != nil {
			if err = repository.SaveAnalysisTask(ctx, taskID, result); err != nil {
				return result, err
			}
		}
		if s.creditMeter != nil {
			if result.Usage != nil {
				if err = s.creditMeter.SettleAnalysis(ctx, operationID, result.Usage); err != nil {
					return result, err
				}
			} else if providerErr != nil {
				_ = s.creditMeter.MarkCreditOperationReconciling(context.Background(), operationID, "analysis_step_provider_error")
			}
		}
		return result, providerErr
	}
	var growth *models.GrowthOutcome
	runner.OnGrowth = func(_ context.Context, outcome models.GrowthOutcome) { growth = &outcome }
	runner.Warn = func(ctx context.Context, message string) {
		s.log.Warn(ctx, message, zap.String("call_id", call.ID.String()))
	}
	result, err := runner.Run(ctx)
	result.PipelineRunKey = runKey
	result.TranscriptionRevision = revision
	result.Growth = growth
	return result, err
}
