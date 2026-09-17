package analysis

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

const rerunRequestColumns = `request_uuid, call_uuid, company_uuid, department_uuid, requested_by_user_uuid,
	reason, status, decided_by_user_uuid, decided_at, comment, created_at, updated_at`

func (r *Repository) CreateRerunRequest(ctx context.Context, request models.AnalysisRerunRequest) (models.AnalysisRerunRequest, error) {
	query := `
	INSERT INTO call_analysis_rerun_requests (
		request_uuid, call_uuid, company_uuid, department_uuid, requested_by_user_uuid, reason, status, created_at, updated_at
	) VALUES ($1,$2,$3,$4,$5,$6,'pending',$7,$7)
	RETURNING ` + rerunRequestColumns

	row := r.db.QueryRowContext(ctx, query, request.ID, request.CallUUID, request.CompanyUUID, request.DepartmentUUID, request.RequestedByUserUUID, request.Reason, request.CreatedAt)
	created, err := scanRerunRequest(row)
	if err != nil {
		// The partial unique index is the invariant: one open request per call.
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return models.AnalysisRerunRequest{}, models.ErrAnalysisRerunRequestPending
		}
		return models.AnalysisRerunRequest{}, err
	}

	return created, nil
}

func (r *Repository) GetRerunRequest(ctx context.Context, id uuid.UUID) (models.AnalysisRerunRequest, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+rerunRequestColumns+` FROM call_analysis_rerun_requests WHERE request_uuid=$1`, id)
	return scanRerunRequest(row)
}

func (r *Repository) ListRerunRequests(ctx context.Context, companyID uuid.UUID, status models.AnalysisRerunRequestStatus) ([]models.AnalysisRerunRequest, error) {
	// A request whose call went to the bin cannot be decided either way, because
	// deciding it needs the call. Leaving it in the queue would give the leader a
	// row with no working button.
	query := `SELECT r.request_uuid, r.call_uuid, r.company_uuid, r.department_uuid, r.requested_by_user_uuid,
		r.reason, r.status, r.decided_by_user_uuid, r.decided_at, r.comment, r.created_at, r.updated_at
		FROM call_analysis_rerun_requests r
		WHERE r.company_uuid=$1
		  AND EXISTS (SELECT 1 FROM calls c WHERE c.call_uuid=r.call_uuid AND c.deleted_at IS NULL)`
	args := []any{companyID}
	if status != "" {
		args = append(args, string(status))
		query += fmt.Sprintf(" AND r.status=$%d", len(args))
	}
	query += " ORDER BY r.created_at DESC, r.request_uuid DESC"

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list analysis rerun requests: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items := []models.AnalysisRerunRequest{}
	for rows.Next() {
		item, err := scanRerunRequest(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}

	return items, rows.Err()
}

func (r *Repository) DecideRerunRequest(ctx context.Context, id uuid.UUID, decidedBy uuid.UUID, approve bool, comment string, now time.Time) (models.AnalysisRerunRequest, error) {
	status := models.AnalysisRerunRequestStatusRejected
	if approve {
		status = models.AnalysisRerunRequestStatusApproved
	}

	query := `
	UPDATE call_analysis_rerun_requests
	SET status=$2, decided_by_user_uuid=$3, decided_at=$4, comment=NULLIF($5,''), updated_at=$4
	WHERE request_uuid=$1 AND status='pending'
	RETURNING ` + rerunRequestColumns

	row := r.db.QueryRowContext(ctx, query, id, string(status), decidedBy, now, strings.TrimSpace(comment))
	return scanRerunRequest(row)
}

func scanRerunRequest(row interface{ Scan(dest ...any) error }) (models.AnalysisRerunRequest, error) {
	var request models.AnalysisRerunRequest
	var reason, comment sql.NullString
	var decidedAt sql.NullTime
	if err := row.Scan(
		&request.ID,
		&request.CallUUID,
		&request.CompanyUUID,
		&request.DepartmentUUID,
		&request.RequestedByUserUUID,
		&reason,
		&request.Status,
		&request.DecidedByUserUUID,
		&decidedAt,
		&comment,
		&request.CreatedAt,
		&request.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.AnalysisRerunRequest{}, models.ErrAnalysisRerunRequestNotFound
		}
		return models.AnalysisRerunRequest{}, err
	}
	if reason.Valid {
		request.Reason = &reason.String
	}
	if comment.Valid {
		request.Comment = &comment.String
	}
	if decidedAt.Valid {
		request.DecidedAt = &decidedAt.Time
	}

	return request, nil
}
