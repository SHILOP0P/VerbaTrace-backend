package call

import (
	"errors"

	"calllens/monolit/internal/models"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
)

func (s *ServiceSuite) TestDeleteCallSuccess() {
	callID := uuid.New()
	userID := uuid.New()
	audioPath := "uploads/call.wav"

	s.repository.EXPECT().GetByUUID(mock.Anything, callID, userID).
		Return(models.Call{ID: callID, AudioPath: audioPath}, nil).
		Once()
	s.repository.EXPECT().DeleteCall(mock.Anything, callID, userID).Return(nil).Once()
	s.audioStorage.EXPECT().Delete(mock.Anything, audioPath).Return(nil).Once()

	err := s.service.DeleteCall(s.ctx, callID, userID)

	s.Require().NoError(err)
}

func (s *ServiceSuite) TestDeleteCallRemovesASRCache() {
	callID := uuid.New()
	userID := uuid.New()
	call := models.Call{ID: callID, AudioPath: "uploads/call.wav", ASRCachePath: "asr/call.ogg"}

	s.repository.EXPECT().GetByUUID(mock.Anything, callID, userID).Return(call, nil).Once()
	s.repository.EXPECT().DeleteCall(mock.Anything, callID, userID).Return(nil).Once()
	s.audioStorage.EXPECT().Delete(mock.Anything, call.AudioPath).Return(nil).Once()
	s.audioStorage.EXPECT().Delete(mock.Anything, call.ASRCachePath).Return(nil).Once()

	s.Require().NoError(s.service.DeleteCall(s.ctx, callID, userID))
}

func (s *ServiceSuite) TestDeleteCallReturnsLookupError() {
	callID := uuid.New()
	userID := uuid.New()

	s.repository.EXPECT().GetByUUID(mock.Anything, callID, userID).
		Return(models.Call{}, models.ErrCallNotFound).
		Once()

	err := s.service.DeleteCall(s.ctx, callID, userID)

	s.Require().ErrorIs(err, models.ErrCallNotFound)
}

func (s *ServiceSuite) TestDeleteCallReturnsRepositoryDeleteError() {
	callID := uuid.New()
	userID := uuid.New()
	repoErr := errors.New("delete failed")

	s.repository.EXPECT().GetByUUID(mock.Anything, callID, userID).
		Return(models.Call{ID: callID, AudioPath: "uploads/call.wav"}, nil).
		Once()
	s.repository.EXPECT().DeleteCall(mock.Anything, callID, userID).Return(repoErr).Once()

	err := s.service.DeleteCall(s.ctx, callID, userID)

	s.Require().ErrorIs(err, repoErr)
}

func (s *ServiceSuite) TestDeleteCallReturnsAudioDeleteError() {
	callID := uuid.New()
	userID := uuid.New()
	audioErr := errors.New("audio delete failed")

	s.repository.EXPECT().GetByUUID(mock.Anything, callID, userID).
		Return(models.Call{ID: callID, AudioPath: "uploads/call.wav"}, nil).
		Once()
	s.repository.EXPECT().DeleteCall(mock.Anything, callID, userID).Return(nil).Once()
	s.audioStorage.EXPECT().Delete(mock.Anything, "uploads/call.wav").Return(audioErr).Once()

	err := s.service.DeleteCall(s.ctx, callID, userID)

	s.Require().ErrorIs(err, audioErr)
}
