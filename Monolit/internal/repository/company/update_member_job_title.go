package company

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"calllens/monolit/internal/models"

	"github.com/google/uuid"
)

func (r *Repository) UpdateCompanyMemberJobTitle(
	ctx context.Context,
	companyID uuid.UUID,
	userID uuid.UUID,
	jobTitle *string,
) (models.CompanyMember, error) {
	row := r.db.QueryRowContext(ctx, `
		WITH updated AS (
			UPDATE company_members
			SET job_title = $3
			WHERE company_uuid = $1 AND user_uuid = $2
			RETURNING company_uuid, user_uuid, job_title, role, status, created_at
		)
		SELECT m.company_uuid, m.user_uuid, p.username, p.full_name, p.full_surname,
		       m.job_title, m.role, m.status, m.created_at
		FROM updated m
		JOIN user_profiles p ON p.user_uuid = m.user_uuid`,
		companyID, userID, jobTitle)

	var member models.CompanyMember
	if err := row.Scan(
		&member.CompanyUUID,
		&member.UserUUID,
		&member.Username,
		&member.FullName,
		&member.FullSurname,
		&member.JobTitle,
		&member.Role,
		&member.Status,
		&member.CreatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.CompanyMember{}, models.ErrCompanyNotFound
		}
		return models.CompanyMember{}, fmt.Errorf("update company member job title: %w", err)
	}

	return member, nil
}
