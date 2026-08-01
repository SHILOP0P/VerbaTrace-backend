package processing_job

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"strings"

	model "verbatrace/monolit/internal/models"
)

const lastFailedJobsLimit = 10

func (r *Repository) GetMonitoring(ctx context.Context, input model.ProcessingMonitoringInput) (model.ProcessingMonitoring, error) {
	where, args := buildMonitoringFilters(input)
	queueQuery := fmt.Sprintf(`
	SELECT COUNT(*) FILTER (WHERE pj.status = 'pending')::int,
	       COUNT(*) FILTER (WHERE pj.status = 'running')::int,
	       COUNT(*) FILTER (WHERE pj.status = 'done')::int,
	       COUNT(*) FILTER (WHERE pj.status = 'failed')::int,
	       COUNT(*) FILTER (WHERE pj.status = 'pending' AND pj.attempts > 0)::int,
	       AVG(EXTRACT(EPOCH FROM (pj.updated_at - pj.created_at))) FILTER (WHERE pj.status = 'done')::float8
	FROM processing_jobs pj
	JOIN calls c ON c.call_uuid = pj.entity_uuid
	WHERE %s
	`, where)

	var monitoring model.ProcessingMonitoring
	var averageProcessing sql.NullFloat64
	err := r.db.QueryRowContext(ctx, queueQuery, args...).Scan(
		&monitoring.Queue.Pending,
		&monitoring.Queue.Running,
		&monitoring.Queue.Done,
		&monitoring.Queue.Failed,
		&monitoring.Queue.Retry,
		&averageProcessing,
	)
	if err != nil {
		return model.ProcessingMonitoring{}, fmt.Errorf("get processing monitoring queue: %w", err)
	}

	if averageProcessing.Valid {
		rounded := int(math.Round(averageProcessing.Float64))
		monitoring.AverageProcessingSeconds = &rounded
	}

	failedQuery := fmt.Sprintf(`
	SELECT pj.job_uuid,
	       pj.job_type,
	       pj.entity_uuid,
	       pj.attempts,
	       pj.last_error,
	       pj.updated_at
	FROM processing_jobs pj
	JOIN calls c ON c.call_uuid = pj.entity_uuid
	WHERE %s
	  AND pj.status = 'failed'
	ORDER BY pj.updated_at DESC
	LIMIT %d
	`, where, lastFailedJobsLimit)

	rows, err := r.db.QueryContext(ctx, failedQuery, args...)
	if err != nil {
		return model.ProcessingMonitoring{}, fmt.Errorf("get failed processing jobs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var job model.FailedProcessingJob
		if err := rows.Scan(&job.ID, &job.Type, &job.EntityUUID, &job.Attempts, &job.LastError, &job.UpdatedAt); err != nil {
			return model.ProcessingMonitoring{}, fmt.Errorf("scan failed processing jobs: %w", err)
		}
		monitoring.LastFailedJobs = append(monitoring.LastFailedJobs, job)
	}
	if err := rows.Err(); err != nil {
		return model.ProcessingMonitoring{}, fmt.Errorf("scan failed processing jobs: %w", err)
	}

	if monitoring.LastFailedJobs == nil {
		monitoring.LastFailedJobs = []model.FailedProcessingJob{}
	}
	monitoring.Services = model.ProcessingServicesStatus{
		Transcriber: "ok",
		Analyzer:    "ok",
		Storage:     "ok",
	}

	return monitoring, nil
}

func buildMonitoringFilters(input model.ProcessingMonitoringInput) (string, []any) {
	var args []any
	conditions := []string{"pj.entity_uuid = c.call_uuid"}

	if input.CompanyUUID.Valid {
		args = append(args, input.CompanyUUID.UUID)
		conditions = append(conditions, fmt.Sprintf("c.company_uuid = $%d", len(args)))
	}
	if input.From != nil {
		args = append(args, *input.From)
		conditions = append(conditions, fmt.Sprintf("pj.created_at >= $%d", len(args)))
	}
	if input.To != nil {
		args = append(args, *input.To)
		conditions = append(conditions, fmt.Sprintf("pj.created_at <= $%d", len(args)))
	}

	return strings.Join(conditions, " AND "), args
}
