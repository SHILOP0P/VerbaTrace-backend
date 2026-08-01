package search

import (
	"context"
	"fmt"
	"strings"

	"verbatrace/monolit/internal/models"
)

const defaultSearchLimit = 10

func (r *Repository) Search(ctx context.Context, input models.SearchInput) (models.SearchResult, error) {
	if input.Limit <= 0 {
		input.Limit = defaultSearchLimit
	}

	pattern := "%" + strings.ToLower(strings.TrimSpace(input.Query)) + "%"
	result := models.SearchResult{}
	types := selectedTypes(input.Types)
	var err error

	if types[models.SearchTypeCalls] {
		result.Calls, err = r.searchCalls(ctx, input, pattern)
		if err != nil {
			return models.SearchResult{}, err
		}
	}
	if types[models.SearchTypeCompanies] {
		result.Companies, err = r.searchCompanies(ctx, input, pattern)
		if err != nil {
			return models.SearchResult{}, err
		}
	}
	if types[models.SearchTypeReports] {
		result.Reports, err = r.searchReports(ctx, input, pattern)
		if err != nil {
			return models.SearchResult{}, err
		}
	}
	if types[models.SearchTypeInstructions] {
		result.Instructions, err = r.searchInstructions(ctx, input, pattern)
		if err != nil {
			return models.SearchResult{}, err
		}
	}

	return result, nil
}

func selectedTypes(types []models.SearchType) map[models.SearchType]bool {
	selected := map[models.SearchType]bool{}
	if len(types) == 0 {
		selected[models.SearchTypeCalls] = true
		selected[models.SearchTypeCompanies] = true
		selected[models.SearchTypeReports] = true
		selected[models.SearchTypeInstructions] = true
		return selected
	}
	for _, item := range types {
		selected[item] = true
	}
	return selected
}

func (r *Repository) searchCalls(ctx context.Context, input models.SearchInput, pattern string) ([]models.SearchCallResult, error) {
	query := fmt.Sprintf(`
	SELECT c.call_uuid, c.title, c.status, c.created_at
	FROM calls c
	WHERE %s
	  AND (LOWER(c.title) LIKE $2 OR LOWER(c.original_filename) LIKE $2)
	ORDER BY c.created_at DESC
	LIMIT $3
	`, visibleCallCondition("c", "$1"))

	rows, err := r.db.QueryContext(ctx, query, input.UserUUID, pattern, input.Limit)
	if err != nil {
		return nil, fmt.Errorf("search calls: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items := make([]models.SearchCallResult, 0)
	for rows.Next() {
		var item models.SearchCallResult
		if err := rows.Scan(&item.ID, &item.Title, &item.Status, &item.CreatedAt); err != nil {
			return nil, fmt.Errorf("search calls: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("search calls: %w", err)
	}
	return items, nil
}

func (r *Repository) searchCompanies(ctx context.Context, input models.SearchInput, pattern string) ([]models.SearchCompanyResult, error) {
	query := `
	SELECT c.company_uuid, c.name
	FROM companies c
	JOIN company_members cm ON cm.company_uuid = c.company_uuid
	WHERE cm.user_uuid = $1
	  AND cm.status = 'active'
	  AND c.deleted_at IS NULL
	  AND LOWER(c.name) LIKE $2
	ORDER BY c.created_at DESC
	LIMIT $3
	`

	rows, err := r.db.QueryContext(ctx, query, input.UserUUID, pattern, input.Limit)
	if err != nil {
		return nil, fmt.Errorf("search companies: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items := make([]models.SearchCompanyResult, 0)
	for rows.Next() {
		var item models.SearchCompanyResult
		if err := rows.Scan(&item.ID, &item.Name); err != nil {
			return nil, fmt.Errorf("search companies: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("search companies: %w", err)
	}
	return items, nil
}

func (r *Repository) searchReports(ctx context.Context, input models.SearchInput, pattern string) ([]models.SearchReportResult, error) {
	query := fmt.Sprintf(`
	SELECT r.report_uuid, r.call_uuid, r.file_name, r.status
	FROM call_report_exports r
	JOIN calls c ON c.call_uuid = r.call_uuid
	WHERE %s
	  AND LOWER(r.file_name) LIKE $2
	  AND r.expires_at > now()
	ORDER BY r.created_at DESC
	LIMIT $3
	`, visibleCallCondition("c", "$1"))

	rows, err := r.db.QueryContext(ctx, query, input.UserUUID, pattern, input.Limit)
	if err != nil {
		return nil, fmt.Errorf("search reports: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items := make([]models.SearchReportResult, 0)
	for rows.Next() {
		var item models.SearchReportResult
		if err := rows.Scan(&item.ID, &item.CallUUID, &item.FileName, &item.Status); err != nil {
			return nil, fmt.Errorf("search reports: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("search reports: %w", err)
	}
	return items, nil
}

func (r *Repository) searchInstructions(ctx context.Context, input models.SearchInput, pattern string) ([]models.SearchInstructionResult, error) {
	query := `
	SELECT ai.instruction_uuid, ai.title, ai.scope
	FROM analysis_instructions ai
	WHERE ai.is_active = true
	  AND (LOWER(ai.title) LIKE $2 OR LOWER(ai.original_filename) LIKE $2)
	  AND (
	      (ai.scope = 'personal' AND ai.user_uuid = $1)
	      OR (
	          ai.scope = 'company'
	          AND ai.department_uuid IS NULL
	          AND ai.company_uuid IS NOT NULL
	          AND (
	              EXISTS (
	                  SELECT 1 FROM company_members cm
	                  WHERE cm.company_uuid = ai.company_uuid
	                    AND cm.user_uuid = $1
	                    AND cm.role = 'company_manager'
	                    AND cm.status = 'active'
	              )
	              OR EXISTS (
	                  SELECT 1 FROM department_members dm
	                  JOIN departments d ON d.department_uuid = dm.department_uuid
	                  WHERE d.company_uuid = ai.company_uuid
	                    AND d.deleted_at IS NULL
	                    AND dm.user_uuid = $1
	                    AND dm.status = 'active'
	              )
	          )
	      )
	      OR (
	          ai.scope = 'department'
	          AND ai.department_uuid IS NOT NULL
	          AND (
	              EXISTS (
	                  SELECT 1 FROM company_members cm
	                  WHERE cm.company_uuid = ai.company_uuid
	                    AND cm.user_uuid = $1
	                    AND cm.role = 'company_manager'
	                    AND cm.status = 'active'
	              )
	              OR EXISTS (
	                  SELECT 1 FROM department_members dm
	                  WHERE dm.department_uuid = ai.department_uuid
	                    AND dm.user_uuid = $1
	                    AND dm.status = 'active'
	              )
	          )
	      )
	  )
	ORDER BY ai.created_at DESC
	LIMIT $3
	`

	rows, err := r.db.QueryContext(ctx, query, input.UserUUID, pattern, input.Limit)
	if err != nil {
		return nil, fmt.Errorf("search instructions: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items := make([]models.SearchInstructionResult, 0)
	for rows.Next() {
		var item models.SearchInstructionResult
		if err := rows.Scan(&item.ID, &item.Title, &item.Scope); err != nil {
			return nil, fmt.Errorf("search instructions: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("search instructions: %w", err)
	}
	return items, nil
}

func visibleCallCondition(callAlias string, userParam string) string {
	return fmt.Sprintf(`
	(
	    %s.uploaded_by_user_uuid = %s
	    OR (
	        %s.company_uuid IS NOT NULL
	        AND EXISTS (
	            SELECT 1
	            FROM company_members cm
	            WHERE cm.company_uuid = %s.company_uuid
	              AND cm.user_uuid = %s
	              AND cm.role = 'company_manager'
	              AND cm.status = 'active'
	        )
	    )
	    OR (
	        %s.department_uuid IS NOT NULL
	        AND EXISTS (
	            SELECT 1
	            FROM department_members dm
	            WHERE dm.department_uuid = %s.department_uuid
	              AND dm.user_uuid = %s
	              AND dm.role = 'department_leader'
	              AND dm.status = 'active'
	        )
	    )
	)`, callAlias, userParam, callAlias, callAlias, userParam, callAlias, callAlias, userParam)
}
