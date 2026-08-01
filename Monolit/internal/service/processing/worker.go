package processing

import (
	"context"
	"errors"
	"time"

	"verbatrace/monolit/internal/logger"
	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

const (
	defaultPollInterval = 2 * time.Second
	// Keep database claiming independent from the potentially long-running
	// transcription context. A stalled database connection must not silently
	// stop the worker from seeing every subsequently uploaded call.
	takeNextTimeout = 10 * time.Second
	// Analyze requests are sent to an external LLM provider. A conservative
	// default prevents a batch upload from creating a rate-limit burst.
	defaultWorkerLimit = 1
	defaultRetryDelay  = 1 * time.Minute
	// A long cloud transcription can take several minutes. Leave a margin so
	// another worker never reclaims the same healthy job midway.
	defaultStaleAfter = 30 * time.Minute
)

type Worker struct {
	service      *Service
	pollInterval time.Duration
	workerLimit  int
	retryDelay   time.Duration
	staleAfter   time.Duration
	workerID     string
	log          logger.Logger
}

type WorkerOptions struct {
	PollInterval time.Duration
	Limit        int
	RetryDelay   time.Duration
	StaleAfter   time.Duration
}

func NewWorker(service *Service, opts WorkerOptions, log logger.Logger) *Worker {
	if log == nil {
		log = logger.NewNop()
	}

	if opts.PollInterval <= 0 {
		opts.PollInterval = defaultPollInterval
	}
	if opts.Limit <= 0 {
		opts.Limit = defaultWorkerLimit
	}
	if opts.RetryDelay <= 0 {
		opts.RetryDelay = defaultRetryDelay
	}
	if opts.StaleAfter <= 0 {
		opts.StaleAfter = defaultStaleAfter
	}

	return &Worker{
		service:      service,
		pollInterval: opts.PollInterval,
		workerLimit:  opts.Limit,
		retryDelay:   opts.RetryDelay,
		staleAfter:   opts.StaleAfter,
		workerID:     "processing-worker-" + uuid.NewString(),
		log:          log,
	}
}

func (w *Worker) Run(ctx context.Context) {
	if w.service == nil || w.service.processingJobRepository == nil {
		w.log.Warn(ctx, "processing worker skipped", zap.String("reason", "service_not_configured"))
		return
	}

	w.log.Info(
		ctx,
		"processing worker started",
		zap.Duration("poll_interval", w.pollInterval),
		zap.Int("worker_limit", w.workerLimit),
		zap.Duration("retry_delay", w.retryDelay),
		zap.Duration("stale_after", w.staleAfter),
		zap.String("worker_id", w.workerID),
	)

	for {
		select {
		case <-ctx.Done():
			w.log.Info(ctx, "processing worker stopped")
			return
		default:
		}

		if err := w.runBatch(ctx); err != nil {
			if ctx.Err() != nil {
				w.log.Info(ctx, "processing worker stopped")
				return
			}

			w.log.Error(ctx, "processing worker batch failed", zap.Error(err))
		}

		select {
		case <-ctx.Done():
			w.log.Info(ctx, "processing worker stopped")
			return
		case <-time.After(w.pollInterval):
		}
	}
}

func (w *Worker) runBatch(ctx context.Context) error {
	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(w.workerLimit)

	claimed := 0

	for {
		takeCtx, cancelTake := context.WithTimeout(groupCtx, takeNextTimeout)
		job, err := w.service.processingJobRepository.TakeNext(takeCtx, w.workerID, w.staleAfter)
		cancelTake()
		if err != nil {
			if errors.Is(err, models.ErrNoProcessingJobs) {
				break
			}

			return err
		}

		claimed++
		w.log.Info(ctx, "processing job claimed", processingJobLogFields(job, w.workerID)...)

		claimedJob := job

		group.Go(func() error {
			startedAt := time.Now()
			w.log.Info(groupCtx, "processing job started", processingJobLogFields(claimedJob, w.workerID)...)

			if err := w.service.ProcessJob(groupCtx, claimedJob); err != nil {
				return w.handleJobError(groupCtx, claimedJob, err, time.Since(startedAt))
			}

			updatedJob, err := w.service.processingJobRepository.MarkDone(groupCtx, claimedJob.ID)
			if err != nil {
				w.log.Error(groupCtx, "processing job mark done failed", append(processingJobLogFields(claimedJob, w.workerID), zap.Error(err))...)
				return err
			}

			w.log.Info(
				groupCtx,
				"processing job done",
				append(processingJobLogFields(updatedJob, w.workerID), zap.Duration("duration", time.Since(startedAt)))...,
			)

			return nil
		})
	}

	if claimed == 0 {
		return nil
	}

	w.log.Info(ctx, "processing worker batch claimed jobs", zap.Int("count", claimed))

	return group.Wait()
}

func (w *Worker) handleJobError(ctx context.Context, job models.ProcessingJob, cause error, duration time.Duration) error {
	if isPermanentProcessingError(cause) {
		return w.handlePermanentJobError(ctx, job, cause, duration)
	}

	retryDelay := w.retryDelayFor(job)
	updatedJob, err := w.service.processingJobRepository.MarkRetry(ctx, job.ID, cause.Error(), retryDelay)
	if err != nil {
		w.log.Error(
			ctx,
			"processing job mark retry failed",
			append(
				processingJobLogFields(job, w.workerID),
				zap.NamedError("cause", cause),
				zap.Error(err),
			)...,
		)
		return err
	}

	message := "processing job scheduled for retry"
	fields := append(
		processingJobLogFields(updatedJob, w.workerID),
		zap.Duration("duration", duration),
		zap.Error(cause),
	)

	if updatedJob.Status == models.ProcessingJobStatusFailed {
		w.service.MarkJobFailed(ctx, job, cause)
		message = "processing job exhausted retries"
	} else {
		fields = append(fields, zap.Duration("retry_delay", retryDelay))
	}

	w.log.Warn(
		ctx,
		message,
		fields...,
	)

	return nil
}

// retryDelayFor exponentially spaces retryable failures. In particular, 429
// responses must not be retried as another simultaneous burst a minute later.
func (w *Worker) retryDelayFor(job models.ProcessingJob) time.Duration {
	exponent := job.Attempts - 1
	if exponent < 0 {
		exponent = 0
	}
	if exponent > 4 {
		exponent = 4
	}

	return w.retryDelay * time.Duration(1<<exponent)
}

func (w *Worker) handlePermanentJobError(ctx context.Context, job models.ProcessingJob, cause error, duration time.Duration) error {
	updatedJob, err := w.service.processingJobRepository.MarkFailed(ctx, job.ID, cause.Error())
	if err != nil {
		w.log.Error(
			ctx,
			"processing job mark failed status failed",
			append(
				processingJobLogFields(job, w.workerID),
				zap.NamedError("cause", cause),
				zap.Error(err),
			)...,
		)
		return err
	}

	w.service.MarkJobFailed(ctx, job, cause)

	w.log.Warn(
		ctx,
		"processing job failed without retry",
		append(
			processingJobLogFields(updatedJob, w.workerID),
			zap.String("failure_type", "permanent"),
			zap.Duration("duration", duration),
			zap.Error(cause),
		)...,
	)

	return nil
}

func processingJobLogFields(job models.ProcessingJob, workerID string) []zap.Field {
	return []zap.Field{
		zap.String("job_id", job.ID.String()),
		zap.String("job_type", string(job.Type)),
		zap.String("entity_id", job.EntityUUID.String()),
		zap.String("status", string(job.Status)),
		zap.Int("attempts", job.Attempts),
		zap.Int("max_attempts", job.MaxAttempts),
		zap.String("worker_id", workerID),
	}
}
