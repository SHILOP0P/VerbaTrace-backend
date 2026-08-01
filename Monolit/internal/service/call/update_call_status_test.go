package call

import (
	"errors"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
)

func (s *ServiceSuite) TestUpdateCallStatusAllowedTransitions() {
	tests := []struct {
		name string
		from models.CallStatus
		to   models.CallStatus
	}{
		{name: "same status is idempotent", from: models.CallStatusNew, to: models.CallStatusNew},
		{name: "new to processing", from: models.CallStatusNew, to: models.CallStatusProcessing},
		{name: "new to failed", from: models.CallStatusNew, to: models.CallStatusFailed},
		{name: "processing to transcribed", from: models.CallStatusProcessing, to: models.CallStatusTranscribed},
		{name: "processing to failed", from: models.CallStatusProcessing, to: models.CallStatusFailed},
		{name: "transcribed to analyzed", from: models.CallStatusTranscribed, to: models.CallStatusAnalyzed},
		{name: "transcribed to failed", from: models.CallStatusTranscribed, to: models.CallStatusFailed},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			s.SetupTest()
			callID := uuid.New()

			s.repository.EXPECT().
				GetByUUIDForProcessing(mock.Anything, callID).
				Return(models.Call{ID: callID, Status: tt.from}, nil).
				Once()
			s.repository.EXPECT().
				UpdateCallStatus(mock.Anything, callID, tt.to).
				Return(models.Call{ID: callID, Status: tt.to}, nil).
				Once()

			got, err := s.service.UpdateCallStatus(s.ctx, models.UpdateCallStatusInput{
				CallUUID: callID,
				Status:   tt.to,
			})

			s.Require().NoError(err)
			s.Require().Equal(tt.to, got.Status)
		})
	}
}

func (s *ServiceSuite) TestUpdateCallStatusForbiddenTransitions() {
	tests := []struct {
		name string
		from models.CallStatus
		to   models.CallStatus
	}{
		{name: "new to transcribed", from: models.CallStatusNew, to: models.CallStatusTranscribed},
		{name: "new to analyzed", from: models.CallStatusNew, to: models.CallStatusAnalyzed},
		{name: "processing to analyzed", from: models.CallStatusProcessing, to: models.CallStatusAnalyzed},
		{name: "transcribed to processing", from: models.CallStatusTranscribed, to: models.CallStatusProcessing},
		{name: "analyzed is final", from: models.CallStatusAnalyzed, to: models.CallStatusProcessing},
		{name: "failed is final", from: models.CallStatusFailed, to: models.CallStatusProcessing},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			s.SetupTest()
			callID := uuid.New()

			s.repository.EXPECT().
				GetByUUIDForProcessing(mock.Anything, callID).
				Return(models.Call{ID: callID, Status: tt.from}, nil).
				Once()

			_, err := s.service.UpdateCallStatus(s.ctx, models.UpdateCallStatusInput{
				CallUUID: callID,
				Status:   tt.to,
			})

			s.Require().ErrorIs(err, models.ErrInvalidCallStatusTransition)
		})
	}
}

func (s *ServiceSuite) TestUpdateCallStatusNilCallUUID() {
	_, err := s.service.UpdateCallStatus(s.ctx, models.UpdateCallStatusInput{
		CallUUID: uuid.Nil,
		Status:   models.CallStatusProcessing,
	})

	s.Require().ErrorIs(err, models.ErrCallNotFound)
}

func (s *ServiceSuite) TestUpdateCallStatusInvalidTargetStatus() {
	_, err := s.service.UpdateCallStatus(s.ctx, models.UpdateCallStatusInput{
		CallUUID: uuid.New(),
		Status:   models.CallStatus("done"),
	})

	s.Require().ErrorIs(err, models.ErrInvalidCallStatus)
}

func (s *ServiceSuite) TestUpdateCallStatusGetError() {
	callID := uuid.New()
	repoErr := errors.New("db get failed")

	s.repository.EXPECT().
		GetByUUIDForProcessing(mock.Anything, callID).
		Return(models.Call{}, repoErr).
		Once()

	_, err := s.service.UpdateCallStatus(s.ctx, models.UpdateCallStatusInput{
		CallUUID: callID,
		Status:   models.CallStatusProcessing,
	})

	s.Require().ErrorIs(err, repoErr)
}

func (s *ServiceSuite) TestUpdateCallStatusUpdateError() {
	callID := uuid.New()
	repoErr := errors.New("db update failed")

	s.repository.EXPECT().
		GetByUUIDForProcessing(mock.Anything, callID).
		Return(models.Call{ID: callID, Status: models.CallStatusNew}, nil).
		Once()
	s.repository.EXPECT().
		UpdateCallStatus(mock.Anything, callID, models.CallStatusProcessing).
		Return(models.Call{}, repoErr).
		Once()

	_, err := s.service.UpdateCallStatus(s.ctx, models.UpdateCallStatusInput{
		CallUUID: callID,
		Status:   models.CallStatusProcessing,
	})

	s.Require().ErrorIs(err, repoErr)
}
