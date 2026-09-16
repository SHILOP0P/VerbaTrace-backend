package analysis

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func TestAnalyzeCallEnqueuesAnalysisJob(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	callID := uuid.New()

	transcriptionText := "Client asked about pricing."
	callRepo := &analysisCallRepository{
		call: models.Call{
			ID:                 callID,
			Status:             models.CallStatusTranscribed,
			UploadedByUserUUID: uuid.NullUUID{UUID: userID, Valid: true},
			VisibilityScope:    models.CallVisibilityScopePersonal,
		},
	}
	transcriptionRepo := &analysisTranscriptionRepository{
		transcription: models.Transcription{
			ID:       uuid.New(),
			CallUUID: callID,
			Status:   models.TranscriptionStatusTranscribed,
			Text:     &transcriptionText,
		},
	}
	analysisRepo := &analysisRepository{
		analysisID: uuid.New(),
		callID:     callID,
	}
	jobRepo := &analysisProcessingJobRepository{}
	analyzerProvider := &recordingAnalyzer{}

	service := NewService(callRepo, transcriptionRepo, &analysisInstructionRepository{}, analysisRepo, &analysisInstructionStorage{}, analyzerProvider, nil)
	service.SetProcessingJobRepository(jobRepo)
	service.SetProcessingJobMaxAttempts(5)

	analysis, err := service.AnalyzeCall(ctx, models.AnalyzeCallInput{
		CallUUID: callID,
		UserUUID: userID,
	})
	if err != nil {
		t.Fatalf("analyze call: %v", err)
	}

	if analysis.Status != models.CallAnalysisStatusPending {
		t.Fatalf("analysis status = %s, want %s", analysis.Status, models.CallAnalysisStatusPending)
	}
	if analyzerProvider.called {
		t.Fatal("analyzer was called synchronously")
	}
	if !jobRepo.enqueued {
		t.Fatal("analysis job was not enqueued")
	}
	if jobRepo.job.Type != models.ProcessingJobTypeAnalyzeCall {
		t.Fatalf("job type = %s, want %s", jobRepo.job.Type, models.ProcessingJobTypeAnalyzeCall)
	}
	if jobRepo.job.EntityUUID != callID {
		t.Fatalf("job entity uuid = %s, want %s", jobRepo.job.EntityUUID, callID)
	}
	if jobRepo.job.Status != models.ProcessingJobStatusPending {
		t.Fatalf("job status = %s, want %s", jobRepo.job.Status, models.ProcessingJobStatusPending)
	}
	if jobRepo.job.MaxAttempts != 5 {
		t.Fatalf("job max attempts = %d, want 5", jobRepo.job.MaxAttempts)
	}
}

func TestAnalyzeCallRestoresTranscribedStatusAfterPreviousAnalysisFailure(t *testing.T) {
	ctx := context.Background()
	userID, callID := uuid.New(), uuid.New()
	text := "Готовая транскрипция"
	callRepo := &analysisCallRepository{call: models.Call{ID: callID, Status: models.CallStatusFailed}}
	transcriptionRepo := &analysisTranscriptionRepository{transcription: models.Transcription{
		ID: uuid.New(), CallUUID: callID, Status: models.TranscriptionStatusTranscribed, Text: &text,
	}}
	analysisRepo := &analysisRepository{analysisID: uuid.New(), callID: callID}
	jobRepo := &analysisProcessingJobRepository{}
	service := NewService(callRepo, transcriptionRepo, &analysisInstructionRepository{}, analysisRepo, &analysisInstructionStorage{}, &recordingAnalyzer{}, nil)
	service.SetProcessingJobRepository(jobRepo)

	if _, err := service.AnalyzeCall(ctx, models.AnalyzeCallInput{CallUUID: callID, UserUUID: userID}); err != nil {
		t.Fatalf("AnalyzeCall: %v", err)
	}
	if !callRepo.updatedStatus || callRepo.lastStatus != models.CallStatusTranscribed {
		t.Fatalf("call status = %s, want %s", callRepo.lastStatus, models.CallStatusTranscribed)
	}
}

func TestAnalyzeCallRejectsTestCallWithoutCreatingJob(t *testing.T) {
	callID, userID := uuid.New(), uuid.New()
	callRepo := &analysisCallRepository{call: models.Call{ID: callID, Status: models.CallStatusTranscribed, IsTest: true}}
	jobRepo := &analysisProcessingJobRepository{}
	service := NewService(callRepo, &analysisTranscriptionRepository{}, &analysisInstructionRepository{}, &analysisRepository{}, &analysisInstructionStorage{}, &recordingAnalyzer{}, nil)
	service.SetProcessingJobRepository(jobRepo)

	_, err := service.AnalyzeCall(context.Background(), models.AnalyzeCallInput{CallUUID: callID, UserUUID: userID})
	if !errors.Is(err, models.ErrTestCallReadOnly) {
		t.Fatalf("AnalyzeCall() error = %v, want ErrTestCallReadOnly", err)
	}
	if jobRepo.enqueued {
		t.Fatal("test call analysis job was enqueued")
	}
}

func TestProcessAnalyzeCallPassesCompanyAndDepartmentInstructions(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	callID := uuid.New()
	companyID := uuid.New()
	departmentID := uuid.New()

	transcriptionText := "Client asked about pricing."
	callRepo := &analysisCallRepository{
		call: models.Call{
			ID:                 callID,
			Status:             models.CallStatusTranscribed,
			UploadedByUserUUID: uuid.NullUUID{UUID: userID, Valid: true},
			CompanyUUID:        uuid.NullUUID{UUID: companyID, Valid: true},
			DepartmentUUID:     uuid.NullUUID{UUID: departmentID, Valid: true},
			VisibilityScope:    models.CallVisibilityScopeDepartment,
		},
	}
	transcriptionRepo := &analysisTranscriptionRepository{
		transcription: models.Transcription{
			ID:       uuid.New(),
			CallUUID: callID,
			Status:   models.TranscriptionStatusTranscribed,
			Text:     &transcriptionText,
		},
	}
	companyInstruction := models.AnalysisInstruction{
		ID:          uuid.New(),
		Scope:       models.AnalysisInstructionScopeCompany,
		CompanyUUID: uuid.NullUUID{UUID: companyID, Valid: true},
		Title:       "Company criteria",
		FilePath:    "company.md",
		IsActive:    true,
	}
	departmentInstruction := models.AnalysisInstruction{
		ID:             uuid.New(),
		Scope:          models.AnalysisInstructionScopeDepartment,
		CompanyUUID:    uuid.NullUUID{UUID: companyID, Valid: true},
		DepartmentUUID: uuid.NullUUID{UUID: departmentID, Valid: true},
		Title:          "Department criteria",
		FilePath:       "department.md",
		IsActive:       true,
	}
	instructionRepo := &analysisInstructionRepository{
		instructions: map[models.AnalysisInstructionScope][]models.AnalysisInstruction{
			models.AnalysisInstructionScopeCompany:    {companyInstruction},
			models.AnalysisInstructionScopeDepartment: {departmentInstruction},
		},
	}
	analysisRepo := &analysisRepository{
		analysisID: uuid.New(),
		callID:     callID,
	}
	instructionStorage := &analysisInstructionStorage{
		files: map[string]string{
			"company.md":    "Require a short company summary.",
			"department.md": "Add the department next step.",
		},
	}
	analyzerProvider := &recordingAnalyzer{
		result: models.AnalysisResult{
			ResultJSON: json.RawMessage(`{"summary":"Client discussed pricing.","next_steps":["Send pricing details."]}`),
		},
	}

	service := NewService(callRepo, transcriptionRepo, instructionRepo, analysisRepo, instructionStorage, analyzerProvider, nil)

	err := service.ProcessAnalyzeCall(ctx, callID)
	if err != nil {
		t.Fatalf("process analyze call: %v", err)
	}

	if len(analyzerProvider.request.Instructions) != 2 {
		t.Fatalf("instructions len = %d, want 2", len(analyzerProvider.request.Instructions))
	}
	if analyzerProvider.request.Instructions[0].Scope != models.AnalysisInstructionScopeCompany {
		t.Fatalf("first instruction scope = %s", analyzerProvider.request.Instructions[0].Scope)
	}
	if analyzerProvider.request.Instructions[1].Scope != models.AnalysisInstructionScopeDepartment {
		t.Fatalf("second instruction scope = %s", analyzerProvider.request.Instructions[1].Scope)
	}
	if !strings.Contains(analyzerProvider.request.Instructions[0].Content, "company summary") {
		t.Fatalf("company instruction content was not passed: %#v", analyzerProvider.request.Instructions[0])
	}
	if !strings.Contains(analyzerProvider.request.Instructions[1].Content, "department next step") {
		t.Fatalf("department instruction content was not passed: %#v", analyzerProvider.request.Instructions[1])
	}
	if !callRepo.updatedStatus || callRepo.lastStatus != models.CallStatusAnalyzed {
		t.Fatalf("call status was not marked analyzed")
	}

	var payload map[string]any
	if err = json.Unmarshal(analysisRepo.doneResult.ResultJSON, &payload); err != nil {
		t.Fatalf("decode saved result json: %v", err)
	}
	if payload["summary"] != "Client discussed pricing." {
		t.Fatalf("summary = %v", payload["summary"])
	}
	if _, ok := payload["topics"]; !ok {
		t.Fatalf("topics key is missing from saved result")
	}
	if _, ok := payload["dialogue_tone"]; !ok {
		t.Fatalf("dialogue_tone key is missing from saved result")
	}
	if _, ok := payload["question_coverage"]; !ok {
		t.Fatalf("question_coverage key is missing from saved result")
	}
	if _, ok := payload["manager_quality"]; !ok {
		t.Fatalf("manager_quality key is missing from saved result")
	}
	if payload["next_step"] != "Send pricing details." {
		t.Fatalf("next_step = %v", payload["next_step"])
	}
	if analysisRepo.doneResult.ResultText == nil || *analysisRepo.doneResult.ResultText != "Client discussed pricing." {
		t.Fatalf("result text = %v", analysisRepo.doneResult.ResultText)
	}
}

func TestProcessAnalyzeCallSkipsMissingInstructionFile(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	callID := uuid.New()
	transcriptionText := "Client asked about pricing."

	callRepo := &analysisCallRepository{
		call: models.Call{
			ID:                 callID,
			Status:             models.CallStatusTranscribed,
			UploadedByUserUUID: uuid.NullUUID{UUID: userID, Valid: true},
			VisibilityScope:    models.CallVisibilityScopePersonal,
		},
	}
	transcriptionRepo := &analysisTranscriptionRepository{
		transcription: models.Transcription{
			ID:       uuid.New(),
			CallUUID: callID,
			Status:   models.TranscriptionStatusTranscribed,
			Text:     &transcriptionText,
		},
	}
	missingInstruction := models.AnalysisInstruction{
		ID:       uuid.New(),
		Scope:    models.AnalysisInstructionScopePersonal,
		UserUUID: uuid.NullUUID{UUID: userID, Valid: true},
		Title:    "Missing criteria",
		FilePath: "missing.md",
		IsActive: true,
	}
	presentInstruction := models.AnalysisInstruction{
		ID:       uuid.New(),
		Scope:    models.AnalysisInstructionScopePersonal,
		UserUUID: uuid.NullUUID{UUID: userID, Valid: true},
		Title:    "Present criteria",
		FilePath: "present.md",
		IsActive: true,
	}
	instructionRepo := &analysisInstructionRepository{
		instructions: map[models.AnalysisInstructionScope][]models.AnalysisInstruction{
			models.AnalysisInstructionScopePersonal: {missingInstruction, presentInstruction},
		},
	}
	analysisRepo := &analysisRepository{
		analysisID: uuid.New(),
		callID:     callID,
	}
	instructionStorage := &analysisInstructionStorage{
		files: map[string]string{
			"present.md": "Ask for a concrete next step.",
		},
	}
	analyzerProvider := &recordingAnalyzer{
		result: models.AnalysisResult{
			ResultJSON: json.RawMessage(`{"summary":"Client discussed pricing."}`),
		},
	}

	service := NewService(callRepo, transcriptionRepo, instructionRepo, analysisRepo, instructionStorage, analyzerProvider, nil)

	if err := service.ProcessAnalyzeCall(ctx, callID); err != nil {
		t.Fatalf("process analyze call: %v", err)
	}
	if len(analyzerProvider.request.Instructions) != 1 {
		t.Fatalf("instructions len = %d, want 1", len(analyzerProvider.request.Instructions))
	}
	if analyzerProvider.request.Instructions[0].ID != presentInstruction.ID {
		t.Fatalf("instruction id = %s, want %s", analyzerProvider.request.Instructions[0].ID, presentInstruction.ID)
	}
	if !strings.Contains(analyzerProvider.request.Instructions[0].Content, "next step") {
		t.Fatalf("instruction content was not passed: %#v", analyzerProvider.request.Instructions[0])
	}
	if !callRepo.updatedStatus || callRepo.lastStatus != models.CallStatusAnalyzed {
		t.Fatalf("call status was not marked analyzed")
	}
}

func TestProcessAnalyzeCallSkipsCustomInstructions(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	callID := uuid.New()
	transcriptionText := "Client asked about pricing."

	callRepo := &analysisCallRepository{
		call: models.Call{
			ID:                     callID,
			Status:                 models.CallStatusTranscribed,
			UploadedByUserUUID:     uuid.NullUUID{UUID: userID, Valid: true},
			VisibilityScope:        models.CallVisibilityScopePersonal,
			SkipCustomInstructions: true,
		},
	}
	transcriptionRepo := &analysisTranscriptionRepository{
		transcription: models.Transcription{
			ID:       uuid.New(),
			CallUUID: callID,
			Status:   models.TranscriptionStatusTranscribed,
			Text:     &transcriptionText,
		},
	}
	instructionRepo := &analysisInstructionRepository{
		instructions: map[models.AnalysisInstructionScope][]models.AnalysisInstruction{
			models.AnalysisInstructionScopePersonal: {{
				ID:       uuid.New(),
				Scope:    models.AnalysisInstructionScopePersonal,
				UserUUID: uuid.NullUUID{UUID: userID, Valid: true},
				Title:    "Missing file",
				FilePath: "missing.md",
				IsActive: true,
			}},
		},
	}
	analysisRepo := &analysisRepository{analysisID: uuid.New(), callID: callID}
	analyzerProvider := &recordingAnalyzer{
		result: models.AnalysisResult{
			ResultJSON: json.RawMessage(`{"summary":"Base analysis only."}`),
		},
	}

	service := NewService(callRepo, transcriptionRepo, instructionRepo, analysisRepo, &analysisInstructionStorage{}, analyzerProvider, nil)

	if err := service.ProcessAnalyzeCall(ctx, callID); err != nil {
		t.Fatalf("process analyze call: %v", err)
	}
	if len(analyzerProvider.request.Instructions) != 0 {
		t.Fatalf("instructions len = %d, want 0", len(analyzerProvider.request.Instructions))
	}
	if !callRepo.updatedStatus || callRepo.lastStatus != models.CallStatusAnalyzed {
		t.Fatalf("call status was not marked analyzed")
	}
}

func TestProcessAnalyzeCallKeepsFolderInstructionsWhenCustomInstructionsAreSkipped(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	callID := uuid.New()
	transcriptionText := "The candidate answered the Go question."
	folderInstruction := models.AnalysisInstruction{
		ID: uuid.New(), Scope: models.AnalysisInstructionScopePersonal, Title: "Go interview analysis",
		OriginalFilename: "go-interview.md", FilePath: "go-interview.md", IsActive: true,
	}
	callRepo := &analysisCallRepository{call: models.Call{
		ID: callID, Status: models.CallStatusTranscribed,
		UploadedByUserUUID: uuid.NullUUID{UUID: userID, Valid: true},
		VisibilityScope:    models.CallVisibilityScopePersonal, SkipCustomInstructions: true,
	}}
	transcriptionRepo := &analysisTranscriptionRepository{transcription: models.Transcription{
		ID: uuid.New(), CallUUID: callID, Status: models.TranscriptionStatusTranscribed, Text: &transcriptionText,
	}}
	analysisRepo := &analysisRepository{analysisID: uuid.New(), callID: callID}
	analyzerProvider := &recordingAnalyzer{result: models.AnalysisResult{ResultJSON: json.RawMessage(`{"summary":"Done."}`)}}
	service := NewService(callRepo, transcriptionRepo, &analysisInstructionRepository{}, analysisRepo, &analysisInstructionStorage{
		files: map[string]string{"go-interview.md": "Evaluate the technical correctness of the candidate's answers."},
	}, analyzerProvider, nil)
	service.SetFolderInstructionReader(&analysisFolderInstructionReader{instructions: []models.AnalysisInstruction{folderInstruction}})

	if err := service.ProcessAnalyzeCall(ctx, callID); err != nil {
		t.Fatalf("process analyze call: %v", err)
	}
	if len(analyzerProvider.request.Instructions) != 1 || analyzerProvider.request.Instructions[0].ID != folderInstruction.ID {
		t.Fatalf("folder instructions = %#v, want %s", analyzerProvider.request.Instructions, folderInstruction.ID)
	}
}

func TestProcessAnalyzeCallKeepsAnalysisProcessingOnProviderError(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	callID := uuid.New()
	transcriptionText := "Client asked about pricing."

	callRepo := &analysisCallRepository{
		call: models.Call{
			ID:                 callID,
			Status:             models.CallStatusTranscribed,
			UploadedByUserUUID: uuid.NullUUID{UUID: userID, Valid: true},
			VisibilityScope:    models.CallVisibilityScopePersonal,
		},
	}
	transcriptionRepo := &analysisTranscriptionRepository{
		transcription: models.Transcription{
			ID:       uuid.New(),
			CallUUID: callID,
			Status:   models.TranscriptionStatusTranscribed,
			Text:     &transcriptionText,
		},
	}
	analysisRepo := &analysisRepository{
		analysisID: uuid.New(),
		callID:     callID,
	}
	service := NewService(
		callRepo,
		transcriptionRepo,
		&analysisInstructionRepository{},
		analysisRepo,
		&analysisInstructionStorage{},
		&recordingAnalyzer{err: errors.New("openrouter analysis failed with status 429")},
		nil,
	)

	err := service.ProcessAnalyzeCall(ctx, callID)
	if err == nil {
		t.Fatal("expected provider error")
	}
	if !analysisRepo.markedProcessing {
		t.Fatal("analysis was not marked processing")
	}
	if analysisRepo.markedFailed {
		t.Fatal("analysis attempt was marked failed before processing retries were exhausted")
	}
}

func TestMarkAnalyzeCallFailedMarksExistingAnalysis(t *testing.T) {
	ctx := context.Background()
	callID := uuid.New()
	analysisID := uuid.New()
	analysisRepo := &analysisRepository{
		callID: callID,
		existingAnalysis: &models.CallAnalysis{
			ID:       analysisID,
			CallUUID: callID,
			Status:   models.CallAnalysisStatusProcessing,
			Provider: "test",
		},
	}
	callRepo := &analysisCallRepository{
		call: models.Call{ID: callID, Status: models.CallStatusTranscribed},
	}
	service := NewService(
		callRepo,
		&analysisTranscriptionRepository{},
		&analysisInstructionRepository{},
		analysisRepo,
		&analysisInstructionStorage{},
		&recordingAnalyzer{},
		nil,
	)

	err := service.MarkAnalyzeCallFailed(ctx, callID, errors.New("openrouter analysis failed with status 429"))
	if err != nil {
		t.Fatalf("mark analyze call failed: %v", err)
	}
	if !analysisRepo.markedFailed {
		t.Fatal("analysis was not marked failed")
	}
	if analysisRepo.failedMessage != "openrouter analysis failed with status 429" {
		t.Fatalf("failed message = %q", analysisRepo.failedMessage)
	}
	if !callRepo.updatedStatus {
		t.Fatal("call was not marked failed")
	}
	if callRepo.lastStatus != models.CallStatusFailed {
		t.Fatalf("call status = %q", callRepo.lastStatus)
	}
}

type recordingAnalyzer struct {
	request models.AnalysisRequest
	result  models.AnalysisResult
	err     error
	called  bool
}

func (a *recordingAnalyzer) Provider() string {
	return "test"
}

func (a *recordingAnalyzer) Analyze(ctx context.Context, request models.AnalysisRequest) (models.AnalysisResult, error) {
	a.called = true
	a.request = request
	if a.err != nil {
		return models.AnalysisResult{}, a.err
	}
	return a.result, nil
}

func (a *recordingAnalyzer) AnalyzeAggregate(context.Context, models.AggregateAnalysisRequest) (models.AnalysisResult, error) {
	panic("not implemented")
}

type analysisProcessingJobRepository struct {
	enqueued bool
	job      models.ProcessingJob
}

func (r *analysisProcessingJobRepository) Create(ctx context.Context, job models.ProcessingJob) (models.ProcessingJob, error) {
	panic("not implemented")
}

func (r *analysisProcessingJobRepository) Enqueue(ctx context.Context, job models.ProcessingJob) (models.ProcessingJob, error) {
	r.enqueued = true
	r.job = job
	return job, nil
}

func (r *analysisProcessingJobRepository) TakeNext(ctx context.Context, workerID string, staleAfter time.Duration) (models.ProcessingJob, error) {
	panic("not implemented")
}

func (r *analysisProcessingJobRepository) MarkDone(ctx context.Context, id uuid.UUID) (models.ProcessingJob, error) {
	panic("not implemented")
}

func (r *analysisProcessingJobRepository) MarkRetry(ctx context.Context, id uuid.UUID, lastError string, delay time.Duration) (models.ProcessingJob, error) {
	panic("not implemented")
}

func (r *analysisProcessingJobRepository) MarkFailed(ctx context.Context, id uuid.UUID, lastError string) (models.ProcessingJob, error) {
	panic("not implemented")
}

type analysisInstructionStorage struct {
	files map[string]string
}

func (s *analysisInstructionStorage) Save(ctx context.Context, input models.SaveInstructionInput) (models.SavedInstructionFile, error) {
	panic("not implemented")
}

func (s *analysisInstructionStorage) Open(ctx context.Context, path string) (io.ReadCloser, error) {
	content, ok := s.files[path]
	if !ok {
		return nil, models.ErrInstructionFileNotFound
	}
	return io.NopCloser(strings.NewReader(content)), nil
}

func (s *analysisInstructionStorage) Delete(ctx context.Context, path string) error {
	panic("not implemented")
}

type analysisCallRepository struct {
	call          models.Call
	updatedStatus bool
	lastStatus    models.CallStatus
}

func (r *analysisCallRepository) CreateCall(ctx context.Context, call models.Call) (models.Call, error) {
	panic("not implemented")
}

func (r *analysisCallRepository) CreateCallWithProcessingJob(ctx context.Context, call models.Call, job models.ProcessingJob) (models.Call, error) {
	panic("not implemented")
}

func (r *analysisCallRepository) List(ctx context.Context, userID uuid.UUID) ([]models.Call, error) {
	panic("not implemented")
}

func (r *analysisCallRepository) ListFiltered(ctx context.Context, input models.ListCallsInput) (models.ListCallsResult, error) {
	panic("not implemented")
}

func (r *analysisCallRepository) GetFilterOptions(ctx context.Context, input models.CallFilterOptionsInput) (models.CallFilterOptions, error) {
	panic("not implemented")
}

func (r *analysisCallRepository) GetByUUID(ctx context.Context, id uuid.UUID, userID uuid.UUID) (models.Call, error) {
	if id != r.call.ID {
		return models.Call{}, models.ErrCallNotFound
	}
	return r.call, nil
}

func (r *analysisCallRepository) GetByUUIDForProcessing(ctx context.Context, id uuid.UUID) (models.Call, error) {
	if id != r.call.ID {
		return models.Call{}, models.ErrCallNotFound
	}
	return r.call, nil
}

func (r *analysisCallRepository) UpdateCallTitle(ctx context.Context, id uuid.UUID, userID uuid.UUID, title string) (models.Call, error) {
	panic("not implemented")
}

func (r *analysisCallRepository) UpdateCallStatus(ctx context.Context, id uuid.UUID, status models.CallStatus) (models.Call, error) {
	r.updatedStatus = true
	r.lastStatus = status
	r.call.Status = status
	return r.call, nil
}

func (r *analysisCallRepository) SoftDeleteCall(ctx context.Context, id uuid.UUID, userID uuid.UUID, now time.Time, purgeAfter time.Time) (models.Call, error) {
	panic("not implemented")
}

func (r *analysisCallRepository) RestoreCall(ctx context.Context, id uuid.UUID, userID uuid.UUID) (models.Call, error) {
	panic("not implemented")
}

func (r *analysisCallRepository) ListDeletedCalls(ctx context.Context, input models.ListDeletedCallsInput) (models.ListDeletedCallsResult, error) {
	panic("not implemented")
}

func (r *analysisCallRepository) TakeNextForProcessing(ctx context.Context) (models.Call, error) {
	panic("not implemented")
}

type analysisTranscriptionRepository struct {
	transcription models.Transcription
}

func (r *analysisTranscriptionRepository) Create(ctx context.Context, transcription models.Transcription) (models.Transcription, error) {
	panic("not implemented")
}

func (r *analysisTranscriptionRepository) GetByCallUUID(ctx context.Context, callID uuid.UUID) (models.Transcription, error) {
	if callID != r.transcription.CallUUID {
		return models.Transcription{}, models.ErrTranscriptionNotFound
	}
	return r.transcription, nil
}

func (r *analysisTranscriptionRepository) MarkTranscribed(ctx context.Context, id uuid.UUID, text string, segments []models.TranscriptionSegment, words []models.TranscriptionWord, language *string) (models.Transcription, error) {
	panic("not implemented")
}

func (r *analysisTranscriptionRepository) MarkFailed(ctx context.Context, id uuid.UUID, errorMessage string) (models.Transcription, error) {
	panic("not implemented")
}

type analysisInstructionRepository struct {
	instructions map[models.AnalysisInstructionScope][]models.AnalysisInstruction
}

type analysisFolderInstructionReader struct{ instructions []models.AnalysisInstruction }

func (r *analysisFolderInstructionReader) ListInstructionsForCall(context.Context, uuid.UUID) ([]models.AnalysisInstruction, error) {
	return r.instructions, nil
}

func (r *analysisInstructionRepository) Create(ctx context.Context, instruction models.AnalysisInstruction) (models.AnalysisInstruction, error) {
	panic("not implemented")
}

func (r *analysisInstructionRepository) GetByUUID(ctx context.Context, id uuid.UUID) (models.AnalysisInstruction, error) {
	panic("not implemented")
}

func (r *analysisInstructionRepository) GetByUUIDIncludingInactive(ctx context.Context, id uuid.UUID) (models.AnalysisInstruction, error) {
	panic("not implemented")
}

func (r *analysisInstructionRepository) ListVersions(ctx context.Context, id uuid.UUID) ([]models.AnalysisInstructionVersion, error) {
	panic("not implemented")
}

func (r *analysisInstructionRepository) GetVersion(ctx context.Context, instructionID, versionID uuid.UUID) (models.AnalysisInstructionVersion, error) {
	panic("not implemented")
}

func (r *analysisInstructionRepository) List(ctx context.Context, input models.ListAnalysisInstructionsInput) ([]models.AnalysisInstruction, error) {
	instructions := r.instructions[input.Scope]
	result := make([]models.AnalysisInstruction, 0, len(instructions))
	for _, instruction := range instructions {
		if input.Scope == models.AnalysisInstructionScopeCompany && instruction.CompanyUUID != input.CompanyUUID {
			continue
		}
		if input.Scope == models.AnalysisInstructionScopeDepartment &&
			(instruction.CompanyUUID != input.CompanyUUID || instruction.DepartmentUUID != input.DepartmentUUID) {
			continue
		}
		result = append(result, instruction)
	}
	return result, nil
}

func (r *analysisInstructionRepository) CountActive(ctx context.Context, input models.ListAnalysisInstructionsInput) (int, error) {
	panic("not implemented")
}

func (r *analysisInstructionRepository) Update(ctx context.Context, input models.UpdateAnalysisInstructionRepositoryInput) (models.AnalysisInstruction, error) {
	panic("not implemented")
}

func (r *analysisInstructionRepository) Reorder(ctx context.Context, items []models.ReorderAnalysisInstructionItem) error {
	panic("not implemented")
}

func (r *analysisInstructionRepository) Deactivate(ctx context.Context, id uuid.UUID) error {
	panic("not implemented")
}

type analysisRepository struct {
	analysisID       uuid.UUID
	callID           uuid.UUID
	doneResult       models.AnalysisResult
	createdTime      time.Time
	existingAnalysis *models.CallAnalysis
	markedProcessing bool
	markedFailed     bool
	failedMessage    string
}

func (r *analysisRepository) Create(ctx context.Context, analysis models.CallAnalysis) (models.CallAnalysis, error) {
	r.createdTime = analysis.CreatedAt
	if r.analysisID != uuid.Nil {
		analysis.ID = r.analysisID
	}
	if r.callID != uuid.Nil {
		analysis.CallUUID = r.callID
	}
	return analysis, nil
}

func (r *analysisRepository) GetByCallUUID(ctx context.Context, callID uuid.UUID) (models.CallAnalysis, error) {
	if r.existingAnalysis == nil || callID != r.existingAnalysis.CallUUID {
		return models.CallAnalysis{}, models.ErrAnalysisNotFound
	}
	return *r.existingAnalysis, nil
}

func (r *analysisRepository) MarkProcessing(ctx context.Context, id uuid.UUID) (models.CallAnalysis, error) {
	r.markedProcessing = true
	return models.CallAnalysis{
		ID:        id,
		CallUUID:  r.callID,
		Status:    models.CallAnalysisStatusProcessing,
		Provider:  "test",
		CreatedAt: r.createdTime,
		UpdatedAt: time.Now().UTC(),
	}, nil
}

func (r *analysisRepository) MarkDone(ctx context.Context, id uuid.UUID, result models.AnalysisResult) (models.CallAnalysis, error) {
	r.doneResult = result
	return models.CallAnalysis{
		ID:         id,
		CallUUID:   r.callID,
		Status:     models.CallAnalysisStatusDone,
		Provider:   "test",
		Model:      result.Model,
		ResultJSON: result.ResultJSON,
		ResultText: result.ResultText,
		CreatedAt:  r.createdTime,
		UpdatedAt:  time.Now().UTC(),
	}, nil
}

func (r *analysisRepository) MarkFailed(ctx context.Context, id uuid.UUID, errorMessage string) (models.CallAnalysis, error) {
	r.markedFailed = true
	r.failedMessage = errorMessage
	return models.CallAnalysis{
		ID:           id,
		CallUUID:     r.callID,
		Status:       models.CallAnalysisStatusFailed,
		Provider:     "test",
		ErrorMessage: &errorMessage,
		CreatedAt:    r.createdTime,
		UpdatedAt:    time.Now().UTC(),
	}, nil
}
