//go:build integration

package call

import (
	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

// createProcessingCall seeds a personal call the queue has already picked up.
func (s *RepositorySuite) createProcessingCall(ownerID uuid.UUID) models.Call {
	call, err := s.repository.CreateCall(s.ctx, models.Call{
		ID:                 uuid.New(),
		Title:              "call",
		Status:             models.CallStatusProcessing,
		AudioPath:          "audio/" + uuid.NewString() + ".ogg",
		OriginalFilename:   "call.ogg",
		MimeType:           "audio/ogg",
		SizeBytes:          1024,
		DurationSeconds:    60,
		UploadedByUserUUID: uuid.NullUUID{UUID: ownerID, Valid: true},
		VisibilityScope:    models.CallVisibilityScopePersonal,
	})
	s.Require().NoError(err)

	return call
}

// Cancelling stops the queue and leaves the call in place. Deleting a call
// mid-flight used to be the only way out, which left the worker chewing through
// retries on something nobody wanted.
func (s *RepositorySuite) TestCancelProcessingStopsTheQueueAndKeepsTheCall() {
	owner := s.createUser(uuid.NewString() + "@example.com").ID
	call := s.createProcessingCall(owner)

	jobID := uuid.New()
	_, err := s.db.ExecContext(s.ctx, `
		INSERT INTO processing_jobs (job_uuid, job_type, entity_uuid, status, attempts, max_attempts, available_at)
		VALUES ($1, 'transcribe_call', $2, 'pending', 0, 3, now())`, jobID, call.ID)
	s.Require().NoError(err)

	cancelled, err := s.repository.CancelCallProcessing(s.ctx, call.ID, owner)
	s.Require().NoError(err)
	s.Require().Equal(models.CallStatusCancelled, cancelled.Status)
	s.Require().Equal(call.AudioPath, cancelled.AudioPath, "the recording is kept: the call can be started again")

	var jobs int
	s.Require().NoError(s.db.QueryRowContext(s.ctx, `SELECT count(*) FROM processing_jobs WHERE entity_uuid=$1`, call.ID).Scan(&jobs))
	s.Require().Zero(jobs, "a cancelled call must not keep the queue busy")

	// Cancelling twice is not an error the second time round, it is simply a
	// call that is no longer being processed.
	_, err = s.repository.CancelCallProcessing(s.ctx, call.ID, owner)
	s.Require().ErrorIs(err, models.ErrCallNotFound)
}

// Somebody who may not delete a call may not cancel its processing either: both
// throw away work that was already paid for.
func (s *RepositorySuite) TestCancelProcessingFollowsDeletionRights() {
	owner := s.createUser(uuid.NewString() + "@example.com").ID
	stranger := s.createUser(uuid.NewString() + "@example.com").ID
	call := s.createProcessingCall(owner)

	_, err := s.repository.CancelCallProcessing(s.ctx, call.ID, stranger)
	s.Require().ErrorIs(err, models.ErrCallNotFound)

	var status string
	s.Require().NoError(s.db.QueryRowContext(s.ctx, `SELECT status FROM calls WHERE call_uuid=$1`, call.ID).Scan(&status))
	s.Require().Equal(string(models.CallStatusProcessing), status)
}
