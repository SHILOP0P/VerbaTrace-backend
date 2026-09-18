package analyticsfacts

import (
	"context"
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// workerLock keeps one backfill pass at a time across API instances.
const workerLock int64 = 0x5645524246414354

const (
	defaultWorkerInterval = 10 * time.Minute
	defaultWorkerBatch    = 200
)

// SubjectRefresher resolves whom a call counts for; the worker fills it in for
// calls that predate subjects.
type SubjectRefresher interface {
	Refresh(ctx context.Context, callID uuid.UUID, actor uuid.NullUUID, cause string)
}

// Worker fills facts for calls analysed before facts existed and re-projects a
// call whose projection was lost: an analysis or a review newer than its facts.
// No queue is needed for projections because this catches every miss.
type Worker struct {
	service  *Service
	subjects SubjectRefresher
	interval time.Duration
	batch    int
}

func NewWorker(service *Service, subjects SubjectRefresher, interval time.Duration, batch int) *Worker {
	if interval <= 0 {
		interval = defaultWorkerInterval
	}
	if batch <= 0 {
		batch = defaultWorkerBatch
	}
	return &Worker{service: service, subjects: subjects, interval: interval, batch: batch}
}

func (w *Worker) Run(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		// The first pass runs soon after start, so a fresh deployment fills its
		// facts without waiting a whole interval.
		timer := time.NewTimer(time.Minute)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				w.RunOnce(ctx)
				timer.Reset(w.interval)
			}
		}
	}()
	return done
}

// RunOnce does one pass and reports how many calls it touched.
func (w *Worker) RunOnce(ctx context.Context) int {
	// A session lock must be taken and released on the same connection, not on
	// whichever one the pool hands out.
	conn, err := w.service.db.Conn(ctx)
	if err != nil {
		return 0
	}
	defer func() { _ = conn.Close() }()
	var locked bool
	if err := conn.QueryRowContext(ctx, `SELECT pg_try_advisory_lock($1)`, workerLock).Scan(&locked); err != nil || !locked {
		return 0
	}
	defer func() {
		_, _ = conn.ExecContext(context.Background(), `SELECT pg_advisory_unlock($1)`, workerLock)
	}()

	touched := 0
	if w.subjects != nil {
		for _, id := range w.ids(ctx, `
			SELECT c.call_uuid FROM calls c
			WHERE c.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM call_subject_states s WHERE s.call_uuid = c.call_uuid)
			ORDER BY c.created_at DESC LIMIT $1`) {
			w.subjects.Refresh(ctx, id, uuid.NullUUID{}, models.CallSubjectCauseBackfill)
			touched++
		}
	}
	for _, id := range w.ids(ctx, `
		SELECT a.call_uuid FROM call_analyses a
		LEFT JOIN analytics_call_facts f ON f.call_uuid = a.call_uuid
		LEFT JOIN call_quality_reviews q ON q.analysis_uuid = a.analysis_uuid AND q.status <> 'canceled'
		WHERE a.status = 'done'
		  AND (f.call_uuid IS NULL OR f.analysis_uuid <> a.analysis_uuid OR f.projected_at < a.updated_at
		       OR (q.published_at IS NOT NULL AND f.projected_at < q.updated_at))
		ORDER BY a.updated_at DESC LIMIT $1`) {
		w.service.Refresh(ctx, id)
		touched++
	}
	if touched > 0 {
		w.service.log.Info(ctx, "analytics facts caught up", zap.Int("calls", touched))
	}
	return touched
}

func (w *Worker) ids(ctx context.Context, query string) []uuid.UUID {
	rows, err := w.service.db.QueryContext(ctx, query, w.batch)
	if err != nil {
		w.service.log.Warn(ctx, "analytics facts backfill query failed", zap.Error(err))
		return nil
	}
	defer func() { _ = rows.Close() }()
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	return ids
}
