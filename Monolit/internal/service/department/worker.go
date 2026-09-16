package department

import (
	"context"
	"time"

	"go.uber.org/zap"
)

// TransferExpiryWorker closes transfer requests nobody decided on, so the
// pending-request slot does not stay blocked forever.
type TransferExpiryWorker struct {
	service  *Service
	interval time.Duration
}

func NewTransferExpiryWorker(service *Service, interval time.Duration) *TransferExpiryWorker {
	if interval <= 0 {
		interval = time.Hour
	}

	return &TransferExpiryWorker{service: service, interval: interval}
}

func (w *TransferExpiryWorker) Run(ctx context.Context) <-chan struct{} {
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

func (w *TransferExpiryWorker) runOnce(ctx context.Context) {
	expired, err := w.service.ExpirePendingTransfers(ctx)
	if err != nil {
		w.service.log.Warn(ctx, "failed to expire department transfers", zap.Error(err))
		return
	}
	if expired > 0 {
		w.service.log.Info(ctx, "department transfers expired", zap.Int64("count", expired))
	}
}
