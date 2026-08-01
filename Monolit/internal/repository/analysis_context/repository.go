package analysis_context

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

type Repository struct{ db *sql.DB }

func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }

func (r *Repository) Get(ctx context.Context, scope models.AnalysisPersonalizationScope, ownerID uuid.UUID) (models.AnalysisPersonalization, error) {
	var item models.AnalysisPersonalization
	err := r.db.QueryRowContext(ctx, `
		SELECT scope, owner_uuid, content, updated_at
		FROM analysis_personalizations
		WHERE scope = $1 AND owner_uuid = $2
	`, scope, ownerID).Scan(&item.Scope, &item.OwnerUUID, &item.Content, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return models.AnalysisPersonalization{Scope: scope, OwnerUUID: ownerID, Content: ""}, nil
	}
	if err != nil {
		return models.AnalysisPersonalization{}, fmt.Errorf("get analysis personalization: %w", err)
	}
	return item, nil
}

func (r *Repository) Save(ctx context.Context, input models.SaveAnalysisPersonalizationInput) (models.AnalysisPersonalization, error) {
	var item models.AnalysisPersonalization
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO analysis_personalizations(scope, owner_uuid, content)
		VALUES ($1, $2, $3)
		ON CONFLICT(scope, owner_uuid)
		DO UPDATE SET content = EXCLUDED.content, updated_at = now()
		RETURNING scope, owner_uuid, content, updated_at
	`, input.Scope, input.OwnerUUID, input.Content).Scan(&item.Scope, &item.OwnerUUID, &item.Content, &item.UpdatedAt)
	if err != nil {
		return models.AnalysisPersonalization{}, fmt.Errorf("save analysis personalization: %w", err)
	}
	return item, nil
}

func (r *Repository) ContextForCall(ctx context.Context, call models.Call) ([]string, error) {
	owners := []struct {
		scope models.AnalysisPersonalizationScope
		id    uuid.UUID
	}{}
	switch call.VisibilityScope {
	case models.CallVisibilityScopePersonal:
		owners = append(owners, struct {
			scope models.AnalysisPersonalizationScope
			id    uuid.UUID
		}{models.AnalysisPersonalizationScopePersonal, call.UploadedByUserUUID.UUID})
	case models.CallVisibilityScopeCompany:
		owners = append(owners, struct {
			scope models.AnalysisPersonalizationScope
			id    uuid.UUID
		}{models.AnalysisPersonalizationScopeCompany, call.CompanyUUID.UUID})
	case models.CallVisibilityScopeDepartment:
		owners = append(owners,
			struct {
				scope models.AnalysisPersonalizationScope
				id    uuid.UUID
			}{models.AnalysisPersonalizationScopeCompany, call.CompanyUUID.UUID},
			struct {
				scope models.AnalysisPersonalizationScope
				id    uuid.UUID
			}{models.AnalysisPersonalizationScopeDepartment, call.DepartmentUUID.UUID},
		)
	}
	result := make([]string, 0, len(owners))
	for _, owner := range owners {
		item, err := r.Get(ctx, owner.scope, owner.id)
		if err != nil {
			return nil, err
		}
		if item.Content != "" {
			result = append(result, item.Content)
		}
	}
	return result, nil
}
