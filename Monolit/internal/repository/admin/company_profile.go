package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"verbatrace/monolit/internal/models"
	"verbatrace/monolit/internal/username"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

// UpdateAdminCompanyTag changes a customer's company tag on their behalf.
//
// It lives here rather than beside the self-service tag change because the two
// are different operations: this one acts on somebody else's data, so it demands
// a reason and writes an audit record. Without those the change was invisible,
// which is exactly the objection the owner raised — the defence against misuse
// is the reason and the journal, not a ban on changing anything.
func (r *Repository) UpdateAdminCompanyTag(ctx context.Context, in models.UpdateAdminCompanyTagInput) (models.AdminCompany, error) {
	tag := strings.TrimSpace(in.Tag)
	if in.CompanyUUID == uuid.Nil || tag == "" {
		return models.AdminCompany{}, models.ErrInvalidAdminInput
	}
	if strings.TrimSpace(in.Metadata.Reason) == "" {
		return models.AdminCompany{}, models.ErrAdminReasonRequired
	}

	normalized := "@" + in.CompanyUUID.String()
	if !strings.EqualFold(tag, normalized) {
		value, ok := username.Normalize(tag)
		if !ok {
			return models.AdminCompany{}, models.ErrInvalidAdminInput
		}
		normalized = value
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return models.AdminCompany{}, err
	}
	defer func() { _ = tx.Rollback() }()

	actor, err := getAdminUserForUpdate(ctx, tx, in.ActorUserUUID)
	if err != nil {
		return models.AdminCompany{}, err
	}
	if actor.Role != models.UserRoleAdmin && actor.Role != models.UserRoleSuperAdmin {
		return models.AdminCompany{}, models.ErrForbidden
	}

	before, err := getAdminCompany(ctx, tx, in.CompanyUUID)
	if err != nil {
		return models.AdminCompany{}, err
	}

	var updated models.AdminCompany
	err = tx.QueryRowContext(ctx, `
		UPDATE companies SET tag=$2, updated_at=now()
		WHERE company_uuid=$1 AND deleted_at IS NULL
		RETURNING company_uuid, name, tag, manager_user_uuid, created_at
	`, in.CompanyUUID, normalized).Scan(&updated.ID, &updated.Name, &updated.Tag, &updated.ManagerUserUUID, &updated.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return models.AdminCompany{}, models.ErrCompanyNotFound
	}
	if err != nil {
		var pg *pgconn.PgError
		if errors.As(err, &pg) && pg.Code == "23505" {
			return models.AdminCompany{}, models.ErrCompanyTagAlreadyExists
		}
		return models.AdminCompany{}, fmt.Errorf("update company tag as admin: %w", err)
	}

	beforeData, _ := json.Marshal(map[string]string{"tag": before.Tag})
	afterData, _ := json.Marshal(map[string]string{"tag": updated.Tag})
	audit := models.AdminAuditLog{
		ID:            mustUUIDv7(),
		ActorUserUUID: actor.ID,
		ActorRole:     actor.Role,
		Action:        "company.tag_changed",
		TargetType:    "company",
		TargetUUID:    uuid.NullUUID{UUID: in.CompanyUUID, Valid: true},
		BeforeData:    beforeData,
		AfterData:     afterData,
		Reason:        &in.Metadata.Reason,
		RequestID:     in.Metadata.RequestID,
		IPAddress:     in.Metadata.IPAddress,
		UserAgent:     in.Metadata.UserAgent,
		CreatedAt:     time.Now().UTC(),
	}
	if err = insertAudit(ctx, tx, audit); err != nil {
		return models.AdminCompany{}, err
	}

	if err = tx.Commit(); err != nil {
		return models.AdminCompany{}, err
	}

	return updated, nil
}
