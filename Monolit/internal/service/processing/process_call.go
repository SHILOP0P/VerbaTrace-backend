package processing

import (
	"context"
	"fmt"
	"strings"
	"time"

	"verbatrace/monolit/internal/models"
	"verbatrace/monolit/internal/storage"
	"verbatrace/monolit/internal/transcriber"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

func (s *Service) ProcessCall(ctx context.Context, callID uuid.UUID) {
	if callID == uuid.Nil {
		return
	}

	if err := s.ProcessTranscribeCall(ctx, callID); err != nil {
		s.log.Error(ctx, "call processing failed", zap.String("call_id", callID.String()), zap.Error(err))
	}
}

func (s *Service) ProcessClaimedCall(ctx context.Context, call models.Call) {
	if call.ID == uuid.Nil {
		return
	}

	if err := s.processTranscribeCall(ctx, call); err != nil {
		s.log.Error(ctx, "claimed call processing failed", zap.String("call_id", call.ID.String()), zap.Error(err))
	}
}

func (s *Service) ProcessJob(ctx context.Context, job models.ProcessingJob) error {
	switch job.Type {
	case models.ProcessingJobTypeTranscribeCall:
		return s.processTranscribeJob(ctx, job)
	case models.ProcessingJobTypeAnalyzeCall:
		return s.ProcessAnalyzeCall(ctx, job.EntityUUID)
	default:
		return models.ErrInvalidProcessingJobType
	}
}

func (s *Service) processTranscribeJob(ctx context.Context, job models.ProcessingJob) error {
	if job.EntityUUID == uuid.Nil {
		return models.ErrCallNotFound
	}
	if s.transcriber == nil {
		return models.ErrTranscriberNotConfigured
	}
	call, err := s.callRepository.GetByUUIDForProcessing(ctx, job.EntityUUID)
	if err != nil {
		return fmt.Errorf("get call for processing: %w", err)
	}
	return s.processTranscribeCallWithMode(ctx, call, job.TranscriptionMode)
}

func (s *Service) ProcessTranscribeCall(ctx context.Context, callID uuid.UUID) error {
	if callID == uuid.Nil {
		return models.ErrCallNotFound
	}

	if s.transcriber == nil {
		return models.ErrTranscriberNotConfigured
	}

	call, err := s.callRepository.GetByUUIDForProcessing(ctx, callID)
	if err != nil {
		return fmt.Errorf("get call for processing: %w", err)
	}

	return s.processTranscribeCallWithMode(ctx, call, models.TranscriptionModeStandard)
}

func (s *Service) processTranscribeCall(ctx context.Context, call models.Call) error {
	return s.processTranscribeCallWithMode(ctx, call, models.TranscriptionModeStandard)
}

func (s *Service) processTranscribeCallWithMode(ctx context.Context, call models.Call, mode models.TranscriptionMode) error {
	startedAt := time.Now()
	if s.transcriber == nil {
		return models.ErrTranscriberNotConfigured
	}

	if call.Status == models.CallStatusAnalyzed {
		s.log.Info(ctx, "call already analyzed", zap.String("call_id", call.ID.String()))
		return nil
	}

	if call.Status == models.CallStatusTranscribed {
		if err := s.enqueueAnalyzeJob(ctx, call.ID); err != nil {
			return fmt.Errorf("enqueue analysis job: %w", err)
		}
		s.log.Info(ctx, "call already transcribed", zap.String("call_id", call.ID.String()))
		return nil
	}

	if call.Status == models.CallStatusNew {
		speakerHints := call.SpeakerHints
		diarizationRoles := call.DiarizationRoles
		updatedCall, err := s.callRepository.UpdateCallStatus(ctx, call.ID, models.CallStatusProcessing)
		if err != nil {
			return fmt.Errorf("mark call processing: %w", err)
		}

		call = updatedCall
		call.SpeakerHints = speakerHints
		call.DiarizationRoles = diarizationRoles
	}

	if call.Status != models.CallStatusProcessing {
		return fmt.Errorf("%w: %s", models.ErrInvalidCallStatusTransition, call.Status)
	}

	transcription, err := s.createProcessingTranscription(ctx, call.ID, mode)
	if err != nil {
		return fmt.Errorf("create transcription record: %w", err)
	}

	audioFile, err := s.openAudio(ctx, call)
	if err != nil {
		return fmt.Errorf("open audio: %w", err)
	}
	audioFile.SpeakerCandidates = speakerCandidates(call)
	defer func() { _ = audioFile.Content.Close() }()

	sttStartedAt := time.Now()
	result, err := s.transcribe(ctx, audioFile, mode)
	if err != nil {
		return fmt.Errorf("transcribe audio: %w", err)
	}
	if mode == models.TranscriptionModeStandard {
		// Start includes only a continuous transcript. Keep this guard even if a
		// provider returns timestamps unexpectedly, so the API never exposes them.
		result.Segments = nil
	}

	if _, err = s.transcriptionRepository.MarkTranscribed(ctx, transcription.ID, result.Text, result.Segments, result.Words, result.Language); err != nil {
		return fmt.Errorf("mark transcription transcribed: %w", err)
	}

	if _, err = s.callRepository.UpdateCallStatus(ctx, call.ID, models.CallStatusTranscribed); err != nil {
		return fmt.Errorf("mark call transcribed: %w", err)
	}

	if err = s.enqueueAnalyzeJob(ctx, call.ID); err != nil {
		return fmt.Errorf("enqueue analysis job: %w", err)
	}

	s.log.Info(ctx, "call transcribed", zap.String("call_id", call.ID.String()), zap.String("provider", s.providerForMode(mode)), zap.String("transcription_mode", string(mode)), zap.Duration("stt_duration", time.Since(sttStartedAt)), zap.Duration("transcription_end_to_end_duration", time.Since(startedAt)))

	return nil
}

func speakerCandidates(call models.Call) []models.SpeakerCandidate {
	candidates := make([]models.SpeakerCandidate, 0, len(call.SpeakerHints)+len(call.DiarizationRoles))
	for _, hint := range call.SpeakerHints {
		label := strings.TrimSpace(hint.Name)
		if label == "" {
			continue
		}
		description := strings.TrimSpace(hint.Note)
		if username := strings.TrimSpace(hint.Username); username != "" {
			if description != "" {
				description += ". "
			}
			description += "Пользователь " + username
		}
		candidates = append(candidates, models.SpeakerCandidate{Label: label, Description: description, Kind: "person"})
	}
	for _, role := range call.DiarizationRoles {
		label := strings.TrimSpace(role.Name)
		if label == "" {
			continue
		}
		candidates = append(candidates, models.SpeakerCandidate{Label: label, Description: strings.TrimSpace(role.Description), Kind: "role"})
	}
	return candidates
}

func (s *Service) ProcessAnalyzeCall(ctx context.Context, callID uuid.UUID) error {
	if callID == uuid.Nil {
		return models.ErrCallNotFound
	}

	if s.analysisProcessor == nil {
		return models.ErrAnalyzerNotConfigured
	}

	return s.analysisProcessor.ProcessAnalyzeCall(ctx, callID)
}

func (s *Service) enqueueAnalyzeJob(ctx context.Context, callID uuid.UUID) error {
	if s.processingJobRepository == nil {
		return nil
	}

	jobID, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("generate analysis job uuid: %w", err)
	}

	now := time.Now().UTC()

	_, err = s.processingJobRepository.Enqueue(ctx, models.ProcessingJob{
		ID:          jobID,
		Type:        models.ProcessingJobTypeAnalyzeCall,
		EntityUUID:  callID,
		Status:      models.ProcessingJobStatusPending,
		Attempts:    0,
		MaxAttempts: s.processingJobMaxAttempts,
		AvailableAt: now,
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	if err != nil {
		return err
	}

	return nil
}

func (s *Service) createProcessingTranscription(ctx context.Context, callID uuid.UUID, mode models.TranscriptionMode) (models.Transcription, error) {
	transcriptionID, err := uuid.NewV7()
	if err != nil {
		return models.Transcription{}, err
	}

	now := time.Now().UTC()

	return s.transcriptionRepository.Create(ctx, models.Transcription{
		ID:        transcriptionID,
		CallUUID:  callID,
		Status:    models.TranscriptionStatusProcessing,
		Provider:  s.providerForMode(mode),
		CreatedAt: now,
		UpdatedAt: now,
	})
}

func (s *Service) transcribe(ctx context.Context, file models.File, mode models.TranscriptionMode) (models.TranscriptionResult, error) {
	if provider, ok := s.transcriber.(transcriber.ModeAware); ok {
		return provider.TranscribeForMode(ctx, file, mode)
	}
	return s.transcriber.Transcribe(ctx, file)
}

func (s *Service) providerForMode(mode models.TranscriptionMode) string {
	if provider, ok := s.transcriber.(transcriber.ModeAware); ok {
		return provider.ProviderForMode(mode)
	}
	return s.transcriber.Provider()
}

func (s *Service) openAudio(ctx context.Context, call models.Call) (models.File, error) {
	if cachePath := strings.TrimSpace(call.ASRCachePath); cachePath != "" {
		if cacheStorage, ok := s.audioStorage.(storage.ASRCacheStorage); ok {
			startedAt := time.Now()
			reused, err := cacheStorage.EnsureASRCache(ctx, call.AudioPath, cachePath)
			if err != nil {
				s.log.Warn(ctx, "ASR cache unavailable; using original media", zap.String("call_id", call.ID.String()), zap.String("asr_cache_path", cachePath), zap.Duration("asr_prepare_duration", time.Since(startedAt)), zap.Error(err))
			} else if content, openErr := s.audioStorage.Open(ctx, cachePath); openErr == nil {
				s.log.Info(ctx, "ASR cache ready", zap.String("call_id", call.ID.String()), zap.String("asr_cache_path", cachePath), zap.Bool("asr_cache_reused", reused), zap.Duration("asr_prepare_duration", time.Since(startedAt)))
				return models.File{Content: content, Path: cachePath, OriginalFilename: "asr.ogg", MimeType: "audio/ogg"}, nil
			} else {
				s.log.Warn(ctx, "ASR cache cannot be opened; using original media", zap.String("call_id", call.ID.String()), zap.String("asr_cache_path", cachePath), zap.Error(openErr))
			}
		}
	}

	content, err := s.audioStorage.Open(ctx, call.AudioPath)
	if err != nil {
		return models.File{}, err
	}

	return models.File{
		Content:          content,
		Path:             call.AudioPath,
		OriginalFilename: call.OriginalFilename,
		MimeType:         call.MimeType,
		SizeBytes:        call.SizeBytes,
	}, nil
}

func (s *Service) MarkJobFailed(ctx context.Context, job models.ProcessingJob, cause error) {
	switch job.Type {
	case models.ProcessingJobTypeTranscribeCall:
		transcription, err := s.transcriptionRepository.GetByCallUUID(ctx, job.EntityUUID)
		if err == nil && transcription.ID != uuid.Nil {
			_, _ = s.transcriptionRepository.MarkFailed(context.Background(), transcription.ID, cause.Error())
		}

		_, _ = s.callRepository.UpdateCallStatus(context.Background(), job.EntityUUID, models.CallStatusFailed)

		s.log.Error(ctx, "processing job permanently failed", zap.String("call_id", job.EntityUUID.String()), zap.String("job_id", job.ID.String()), zap.Error(cause))

	case models.ProcessingJobTypeAnalyzeCall:
		if s.analysisProcessor == nil {
			s.log.Error(ctx, "analysis job permanently failed but processor is not configured", zap.String("call_id", job.EntityUUID.String()), zap.String("job_id", job.ID.String()), zap.Error(cause))
			return
		}

		if err := s.analysisProcessor.MarkAnalyzeCallFailed(context.Background(), job.EntityUUID, cause); err != nil {
			s.log.Error(ctx, "mark analysis job permanently failed", zap.String("call_id", job.EntityUUID.String()), zap.String("job_id", job.ID.String()), zap.Error(err), zap.NamedError("cause", cause))
			return
		}

		s.log.Error(ctx, "analysis processing job permanently failed", zap.String("call_id", job.EntityUUID.String()), zap.String("job_id", job.ID.String()), zap.Error(cause))

	default:
		return
	}
}
