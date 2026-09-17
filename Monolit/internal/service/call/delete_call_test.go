package call

import (
	"context"
	"errors"
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
)

// expectReadyToDelete stands in for the check that a call is not still being
// processed. Deleting one mid-flight is refused now, so every deletion first
// reads the call to see where it stands.
func (s *ServiceSuite) expectReadyToDelete(callID, userID uuid.UUID) {
	s.repository.EXPECT().
		GetByUUID(mock.Anything, callID, userID).
		Return(models.Call{ID: callID, Status: models.CallStatusAnalyzed}, nil).
		Once()
}

// A call the queue is still working on keeps its files and its credit
// reservation, so it has to be cancelled before it can be thrown away.
func (s *ServiceSuite) TestDeleteCallRefusesWhileStillProcessing() {
	callID := uuid.New()
	userID := uuid.New()

	for _, status := range []models.CallStatus{models.CallStatusNew, models.CallStatusProcessing, models.CallStatusAwaitingCredits} {
		s.repository.EXPECT().
			GetByUUID(mock.Anything, callID, userID).
			Return(models.Call{ID: callID, Status: status}, nil).
			Once()

		s.Require().ErrorIs(s.service.DeleteCall(s.ctx, callID, userID), models.ErrCallProcessingInProgress)
	}
}

func (s *ServiceSuite) TestDeleteCallMovesCallToBin() {
	callID := uuid.New()
	userID := uuid.New()

	s.expectReadyToDelete(callID, userID)
	s.repository.EXPECT().
		SoftDeleteCall(mock.Anything, callID, userID, mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, id uuid.UUID, _ uuid.UUID, now time.Time, purgeAfter time.Time) (models.Call, error) {
			s.Require().Equal(models.CallBinRetention, purgeAfter.Sub(now))
			return models.Call{ID: id}, nil
		}).
		Once()

	s.Require().NoError(s.service.DeleteCall(s.ctx, callID, userID))
}

func (s *ServiceSuite) TestDeleteCallKeepsFiles() {
	callID := uuid.New()
	userID := uuid.New()

	// Files must survive the bin: a restore has to bring the audio back.
	s.expectReadyToDelete(callID, userID)
	s.repository.EXPECT().
		SoftDeleteCall(mock.Anything, callID, userID, mock.Anything, mock.Anything).
		Return(models.Call{ID: callID, AudioPath: "uploads/call.wav", ASRCachePath: "asr/call.ogg"}, nil).
		Once()

	s.Require().NoError(s.service.DeleteCall(s.ctx, callID, userID))
	s.audioStorage.AssertNotCalled(s.T(), "Delete", mock.Anything, mock.Anything)
}

func (s *ServiceSuite) TestDeleteCallReturnsRepositoryError() {
	callID := uuid.New()
	userID := uuid.New()
	repoErr := errors.New("delete failed")

	s.expectReadyToDelete(callID, userID)
	s.repository.EXPECT().
		SoftDeleteCall(mock.Anything, callID, userID, mock.Anything, mock.Anything).
		Return(models.Call{}, repoErr).
		Once()

	s.Require().ErrorIs(s.service.DeleteCall(s.ctx, callID, userID), repoErr)
}

func (s *ServiceSuite) TestRestoreCallReturnsCall() {
	callID := uuid.New()
	userID := uuid.New()

	s.repository.EXPECT().RestoreCall(mock.Anything, callID, userID).Return(models.Call{ID: callID}, nil).Once()

	call, err := s.service.RestoreCall(s.ctx, callID, userID)

	s.Require().NoError(err)
	s.Require().Equal(callID, call.ID)
}

func (s *ServiceSuite) TestRestoreCallMapsNotFound() {
	callID := uuid.New()
	userID := uuid.New()

	s.repository.EXPECT().RestoreCall(mock.Anything, callID, userID).Return(models.Call{}, models.ErrCallNotFound).Once()

	_, err := s.service.RestoreCall(s.ctx, callID, userID)

	s.Require().ErrorIs(err, models.ErrCallNotFound)
}

func (s *ServiceSuite) TestListDeletedCallsNormalizesPaging() {
	userID := uuid.New()

	s.repository.EXPECT().
		ListDeletedCalls(mock.Anything, models.ListDeletedCallsInput{UserID: userID, Limit: 20}).
		Return(models.ListDeletedCallsResult{}, nil).
		Once()

	_, err := s.service.ListDeletedCalls(s.ctx, models.ListDeletedCallsInput{UserID: userID, Limit: 0, Offset: -5})

	s.Require().NoError(err)
}
