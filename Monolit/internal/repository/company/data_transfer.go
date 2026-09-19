package company

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	model "verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

// TransferCompanyData moves calls and instruction folders from one of the
// owner's companies into another.
//
// It exists because the lifecycle already deletes companies: a freeze runs out,
// a soft deletion runs out, and the data goes. Telling an owner to move their
// data first only works if there is something to move it with, and there was
// not. This is that operation, and it is deliberately explicit rather than an
// automatic merge at freeze time — two companies have different departments,
// people and privacy policies, and a silent merge would hand the wrong people
// access to recordings.
//
// Everything travels in one transaction. The author of a call is kept as it is:
// they uploaded it, and that stays true whether or not they work in the company
// it ends up in. What they lose is access, which follows from membership.
func (r *Repository) TransferCompanyData(ctx context.Context, input model.TransferCompanyDataInput) (model.TransferCompanyDataResult, error) {
	if input.OwnerUserUUID == uuid.Nil || input.SourceCompanyUUID == uuid.Nil || input.TargetCompanyUUID == uuid.Nil {
		return model.TransferCompanyDataResult{}, model.ErrInvalidCompanyInput
	}
	if input.SourceCompanyUUID == input.TargetCompanyUUID {
		return model.TransferCompanyDataResult{}, model.ErrInvalidCompanyInput
	}
	if !input.IncludeCalls && !input.IncludeFolders {
		return model.TransferCompanyDataResult{}, model.ErrInvalidCompanyInput
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return model.TransferCompanyDataResult{}, fmt.Errorf("begin transfer company data: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err = ensureTransferEndpoints(ctx, tx, input); err != nil {
		return model.TransferCompanyDataResult{}, err
	}

	result := model.TransferCompanyDataResult{
		SourceCompanyUUID: input.SourceCompanyUUID,
		TargetCompanyUUID: input.TargetCompanyUUID,
	}

	if input.IncludeCalls {
		moved, err := transferCalls(ctx, tx, input)
		if err != nil {
			return model.TransferCompanyDataResult{}, err
		}
		result.Calls = moved
	}

	if input.IncludeFolders {
		moved, err := transferFolders(ctx, tx, input)
		if err != nil {
			return model.TransferCompanyDataResult{}, err
		}
		result.Folders = moved
	}

	transferID, err := recordTransfer(ctx, tx, input, result, time.Now().UTC())
	if err != nil {
		return model.TransferCompanyDataResult{}, err
	}
	result.ID = transferID

	if err = tx.Commit(); err != nil {
		return model.TransferCompanyDataResult{}, fmt.Errorf("commit transfer company data: %w", err)
	}

	return result, nil
}

// ensureTransferEndpoints refuses anything but a move between two companies the
// same person owns, into one that still works.
func ensureTransferEndpoints(ctx context.Context, tx *sql.Tx, input model.TransferCompanyDataInput) error {
	var sourceOwner uuid.UUID
	if err := tx.QueryRowContext(ctx, `SELECT manager_user_uuid FROM companies WHERE company_uuid=$1 AND deleted_at IS NULL FOR UPDATE`, input.SourceCompanyUUID).Scan(&sourceOwner); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.ErrCompanyNotFound
		}
		return fmt.Errorf("lock source company: %w", err)
	}

	var targetOwner uuid.UUID
	var targetState string
	if err := tx.QueryRowContext(ctx, `SELECT manager_user_uuid, lifecycle_state FROM companies WHERE company_uuid=$1 AND deleted_at IS NULL FOR UPDATE`, input.TargetCompanyUUID).Scan(&targetOwner, &targetState); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.ErrCompanyNotFound
		}
		return fmt.Errorf("lock target company: %w", err)
	}

	if sourceOwner != input.OwnerUserUUID || targetOwner != input.OwnerUserUUID {
		return model.ErrForbidden
	}
	if model.CompanyLifecycleState(targetState) != model.CompanyLifecycleActive {
		// Moving data into a company that does not work would only hide it.
		return model.ErrCompanyFrozen
	}

	return nil
}

// transferCalls re-points the calls and clears the department: the one they came
// from does not exist in the receiving company, and leaving a stale reference
// would let the wrong leader see them.
//
// The moved ids are carried out of the UPDATE, so everything that follows acts
// on exactly those calls rather than on whatever happens to sit in the receiving
// company.
func transferCalls(ctx context.Context, tx *sql.Tx, input model.TransferCompanyDataInput) (int64, error) {
	filter, args := callTransferFilter(input)

	// Clearing the department also widens the scope: the database ties the two
	// together, and a call scoped to a department it no longer has would be
	// rejected outright.
	var movedList string
	err := tx.QueryRowContext(ctx, `
		WITH moved AS (
			UPDATE calls
			SET company_uuid = $2,
			    department_uuid = NULL,
			    visibility_scope = CASE WHEN visibility_scope = 'department' THEN 'company' ELSE visibility_scope END
			WHERE company_uuid = $1 AND `+filter+`
			RETURNING call_uuid
		)
		SELECT COALESCE(array_to_string(array_agg(call_uuid), ','), '') FROM moved`, args...).Scan(&movedList)
	if err != nil {
		return 0, fmt.Errorf("move calls: %w", err)
	}

	moved, err := splitUUIDs(movedList)
	if err != nil {
		return 0, err
	}
	if len(moved) == 0 {
		return 0, nil
	}
	movedIDs := joinUUIDs(moved)

	// Who a call counts for was found among the employees of the old company.
	// Dropping it lets the analytics worker resolve it again among the new
	// company's employees and re-project the facts. The audit keeps what was
	// dropped and why; no actor, because nobody chose these people by hand.
	if _, err = tx.ExecContext(ctx, `
		INSERT INTO call_subject_events (event_uuid, call_uuid, actor_user_uuid, before, after, cause)
		SELECT gen_random_uuid(), s.call_uuid, NULL,
		       jsonb_agg(jsonb_build_object('user_uuid', s.user_uuid, 'source', s.source, 'is_primary', s.is_primary, 'grants_access', s.grants_access) ORDER BY s.is_primary DESC, s.user_uuid),
		       '[]'::jsonb, $2
		FROM call_subjects s
		WHERE s.call_uuid = ANY(string_to_array($1, ',')::uuid[])
		GROUP BY s.call_uuid`, movedIDs, model.CallSubjectCauseCompanyTransfer); err != nil {
		return 0, fmt.Errorf("record dropped call subjects: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM call_subjects WHERE call_uuid = ANY(string_to_array($1, ',')::uuid[])`, movedIDs); err != nil {
		return 0, fmt.Errorf("reset moved call subjects: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM call_subject_states WHERE call_uuid = ANY(string_to_array($1, ',')::uuid[])`, movedIDs); err != nil {
		return 0, fmt.Errorf("reset moved call subject states: %w", err)
	}

	// Folder links from the source company stop making sense the moment the call
	// leaves it.
	if _, err = tx.ExecContext(ctx, `
		DELETE FROM call_folder_assignments a
		USING call_folders f
		WHERE f.folder_uuid = a.folder_uuid
		  AND f.company_uuid = $1
		  AND a.call_uuid = ANY(string_to_array($2, ',')::uuid[])
	`, input.SourceCompanyUUID, movedIDs); err != nil {
		return 0, fmt.Errorf("drop stale folder links: %w", err)
	}

	// Everything a call owns and that records a company of its own follows it,
	// otherwise a quality review or an action would answer to one company while
	// its call answers to another.
	for _, table := range []string{"call_actions", "call_quality_reviews", "call_action_dispositions", "call_analysis_rerun_requests"} {
		if _, err = tx.ExecContext(ctx, fmt.Sprintf(`
			UPDATE %s SET company_uuid = $1
			WHERE call_uuid = ANY(string_to_array($2, ',')::uuid[])
		`, table), input.TargetCompanyUUID, movedIDs); err != nil {
			return 0, fmt.Errorf("move %s: %w", table, err)
		}
	}

	// A department the receiving company does not have would point nowhere.
	if _, err = tx.ExecContext(ctx, `
		UPDATE call_actions SET source_department_uuid = NULL, target_department_uuid = NULL
		WHERE call_uuid = ANY(string_to_array($1, ',')::uuid[])
	`, movedIDs); err != nil {
		return 0, fmt.Errorf("clear action departments: %w", err)
	}

	return int64(len(moved)), nil
}

func callTransferFilter(input model.TransferCompanyDataInput) (string, []any) {
	args := []any{input.SourceCompanyUUID, input.TargetCompanyUUID}
	if len(input.CallUUIDs) == 0 {
		return "deleted_at IS NULL", args
	}

	args = append(args, joinUUIDs(input.CallUUIDs))

	return "deleted_at IS NULL AND call_uuid = ANY(string_to_array($3, ',')::uuid[])", args
}

// transferFolders moves the instruction folders themselves. Their department is
// cleared for the same reason a call's is.
func transferFolders(ctx context.Context, tx *sql.Tx, input model.TransferCompanyDataInput) (int64, error) {
	moved, err := tx.ExecContext(ctx, `
		UPDATE call_folders
		SET company_uuid = $2,
		    department_uuid = NULL,
		    scope = CASE WHEN scope = 'department' THEN 'company' ELSE scope END
		WHERE company_uuid = $1 AND deleted_at IS NULL
	`, input.SourceCompanyUUID, input.TargetCompanyUUID)
	if err != nil {
		return 0, fmt.Errorf("move instruction folders: %w", err)
	}
	count, _ := moved.RowsAffected()

	return count, nil
}

func recordTransfer(ctx context.Context, tx *sql.Tx, input model.TransferCompanyDataInput, result model.TransferCompanyDataResult, now time.Time) (uuid.UUID, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.Nil, fmt.Errorf("new company data transfer id: %w", err)
	}

	if _, err = tx.ExecContext(ctx, `
		INSERT INTO company_data_transfers (
			transfer_uuid, owner_user_uuid, source_company_uuid, target_company_uuid,
			calls_moved, folders_moved, selected_calls, reason, created_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, NULLIF($8, ''), $9)
	`, id, input.OwnerUserUUID, input.SourceCompanyUUID, input.TargetCompanyUUID,
		result.Calls, result.Folders, len(input.CallUUIDs), input.Reason, now); err != nil {
		return uuid.Nil, fmt.Errorf("record company data transfer: %w", err)
	}

	return id, nil
}

// ListCompanyDataTransfers shows the owner what they have already moved.
func (r *Repository) ListCompanyDataTransfers(ctx context.Context, ownerID uuid.UUID, limit int) ([]model.TransferCompanyDataResult, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	rows, err := r.db.QueryContext(ctx, `
		SELECT transfer_uuid, source_company_uuid, target_company_uuid, calls_moved, folders_moved, reason, created_at
		FROM company_data_transfers
		WHERE owner_user_uuid = $1
		ORDER BY created_at DESC
		LIMIT $2
	`, ownerID, limit)
	if err != nil {
		return nil, fmt.Errorf("list company data transfers: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items := []model.TransferCompanyDataResult{}
	for rows.Next() {
		var item model.TransferCompanyDataResult
		var source, target uuid.NullUUID
		var reason sql.NullString
		if err := rows.Scan(&item.ID, &source, &target, &item.Calls, &item.Folders, &reason, &item.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan company data transfer: %w", err)
		}
		item.SourceCompanyUUID = source.UUID
		item.TargetCompanyUUID = target.UUID
		if reason.Valid {
			item.Reason = reason.String
		}
		items = append(items, item)
	}

	return items, rows.Err()
}
