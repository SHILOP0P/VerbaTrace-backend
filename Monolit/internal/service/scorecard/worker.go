package scorecard

import (
	"context"
	"time"

	"go.uber.org/zap"
)

const (
	defaultWorkerInterval = 5 * time.Second
	workerBatch           = 3
)

// Worker compiles scorecards apart from call processing, which runs one job at
// a time behind long transcriptions: an analysis waiting for a scorecard in
// that queue would wait for itself.
type Worker struct {
	service  *Service
	interval time.Duration
}

func NewWorker(service *Service, interval time.Duration) *Worker {
	if interval <= 0 {
		interval = defaultWorkerInterval
	}
	return &Worker{service: service, interval: interval}
}

func (w *Worker) Run(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(w.interval)
		defer ticker.Stop()
		for {
			for {
				taken, err := w.service.CompileDue(ctx, workerBatch)
				if err != nil && ctx.Err() == nil {
					w.service.log.Error(ctx, "scorecard worker batch failed", zap.Error(err))
				}
				if err != nil || taken < workerBatch {
					break
				}
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
