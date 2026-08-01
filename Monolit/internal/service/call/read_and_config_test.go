package call

import (
	"context"
	"errors"
	"testing"

	"verbatrace/monolit/internal/models"
	repositoryMocks "verbatrace/monolit/internal/repository/mocks"
	callMocks "verbatrace/monolit/internal/service/call/mocks"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
)

func TestList(t *testing.T) {
	repository := repositoryMocks.NewCallRepository(t)
	service := NewService(repository, nil, nil, nil, nil)
	userID := uuid.New()
	want := []models.Call{{ID: uuid.New()}}
	repository.EXPECT().List(mock.Anything, userID).Return(want, nil).Once()

	got, err := service.List(context.Background(), userID)
	if err != nil || len(got) != 1 || got[0].ID != want[0].ID {
		t.Fatalf("List = %+v, %v", got, err)
	}
}

func TestGetTranscriptionByCallUUID(t *testing.T) {
	ctx := context.Background()
	callID := uuid.New()
	userID := uuid.New()

	t.Run("repository not configured", func(t *testing.T) {
		service := NewService(repositoryMocks.NewCallRepository(t), nil, nil, nil, nil)
		if _, err := service.GetTranscriptionByCallUUID(ctx, callID, userID); err == nil {
			t.Fatal("expected configuration error")
		}
	})

	t.Run("call access error", func(t *testing.T) {
		callRepo := repositoryMocks.NewCallRepository(t)
		transcriptionRepo := repositoryMocks.NewTranscriptionRepository(t)
		service := NewService(callRepo, nil, nil, nil, nil)
		service.SetTranscriptionRepository(transcriptionRepo)
		wantErr := errors.New("forbidden")
		callRepo.EXPECT().GetByUUID(mock.Anything, callID, userID).Return(models.Call{}, wantErr).Once()
		if _, err := service.GetTranscriptionByCallUUID(ctx, callID, userID); !errors.Is(err, wantErr) {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("success", func(t *testing.T) {
		callRepo := repositoryMocks.NewCallRepository(t)
		transcriptionRepo := repositoryMocks.NewTranscriptionRepository(t)
		service := NewService(callRepo, nil, nil, nil, nil)
		service.SetTranscriptionRepository(transcriptionRepo)
		want := models.Transcription{ID: uuid.New(), CallUUID: callID}
		callRepo.EXPECT().GetByUUID(mock.Anything, callID, userID).Return(models.Call{ID: callID}, nil).Once()
		transcriptionRepo.EXPECT().GetByCallUUID(mock.Anything, callID).Return(want, nil).Once()
		got, err := service.GetTranscriptionByCallUUID(ctx, callID, userID)
		if err != nil || got.ID != want.ID {
			t.Fatalf("GetTranscriptionByCallUUID = %+v, %v", got, err)
		}
	})
}

func TestServiceSetters(t *testing.T) {
	service := NewService(repositoryMocks.NewCallRepository(t), nil, nil, nil, nil)
	transcriptionRepo := repositoryMocks.NewTranscriptionRepository(t)
	limiter := callMocks.NewBillingLimiter(t)

	service.SetTranscriptionRepository(transcriptionRepo)
	service.SetProcessingJobRepository(nil)
	service.SetProcessingJobMaxAttempts(7)
	service.SetBillingLimiter(limiter)
	if service.transcriptionRepository != transcriptionRepo || service.processingJobRepository != nil ||
		service.processingJobMaxAttempts != 7 || service.billingLimiter != limiter {
		t.Fatalf("setters did not update service: %+v", service)
	}

	service.SetProcessingJobMaxAttempts(0)
	if service.processingJobMaxAttempts != defaultProcessingJobMaxAttempts {
		t.Fatalf("max attempts = %d", service.processingJobMaxAttempts)
	}
}
