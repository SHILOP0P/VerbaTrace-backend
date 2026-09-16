package integration

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

// resolveMappedUploader answers who made the imported call. A portal user has
// to be mapped to a VerbaTrace account that still works in the company: an
// unmapped call would otherwise land on the administrator who connected the
// portal and be invisible to the person it belongs to.
func resolveMappedUploader(
	ctx context.Context,
	tx *sql.Tx,
	connectionID uuid.UUID,
	companyID uuid.NullUUID,
	participants []map[string]string,
) (uuid.NullUUID, uuid.NullUUID, error) {
	if !companyID.Valid {
		// A personal connection belongs to one person, so there is nobody to map.
		return uuid.NullUUID{}, uuid.NullUUID{}, nil
	}

	externalID := externalUserIDFrom(participants)
	if externalID == "" {
		return uuid.NullUUID{}, uuid.NullUUID{}, models.ErrExternalUserNotMapped
	}

	var uploader uuid.UUID
	var department uuid.NullUUID
	err := tx.QueryRowContext(ctx, `
		SELECT m.internal_user_uuid, m.department_uuid
		FROM integration_external_user_mappings m
		JOIN company_members cm ON cm.user_uuid = m.internal_user_uuid
		WHERE m.connection_uuid = $1
		  AND m.external_user_id = $2
		  AND m.status = 'mapped'
		  AND cm.company_uuid = $3
		  AND cm.status = 'active'`, connectionID, externalID, companyID.UUID).Scan(&uploader, &department)
	if errors.Is(err, sql.ErrNoRows) {
		return uuid.NullUUID{}, uuid.NullUUID{}, models.ErrExternalUserNotMapped
	}
	if err != nil {
		return uuid.NullUUID{}, uuid.NullUUID{}, err
	}

	return uuid.NullUUID{UUID: uploader, Valid: true}, department, nil
}

// externalUserIDFrom picks the portal user the call is attributed to. The
// employee is the one whose work is being analysed.
func externalUserIDFrom(participants []map[string]string) string {
	fallback := ""
	for _, participant := range participants {
		id := strings.TrimSpace(participant["external_user_id"])
		if id == "" {
			continue
		}
		if participant["role"] == "employee" {
			return id
		}
		if fallback == "" {
			fallback = id
		}
	}

	return fallback
}
