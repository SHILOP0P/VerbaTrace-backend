package analysis_context

import (
	"context"
	"strings"
	"unicode/utf8"

	"calllens/monolit/internal/models"

	"github.com/google/uuid"
)

const maxPersonalizationLength = 6000

type store interface {
	Get(context.Context, models.AnalysisPersonalizationScope, uuid.UUID) (models.AnalysisPersonalization, error)
	Save(context.Context, models.SaveAnalysisPersonalizationInput) (models.AnalysisPersonalization, error)
}

type companyReader interface {
	GetCompanyMember(context.Context, uuid.UUID, uuid.UUID) (models.CompanyMember, error)
}

type departmentReader interface {
	GetDepartmentMember(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (models.DepartmentMember, error)
	ListVisibleCompanyDepartments(context.Context, uuid.UUID, uuid.UUID) ([]models.Department, error)
}

type Service struct {
	store       store
	companies   companyReader
	departments departmentReader
}

func NewService(store store, companies companyReader, departments departmentReader) *Service {
	return &Service{store: store, companies: companies, departments: departments}
}

func (s *Service) Get(ctx context.Context, userID uuid.UUID, scope models.AnalysisPersonalizationScope, ownerID uuid.UUID, companyID uuid.UUID) (models.AnalysisPersonalization, error) {
	if err := s.authorize(ctx, userID, scope, ownerID, companyID, false); err != nil {
		return models.AnalysisPersonalization{}, err
	}
	return s.store.Get(ctx, scope, ownerID)
}

func (s *Service) Save(ctx context.Context, userID uuid.UUID, input models.SaveAnalysisPersonalizationInput, companyID uuid.UUID) (models.AnalysisPersonalization, error) {
	input.Content = strings.TrimSpace(input.Content)
	if utf8.RuneCountInString(input.Content) > maxPersonalizationLength {
		return models.AnalysisPersonalization{}, models.ErrInvalidAnalysisInput
	}
	if err := s.authorize(ctx, userID, input.Scope, input.OwnerUUID, companyID, true); err != nil {
		return models.AnalysisPersonalization{}, err
	}
	return s.store.Save(ctx, input)
}

func (s *Service) authorize(ctx context.Context, userID uuid.UUID, scope models.AnalysisPersonalizationScope, ownerID uuid.UUID, companyID uuid.UUID, write bool) error {
	switch scope {
	case models.AnalysisPersonalizationScopePersonal:
		if ownerID != userID {
			return models.ErrForbidden
		}
		return nil
	case models.AnalysisPersonalizationScopeCompany:
		member, err := s.companies.GetCompanyMember(ctx, ownerID, userID)
		if err != nil {
			return err
		}
		if write && member.Role != models.CompanyMemberRoleManager {
			return models.ErrForbidden
		}
		return nil
	case models.AnalysisPersonalizationScopeDepartment:
		if companyID == uuid.Nil {
			return models.ErrInvalidAnalysisInput
		}
		if !write {
			_, err := s.departments.GetDepartmentMember(ctx, companyID, ownerID, userID)
			if err == nil {
				return nil
			}
			member, companyErr := s.companies.GetCompanyMember(ctx, companyID, userID)
			if companyErr == nil && member.Status == models.MembershipStatusActive {
				return nil
			}
			return err
		}
		member, err := s.companies.GetCompanyMember(ctx, companyID, userID)
		if err == nil && member.Role == models.CompanyMemberRoleManager {
			return nil
		}
		departmentMember, departmentErr := s.departments.GetDepartmentMember(ctx, companyID, ownerID, userID)
		if departmentErr != nil || departmentMember.Role != models.DepartmentMemberRoleLeader {
			return models.ErrForbidden
		}
		return nil
	default:
		return models.ErrInvalidAnalysisInput
	}
}
