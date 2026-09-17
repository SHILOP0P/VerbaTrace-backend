package billing

import (
	"context"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

// pendingCreditQueueRepository is implemented by the billing repository. It is a
// separate interface so the service keeps working in tests that never upload.
type pendingCreditQueueRepository interface {
	CountCallsAwaitingCredits(ctx context.Context, companyID, departmentID uuid.NullUUID, userID uuid.UUID) (int, error)
}

// CanQueueCallForCredits decides whether one more call may be accepted while
// there is no budget to process it. A call that cannot start yet still occupies
// disk, so the queue has a depth and the plan sets it: nil means no cap, zero
// means an upload is refused the moment the budget runs out.
//
// The depth is counted per department when the department has a credit limit of
// its own, and per company otherwise, so one department cannot fill the queue
// for the rest.
func (s *Service) CanQueueCallForCredits(ctx context.Context, userID uuid.UUID, companyID, departmentID uuid.NullUUID) error {
	queue := s.pendingQueue
	if queue == nil {
		return nil
	}

	var subscription models.Subscription
	var err error
	if companyID.Valid {
		subscription, err = s.activeBusinessSubscription(ctx, companyID.UUID)
	} else {
		subscription, err = s.activePersonalSubscription(ctx, userID)
	}
	if err != nil {
		return err
	}

	limit := subscription.Plan.PendingCreditCallsLimit
	if limit == nil {
		return nil
	}

	waiting, err := queue.CountCallsAwaitingCredits(ctx, companyID, departmentID, userID)
	if err != nil {
		return err
	}
	if waiting >= *limit {
		return models.ErrPendingCreditQueueFull
	}

	return nil
}
