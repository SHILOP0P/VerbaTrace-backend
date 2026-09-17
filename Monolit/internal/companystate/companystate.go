// Package companystate answers one question that every mutation has to ask: may
// this company still be changed?
//
// A frozen company stays fully readable — its calls, transcripts, analyses,
// reports and analytics all work — but nothing inside it may be changed any
// more. That rule used to hold only by accident: it was enforced wherever the
// code happened to ask for a subscription, and every operation that did not ask
// kept working. Actions, quality reviews, transcript edits, folders and
// department membership all went on as if nothing had happened.
//
// The guard lives in its own package because the services that need it are split
// between those built on repositories and those written against *sql.DB, and
// neither should have to depend on the other.
package companystate

import (
	"context"
	"database/sql"
	"errors"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

// Querier is the part of *sql.DB and *sql.Tx this package uses.
type Querier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// EnsureActive refuses when the company is frozen or on its way out. A call
// without a company is personal and has no company state to check.
func EnsureActive(ctx context.Context, q Querier, companyID uuid.UUID) error {
	if q == nil || companyID == uuid.Nil {
		return nil
	}

	var state string
	err := q.QueryRowContext(ctx, `SELECT lifecycle_state FROM companies WHERE company_uuid=$1 AND deleted_at IS NULL`, companyID).Scan(&state)
	if errors.Is(err, sql.ErrNoRows) {
		return models.ErrCompanyNotFound
	}
	if err != nil {
		return err
	}
	if models.CompanyLifecycleState(state) != models.CompanyLifecycleActive {
		return models.ErrCompanyFrozen
	}

	return nil
}

// EnsureActiveNullable is the same check for an optional company.
func EnsureActiveNullable(ctx context.Context, q Querier, companyID uuid.NullUUID) error {
	if !companyID.Valid {
		return nil
	}

	return EnsureActive(ctx, q, companyID.UUID)
}
