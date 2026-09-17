//go:build integration

package call

import (
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func (s *RepositorySuite) restartJob(callID uuid.UUID) models.ProcessingJob {
	now := time.Now().UTC()

	return models.ProcessingJob{
		ID:                uuid.New(),
		Type:              models.ProcessingJobTypeTranscribeCall,
		TranscriptionMode: models.TranscriptionModeStandard,
		EntityUUID:        callID,
		Status:            models.ProcessingJobStatusPending,
		MaxAttempts:       3,
		AvailableAt:       now,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
}

// Cancelling is a pause, not a verdict: the recording stayed, so the work can
// start again and a fresh job goes back into the queue.
func (s *RepositorySuite) TestRestartProcessingQueuesTheCallAgain() {
	owner := s.createUser(uuid.NewString() + "@example.com").ID
	call := s.createProcessingCall(owner)

	cancelled, err := s.repository.CancelCallProcessing(s.ctx, call.ID, owner)
	s.Require().NoError(err)
	s.Require().Equal(models.CallStatusCancelled, cancelled.Status)

	restarted, err := s.repository.RestartCallProcessing(s.ctx, call.ID, owner, false, s.restartJob(call.ID))
	s.Require().NoError(err)
	s.Require().Equal(models.CallStatusNew, restarted.Status)
	s.Require().False(restarted.TranscriptionOnly)

	var jobs int
	s.Require().NoError(s.db.QueryRowContext(s.ctx, `SELECT count(*) FROM processing_jobs WHERE entity_uuid=$1 AND status='pending'`, call.ID).Scan(&jobs))
	s.Require().Equal(1, jobs, "restarting puts the call back in the queue")

	// A call that is already running cannot be restarted on top of itself.
	_, err = s.repository.RestartCallProcessing(s.ctx, call.ID, owner, false, s.restartJob(call.ID))
	s.Require().ErrorIs(err, models.ErrCallNotFound)
}

// Restarting in the cheaper mode drops the analysis half before the work begins.
func (s *RepositorySuite) TestRestartProcessingCanSwitchToTranscriptionOnly() {
	owner := s.createUser(uuid.NewString() + "@example.com").ID
	call := s.createProcessingCall(owner)

	_, err := s.repository.CancelCallProcessing(s.ctx, call.ID, owner)
	s.Require().NoError(err)

	restarted, err := s.repository.RestartCallProcessing(s.ctx, call.ID, owner, true, s.restartJob(call.ID))
	s.Require().NoError(err)
	s.Require().True(restarted.TranscriptionOnly)
}

// Somebody who may not cancel a call may not restart it either.
func (s *RepositorySuite) TestRestartProcessingFollowsDeletionRights() {
	owner := s.createUser(uuid.NewString() + "@example.com").ID
	stranger := s.createUser(uuid.NewString() + "@example.com").ID
	call := s.createProcessingCall(owner)

	_, err := s.repository.CancelCallProcessing(s.ctx, call.ID, owner)
	s.Require().NoError(err)

	_, err = s.repository.RestartCallProcessing(s.ctx, call.ID, stranger, false, s.restartJob(call.ID))
	s.Require().ErrorIs(err, models.ErrCallNotFound)
}

// Switching a transcribed call to transcription only costs nothing: the
// transcript is there, so the call is simply finished and no job is queued.
func (s *RepositorySuite) TestSwitchToTranscriptionOnlyFinishesAnAlreadyTranscribedCall() {
	owner := s.createUser(uuid.NewString() + "@example.com").ID
	call := s.createProcessingCall(owner)

	_, err := s.db.ExecContext(s.ctx, `
		INSERT INTO call_transcriptions(transcription_uuid, call_uuid, status, provider, text, segments, words)
		VALUES ($1, $2, 'transcribed', 'mock', 'Привет', '[]', '[]')`, uuid.New(), call.ID)
	s.Require().NoError(err)

	_, err = s.repository.CancelCallProcessing(s.ctx, call.ID, owner)
	s.Require().NoError(err)

	switched, err := s.repository.SwitchCallToTranscriptionOnly(s.ctx, call.ID, owner)
	s.Require().NoError(err)
	s.Require().Equal(models.CallStatusTranscribed, switched.Status)
	s.Require().True(switched.TranscriptionOnly)

	var jobs int
	s.Require().NoError(s.db.QueryRowContext(s.ctx, `SELECT count(*) FROM processing_jobs WHERE entity_uuid=$1`, call.ID).Scan(&jobs))
	s.Require().Zero(jobs, "nothing is sent to a provider, so nothing is queued")
}

// Without a transcript there is nothing to switch to: the call has to go
// through the queue like any other.
func (s *RepositorySuite) TestSwitchToTranscriptionOnlyNeedsATranscript() {
	owner := s.createUser(uuid.NewString() + "@example.com").ID
	call := s.createProcessingCall(owner)

	_, err := s.repository.CancelCallProcessing(s.ctx, call.ID, owner)
	s.Require().NoError(err)

	_, err = s.repository.SwitchCallToTranscriptionOnly(s.ctx, call.ID, owner)
	s.Require().ErrorIs(err, models.ErrCallNotFound)
}
