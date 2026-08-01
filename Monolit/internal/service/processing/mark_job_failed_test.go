package processing

import (
	"context"
	"errors"
	"testing"

	"verbatrace/monolit/internal/logger"
	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func TestMarkJobFailedFinalizesAnalysisJob(t *testing.T) {
	ctx := context.Background()
	callID := uuid.New()
	cause := errors.New("openrouter analysis failed with status 429")
	processor := &failedAnalysisProcessor{}
	service := &Service{
		analysisProcessor: processor,
		log:               logger.NewNop(),
	}

	service.MarkJobFailed(ctx, models.ProcessingJob{
		ID:         uuid.New(),
		Type:       models.ProcessingJobTypeAnalyzeCall,
		EntityUUID: callID,
	}, cause)

	if !processor.called {
		t.Fatal("analysis failure was not finalized")
	}
	if processor.callID != callID {
		t.Fatalf("call id = %s, want %s", processor.callID, callID)
	}
	if processor.cause != cause {
		t.Fatalf("cause = %v, want %v", processor.cause, cause)
	}
}

type failedAnalysisProcessor struct {
	called bool
	callID uuid.UUID
	cause  error
}

func (p *failedAnalysisProcessor) ProcessAnalyzeCall(ctx context.Context, callID uuid.UUID) error {
	return nil
}

func (p *failedAnalysisProcessor) MarkAnalyzeCallFailed(ctx context.Context, callID uuid.UUID, cause error) error {
	p.called = true
	p.callID = callID
	p.cause = cause
	return nil
}
