package company

import (
	"context"
	"time"

	"go.uber.org/zap"
)

// MembershipMaintenanceWorker keeps membership records tidy: it closes offers
// nobody answered and drops exclusion records that outlived their window.
type MembershipMaintenanceWorker struct {
	service  *Service
	interval time.Duration
}

func NewMembershipMaintenanceWorker(service *Service, interval time.Duration) *MembershipMaintenanceWorker {
	if interval <= 0 {
		interval = time.Hour
	}

	return &MembershipMaintenanceWorker{service: service, interval: interval}
}

func (w *MembershipMaintenanceWorker) Run(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})

	go func() {
		defer close(done)

		ticker := time.NewTicker(w.interval)
		defer ticker.Stop()

		w.runOnce(ctx)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				w.runOnce(ctx)
			}
		}
	}()

	return done
}

func (w *MembershipMaintenanceWorker) runOnce(ctx context.Context) {
	if expired, err := w.service.ExpireOwnershipOffers(ctx); err != nil {
		w.service.log.Warn(ctx, "failed to expire ownership offers", zap.Error(err))
	} else if expired > 0 {
		w.service.log.Info(ctx, "ownership offers expired", zap.Int64("count", expired))
	}

	if cleaned, err := w.service.CleanupMembershipRestrictions(ctx); err != nil {
		w.service.log.Warn(ctx, "failed to clean membership restrictions", zap.Error(err))
	} else if cleaned > 0 {
		w.service.log.Info(ctx, "membership restrictions cleaned", zap.Int64("count", cleaned))
	}
}
