package processing

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"verbatrace/monolit/internal/logger"
	"verbatrace/monolit/internal/models"
	repositoryMocks "verbatrace/monolit/internal/repository/mocks"
	processingMocks "verbatrace/monolit/internal/service/processing/mocks"
	storageMocks "verbatrace/monolit/internal/storage/mocks"
	transcriberMocks "verbatrace/monolit/internal/transcriber/mocks"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
)

func TestProcessTranscribeCallHappyPath(t *testing.T) {
	ctx := context.Background()
	callID := uuid.New()
	transcriptionID := uuid.New()
	callRepo := repositoryMocks.NewCallRepository(t)
	transcriptionRepo := repositoryMocks.NewTranscriptionRepository(t)
	jobRepo := repositoryMocks.NewProcessingJobRepository(t)
	audioStorage := storageMocks.NewAudioStorage(t)
	transcriber := transcriberMocks.NewTranscriber(t)
	service := NewService(callRepo, transcriptionRepo, jobRepo, audioStorage, transcriber, nil)

	callRepo.EXPECT().GetByUUIDForProcessing(mock.Anything, callID).Return(models.Call{
		ID: callID, Status: models.CallStatusNew, AudioPath: "call.wav",
		OriginalFilename: "call.wav", MimeType: "audio/wav",
	}, nil).Once()
	callRepo.EXPECT().UpdateCallStatus(mock.Anything, callID, models.CallStatusProcessing).
		Return(models.Call{ID: callID, Status: models.CallStatusProcessing, AudioPath: "call.wav", OriginalFilename: "call.wav", MimeType: "audio/wav"}, nil).Once()
	transcriber.EXPECT().Provider().Return("test").Times(2)
	transcriptionRepo.EXPECT().Create(mock.Anything, mock.MatchedBy(func(value models.Transcription) bool {
		return value.CallUUID == callID && value.Status == models.TranscriptionStatusProcessing && value.Provider == "test"
	})).Return(models.Transcription{ID: transcriptionID, CallUUID: callID}, nil).Once()
	audioStorage.EXPECT().Open(mock.Anything, "call.wav").Return(io.NopCloser(strings.NewReader("audio")), nil).Once()
	transcriber.EXPECT().Transcribe(mock.Anything, mock.Anything).
		Return(models.TranscriptionResult{Text: "transcribed", Segments: []models.TranscriptionSegment{{Speaker: "speaker_0", Text: "transcribed"}}}, nil).Once()
	transcriptionRepo.EXPECT().MarkTranscribed(mock.Anything, transcriptionID, "transcribed", mock.MatchedBy(func(segments []models.TranscriptionSegment) bool {
		return len(segments) == 0
	}), mock.MatchedBy(func(words []models.TranscriptionWord) bool {
		return len(words) == 0
	}), mock.Anything).
		Return(models.Transcription{ID: transcriptionID}, nil).Once()
	callRepo.EXPECT().UpdateCallStatus(mock.Anything, callID, models.CallStatusTranscribed).
		Return(models.Call{ID: callID, Status: models.CallStatusTranscribed}, nil).Once()
	jobRepo.EXPECT().Enqueue(mock.Anything, mock.Anything).Return(models.ProcessingJob{}, nil).Once()

	if err := service.ProcessTranscribeCall(ctx, callID); err != nil {
		t.Fatalf("ProcessTranscribeCall: %v", err)
	}
}

func TestDiarizedTranscriptionWithoutSpeakerSegmentsIsNotPersisted(t *testing.T) {
	ctx := context.Background()
	callID := uuid.New()
	transcriptionID := uuid.New()
	callRepo := repositoryMocks.NewCallRepository(t)
	transcriptionRepo := repositoryMocks.NewTranscriptionRepository(t)
	audioStorage := storageMocks.NewAudioStorage(t)
	transcriber := transcriberMocks.NewTranscriber(t)
	service := NewService(callRepo, transcriptionRepo, nil, audioStorage, transcriber, nil)

	transcriber.EXPECT().Provider().Return("test").Once()
	transcriptionRepo.EXPECT().Create(mock.Anything, mock.MatchedBy(func(value models.Transcription) bool {
		return value.CallUUID == callID && value.Status == models.TranscriptionStatusProcessing
	})).Return(models.Transcription{ID: transcriptionID, CallUUID: callID}, nil).Once()
	audioStorage.EXPECT().Open(mock.Anything, "call.mp4").Return(io.NopCloser(strings.NewReader("video")), nil).Once()
	transcriber.EXPECT().Transcribe(mock.Anything, mock.Anything).
		Return(models.TranscriptionResult{Text: "text without speakers"}, nil).Once()

	err := service.processTranscribeCallWithMode(ctx, models.Call{
		ID: callID, Status: models.CallStatusProcessing, AudioPath: "call.mp4",
		OriginalFilename: "call.mp4", MimeType: "video/mp4",
	}, models.TranscriptionModeIdentified)

	if err == nil || !strings.Contains(err.Error(), "provider returned no speaker segments") {
		t.Fatalf("expected missing diarization error, got %v", err)
	}
}

func TestProcessEntryPointsAndStatuses(t *testing.T) {
	ctx := context.Background()
	callID := uuid.New()
	callRepo := repositoryMocks.NewCallRepository(t)
	transcriptionRepo := repositoryMocks.NewTranscriptionRepository(t)
	transcriber := transcriberMocks.NewTranscriber(t)
	service := NewService(callRepo, transcriptionRepo, nil, storageMocks.NewAudioStorage(t), transcriber, nil)
	processor := processingMocks.NewAnalysisProcessor(t)
	processor.EXPECT().ProcessAnalyzeCall(mock.Anything, callID).Return(nil).Once()
	service.SetAnalysisProcessor(processor)
	service.SetProcessingJobMaxAttempts(8)
	if service.processingJobMaxAttempts != 8 {
		t.Fatalf("max attempts = %d", service.processingJobMaxAttempts)
	}
	service.SetProcessingJobMaxAttempts(0)

	if err := service.ProcessJob(ctx, models.ProcessingJob{Type: "unknown"}); !errors.Is(err, models.ErrInvalidProcessingJobType) {
		t.Fatalf("invalid job error = %v", err)
	}
	if err := service.ProcessJob(ctx, models.ProcessingJob{Type: models.ProcessingJobTypeAnalyzeCall, EntityUUID: callID}); err != nil {
		t.Fatalf("analysis job: %v", err)
	}
	if err := service.ProcessAnalyzeCall(ctx, uuid.Nil); !errors.Is(err, models.ErrCallNotFound) {
		t.Fatalf("nil analysis call error = %v", err)
	}
	service.SetAnalysisProcessor(nil)
	if err := service.ProcessAnalyzeCall(ctx, callID); !errors.Is(err, models.ErrAnalyzerNotConfigured) {
		t.Fatalf("missing analyzer error = %v", err)
	}
	if err := service.processTranscribeCall(ctx, models.Call{ID: callID, Status: models.CallStatusAnalyzed}); err != nil {
		t.Fatal(err)
	}
	if err := service.processTranscribeCall(ctx, models.Call{ID: callID, Status: models.CallStatusTranscribed}); err != nil {
		t.Fatal(err)
	}
	if err := service.processTranscribeCall(ctx, models.Call{ID: callID, Status: models.CallStatusFailed}); !errors.Is(err, models.ErrInvalidCallStatusTransition) {
		t.Fatalf("invalid status error = %v", err)
	}

	callRepo.EXPECT().GetByUUIDForProcessing(mock.Anything, callID).
		Return(models.Call{}, errors.New("db error")).Once()
	service.ProcessCall(ctx, callID)
	service.ProcessClaimedCall(ctx, models.Call{ID: callID, Status: models.CallStatusFailed})

	withoutTranscriber := NewService(callRepo, transcriptionRepo, nil, nil, nil, nil)
	if err := withoutTranscriber.ProcessTranscribeCall(ctx, callID); !errors.Is(err, models.ErrTranscriberNotConfigured) {
		t.Fatalf("missing transcriber error = %v", err)
	}
	withoutTranscriber.ProcessCall(ctx, uuid.Nil)
	withoutTranscriber.ProcessClaimedCall(ctx, models.Call{})
}

func TestMarkJobFailedTranscription(t *testing.T) {
	callID := uuid.New()
	transcriptionID := uuid.New()
	callRepo := repositoryMocks.NewCallRepository(t)
	transcriptionRepo := repositoryMocks.NewTranscriptionRepository(t)
	service := NewService(callRepo, transcriptionRepo, nil, nil, transcriberMocks.NewTranscriber(t), nil)
	cause := errors.New("permanent")

	transcriptionRepo.EXPECT().GetByCallUUID(mock.Anything, callID).
		Return(models.Transcription{ID: transcriptionID}, nil).Once()
	transcriptionRepo.EXPECT().MarkFailed(mock.Anything, transcriptionID, cause.Error()).
		Return(models.Transcription{}, nil).Once()
	callRepo.EXPECT().UpdateCallStatus(mock.Anything, callID, models.CallStatusFailed).
		Return(models.Call{}, nil).Once()

	service.MarkJobFailed(context.Background(), models.ProcessingJob{
		Type: models.ProcessingJobTypeTranscribeCall, EntityUUID: callID,
	}, cause)
	service.MarkJobFailed(context.Background(), models.ProcessingJob{Type: "unknown"}, cause)
	service.MarkJobFailed(context.Background(), models.ProcessingJob{Type: models.ProcessingJobTypeAnalyzeCall}, cause)
}

func TestWorkerDefaultsAndErrorHandling(t *testing.T) {
	worker := NewWorker(nil, WorkerOptions{}, nil)
	if worker.pollInterval != defaultPollInterval || worker.workerLimit != defaultWorkerLimit ||
		worker.retryDelay != defaultRetryDelay || worker.staleAfter != defaultStaleAfter || worker.workerID == "" {
		t.Fatalf("worker defaults = %+v", worker)
	}
	worker.Run(context.Background())
	if fields := processingJobLogFields(models.ProcessingJob{}, "worker"); len(fields) != 7 {
		t.Fatalf("log fields = %d", len(fields))
	}

	repo := repositoryMocks.NewProcessingJobRepository(t)
	repo.EXPECT().TakeNext(mock.Anything, mock.Anything, mock.Anything).
		Return(models.ProcessingJob{}, models.ErrNoProcessingJobs).Once()
	service := &Service{processingJobRepository: repo, log: logger.NewNop()}
	worker = NewWorker(service, WorkerOptions{PollInterval: time.Millisecond, Limit: 1, RetryDelay: time.Second, StaleAfter: time.Minute}, nil)
	if err := worker.runBatch(context.Background()); err != nil {
		t.Fatal(err)
	}

	job := models.ProcessingJob{ID: uuid.New(), Type: "unknown", EntityUUID: uuid.New()}
	repo.EXPECT().MarkRetry(mock.Anything, job.ID, "temporary", time.Second).
		Return(models.ProcessingJob{Status: models.ProcessingJobStatusPending}, nil).Once()
	if err := worker.handleJobError(context.Background(), job, errors.New("temporary"), time.Second); err != nil {
		t.Fatal(err)
	}
	job.Attempts = 3
	if delay := worker.retryDelayFor(job); delay != 4*time.Second {
		t.Fatalf("retry delay = %s, want 4s", delay)
	}
	job.Attempts = 0
	repo.EXPECT().MarkFailed(mock.Anything, job.ID, models.ErrInvalidProcessingJobType.Error()).
		Return(models.ProcessingJob{Status: models.ProcessingJobStatusFailed}, nil).Once()
	if err := worker.handlePermanentJobError(context.Background(), job, models.ErrInvalidProcessingJobType, time.Second); err != nil {
		t.Fatal(err)
	}
	repo.EXPECT().MarkRetry(mock.Anything, job.ID, "temporary", time.Second).
		Return(models.ProcessingJob{Status: models.ProcessingJobStatusFailed}, nil).Once()
	if err := worker.handleJobError(context.Background(), job, errors.New("temporary"), time.Second); err != nil {
		t.Fatal(err)
	}

	markRetryErr := errors.New("mark retry failed")
	repo.EXPECT().MarkRetry(mock.Anything, job.ID, "temporary", time.Second).
		Return(models.ProcessingJob{}, markRetryErr).Once()
	if err := worker.handleJobError(context.Background(), job, errors.New("temporary"), time.Second); !errors.Is(err, markRetryErr) {
		t.Fatalf("mark retry error = %v", err)
	}

	markFailedErr := errors.New("mark failed failed")
	repo.EXPECT().MarkFailed(mock.Anything, job.ID, models.ErrInvalidProcessingJobType.Error()).
		Return(models.ProcessingJob{}, markFailedErr).Once()
	if err := worker.handlePermanentJobError(context.Background(), job, models.ErrInvalidProcessingJobType, time.Second); !errors.Is(err, markFailedErr) {
		t.Fatalf("mark failed error = %v", err)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	worker.Run(canceled)
}

func TestWorkerRunBatchProcessesJobs(t *testing.T) {
	callID := uuid.New()
	job := models.ProcessingJob{
		ID: uuid.New(), Type: models.ProcessingJobTypeAnalyzeCall, EntityUUID: callID,
		Status: models.ProcessingJobStatusRunning,
	}
	repo := repositoryMocks.NewProcessingJobRepository(t)
	repo.EXPECT().TakeNext(mock.Anything, mock.Anything, mock.Anything).Return(job, nil).Once()
	repo.EXPECT().TakeNext(mock.Anything, mock.Anything, mock.Anything).
		Return(models.ProcessingJob{}, models.ErrNoProcessingJobs).Once()
	repo.EXPECT().MarkDone(mock.Anything, job.ID).
		Return(models.ProcessingJob{ID: job.ID, Status: models.ProcessingJobStatusDone}, nil).Once()
	processor := processingMocks.NewAnalysisProcessor(t)
	processor.EXPECT().ProcessAnalyzeCall(mock.Anything, callID).Return(nil).Once()
	service := &Service{processingJobRepository: repo, analysisProcessor: processor, log: logger.NewNop()}
	worker := NewWorker(service, WorkerOptions{Limit: 1}, nil)
	if err := worker.runBatch(context.Background()); err != nil {
		t.Fatalf("runBatch: %v", err)
	}

	takeErr := errors.New("take failed")
	repo = repositoryMocks.NewProcessingJobRepository(t)
	repo.EXPECT().TakeNext(mock.Anything, mock.Anything, mock.Anything).
		Return(models.ProcessingJob{}, takeErr).Once()
	worker = NewWorker(&Service{processingJobRepository: repo, log: logger.NewNop()}, WorkerOptions{}, nil)
	if err := worker.runBatch(context.Background()); !errors.Is(err, takeErr) {
		t.Fatalf("take error = %v", err)
	}

	markDoneErr := errors.New("mark done failed")
	repo = repositoryMocks.NewProcessingJobRepository(t)
	repo.EXPECT().TakeNext(mock.Anything, mock.Anything, mock.Anything).Return(job, nil).Once()
	repo.EXPECT().TakeNext(mock.Anything, mock.Anything, mock.Anything).
		Return(models.ProcessingJob{}, models.ErrNoProcessingJobs).Once()
	repo.EXPECT().MarkDone(mock.Anything, job.ID).Return(models.ProcessingJob{}, markDoneErr).Once()
	processor = processingMocks.NewAnalysisProcessor(t)
	processor.EXPECT().ProcessAnalyzeCall(mock.Anything, callID).Return(nil).Once()
	worker = NewWorker(&Service{
		processingJobRepository: repo, analysisProcessor: processor, log: logger.NewNop(),
	}, WorkerOptions{Limit: 1}, nil)
	if err := worker.runBatch(context.Background()); !errors.Is(err, markDoneErr) {
		t.Fatalf("mark done error = %v", err)
	}
}
