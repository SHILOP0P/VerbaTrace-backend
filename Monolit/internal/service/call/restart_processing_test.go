package call

import (
	"context"

	"verbatrace/monolit/internal/models"
	repositoryMocks "verbatrace/monolit/internal/repository/mocks"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
)

// restartableRepository is the call repository plus the two statements that
// restarting needs. They live behind a type assertion in the service, so the
// test has to supply them the same way the real repository does.
type restartableRepository struct {
	*repositoryMocks.CallRepository
	restarted  *models.ProcessingJob
	switched   bool
	restartErr error
	switchErr  error
}

func (r *restartableRepository) RestartCallProcessing(_ context.Context, id, _ uuid.UUID, transcriptionOnly bool, job models.ProcessingJob) (models.Call, error) {
	if r.restartErr != nil {
		return models.Call{}, r.restartErr
	}
	r.restarted = &job

	return models.Call{ID: id, Status: models.CallStatusNew, TranscriptionOnly: transcriptionOnly}, nil
}

func (r *restartableRepository) SwitchCallToTranscriptionOnly(_ context.Context, id, _ uuid.UUID) (models.Call, error) {
	if r.switchErr != nil {
		return models.Call{}, r.switchErr
	}
	r.switched = true

	return models.Call{ID: id, Status: models.CallStatusTranscribed, TranscriptionOnly: true}, nil
}

func (s *ServiceSuite) restartableService() *restartableRepository {
	repository := &restartableRepository{CallRepository: s.repository}
	s.service.repository = repository

	return repository
}

// A cancelled call goes back into the queue with a fresh job.
func (s *ServiceSuite) TestRestartProcessingQueuesTheCallAgain() {
	repository := s.restartableService()
	callID := uuid.New()
	userID := uuid.New()

	s.repository.EXPECT().
		GetByUUID(mock.Anything, callID, userID).
		Return(models.Call{ID: callID, Status: models.CallStatusCancelled}, nil).
		Once()

	call, err := s.service.RestartProcessing(s.ctx, callID, userID, false)

	s.Require().NoError(err)
	s.Require().Equal(models.CallStatusNew, call.Status)
	s.Require().NotNil(repository.restarted)
	s.Require().Equal(models.ProcessingJobTypeTranscribeCall, repository.restarted.Type)
	s.Require().Equal(models.ProcessingJobStatusPending, repository.restarted.Status)
}

// Only a cancelled call can be restarted: one the queue still owns would end up
// with two jobs racing over the same recording.
func (s *ServiceSuite) TestRestartProcessingRefusesACallThatIsNotCancelled() {
	s.restartableService()
	callID := uuid.New()
	userID := uuid.New()

	s.repository.EXPECT().
		GetByUUID(mock.Anything, callID, userID).
		Return(models.Call{ID: callID, Status: models.CallStatusProcessing}, nil).
		Once()

	_, err := s.service.RestartProcessing(s.ctx, callID, userID, false)

	s.Require().ErrorIs(err, models.ErrInvalidCallStatusTransition)
}

// A test call is read only: it exists to prove an upload works, nothing else.
func (s *ServiceSuite) TestRestartProcessingRefusesATestCall() {
	s.restartableService()
	callID := uuid.New()
	userID := uuid.New()

	s.repository.EXPECT().
		GetByUUID(mock.Anything, callID, userID).
		Return(models.Call{ID: callID, Status: models.CallStatusCancelled, IsTest: true}, nil).
		Once()

	_, err := s.service.RestartProcessing(s.ctx, callID, userID, true)

	s.Require().ErrorIs(err, models.ErrTestCallReadOnly)
}

// With a transcript already in hand, switching to transcription only finishes
// the call on the spot: nothing goes to a provider, so nothing is queued.
func (s *ServiceSuite) TestRestartProcessingSwitchesAnAlreadyTranscribedCallForFree() {
	repository := s.restartableService()
	transcriptions := repositoryMocks.NewTranscriptionRepository(s.T())
	s.service.SetTranscriptionRepository(transcriptions)

	callID := uuid.New()
	userID := uuid.New()
	text := "Привет"

	s.repository.EXPECT().
		GetByUUID(mock.Anything, callID, userID).
		Return(models.Call{ID: callID, Status: models.CallStatusCancelled}, nil).
		Once()
	transcriptions.EXPECT().
		GetByCallUUID(mock.Anything, callID).
		Return(models.Transcription{CallUUID: callID, Status: models.TranscriptionStatusTranscribed, Text: &text}, nil).
		Once()

	call, err := s.service.RestartProcessing(s.ctx, callID, userID, true)

	s.Require().NoError(err)
	s.Require().True(repository.switched)
	s.Require().Nil(repository.restarted, "an existing transcript is not transcribed a second time")
	s.Require().Equal(models.CallStatusTranscribed, call.Status)
	s.Require().True(call.TranscriptionOnly)
}
