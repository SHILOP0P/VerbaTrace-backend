package invitation

import (
	"context"
	"time"

	"go.uber.org/zap"
)

// ExpiryWorker marks invitations nobody answered in time. Without it a stale
// invitation keeps looking actionable in every list and report.
type ExpiryWorker struct {
	service  *Service
	interval time.Duration
}

func NewExpiryWorker(service *Service, interval time.Duration) *ExpiryWorker {
	if interval <= 0 {
		interval = time.Hour
	}

	return &ExpiryWorker{service: service, interval: interval}
}

func (w *ExpiryWorker) Run(ctx context.Context) <-chan struct{} {
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

func (w *ExpiryWorker) runOnce(ctx context.Context) {
	expired, err := w.service.ExpirePendingInvitations(ctx)
	if err != nil {
		w.service.log.Warn(ctx, "failed to expire invitations", zap.Error(err))
		return
	}
	if expired > 0 {
		w.service.log.Info(ctx, "invitations expired", zap.Int64("count", expired))
	}
}
