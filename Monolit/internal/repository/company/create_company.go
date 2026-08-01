package company

import (
	"context"
	"fmt"

	model "verbatrace/monolit/internal/models"
	"verbatrace/monolit/internal/repository/converter"
	repoModel "verbatrace/monolit/internal/repository/models"
	"verbatrace/monolit/internal/repository/scaner"
)

func (r *Repository) CreateCompany(ctx context.Context, company model.Company, member model.CompanyMember) (model.Company, error) {
	if company.Tag == "" {
		company.Tag = "@" + company.ID.String()
	}
	repoCompany, err := converter.ModelCompanyToRepoCompany(company)
	if err != nil {
		return model.Company{}, fmt.Errorf("convert company: %w", err)
	}

	repoMember, err := converter.ModelCompanyMemberToRepoCompanyMember(member)
	if err != nil {
		return model.Company{}, fmt.Errorf("convert company member: %w", err)
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return model.Company{}, fmt.Errorf("begin create company transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	createCompanyQuery := `
	INSERT INTO companies (
		company_uuid,
		name,
		tag,
		manager_user_uuid,
		member_limit,
		created_at
	)
	VALUES ($1, $2, $3, $4, $5, $6)
	RETURNING company_uuid,
	          name,
	          tag,
	          manager_user_uuid,
	          member_limit,
	          created_at,
	          deleted_at
	`

	row := tx.QueryRowContext(
		ctx,
		createCompanyQuery,
		repoCompany.ID,
		repoCompany.Name,
		repoCompany.Tag,
		repoCompany.ManagerUserUUID,
		repoCompany.MemberLimit,
		repoCompany.CreatedAt,
	)

	var createdCompany repoModel.Company
	createdCompany, err = scaner.ScanCompany(row)
	if err != nil {
		return model.Company{}, fmt.Errorf("create company: %w", err)
	}

	createMemberQuery := `
	INSERT INTO company_members (
		company_uuid,
		user_uuid,
		role,
		status,
		created_at
	)
	VALUES ($1, $2, $3, $4, $5)
	`

	if _, err := tx.ExecContext(
		ctx,
		createMemberQuery,
		repoMember.CompanyUUID,
		repoMember.UserUUID,
		repoMember.Role,
		repoMember.Status,
		repoMember.CreatedAt,
	); err != nil {
		return model.Company{}, fmt.Errorf("create company member: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return model.Company{}, fmt.Errorf("commit create company transaction: %w", err)
	}

	return converter.RepoCompanyToModel(createdCompany)
}
