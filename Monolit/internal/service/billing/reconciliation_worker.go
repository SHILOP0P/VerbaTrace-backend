package billing

import (
	"context"
	"time"

	"verbatrace/monolit/internal/logger"
	billingrepo "verbatrace/monolit/internal/repository/billing"

	"go.uber.org/zap"
)

type reconciliationRepository interface {
	ReconcileCreditOperations(context.Context, time.Time, int) (billingrepo.ReconciliationSummary, error)
}
type ReconciliationWorker struct {
	repo                 reconciliationRepository
	log                  logger.Logger
	interval, staleAfter time.Duration
	batch                int
}

func NewReconciliationWorker(repo reconciliationRepository, log logger.Logger) *ReconciliationWorker {
	if log == nil {
		log = logger.NewNop()
	}
	return &ReconciliationWorker{repo: repo, log: log, interval: 5 * time.Minute, staleAfter: 45 * time.Minute, batch: 200}
}
func (w *ReconciliationWorker) Run(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(w.interval)
		defer ticker.Stop()
		for {
			summary, err := w.repo.ReconcileCreditOperations(ctx, time.Now().UTC().Add(-w.staleAfter), w.batch)
			if err != nil && ctx.Err() == nil {
				w.log.Error(ctx, "credit reconciliation failed", zap.Error(err))
			} else if summary.Mismatched > 0 {
				w.log.Warn(ctx, "credit reconciliation found stale operations", zap.Int64("count", summary.Mismatched))
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return done
}
