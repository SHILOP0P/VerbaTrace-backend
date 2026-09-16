package department

import (
	"context"
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// AddDepartmentMember puts an existing company member into a department. It is
// the same operation as a move, so nobody ends up in two departments, and only
// the owner and the deputy may do it directly. Leaders ask for a transfer.
func (s *Service) AddDepartmentMember(ctx context.Context, input models.AddDepartmentMemberInput) (models.DepartmentMember, error) {
	if input.CompanyUUID == uuid.Nil || input.DepartmentUUID == uuid.Nil || input.RequestUser == uuid.Nil || input.UserUUID == uuid.Nil {
		return models.DepartmentMember{}, models.ErrInvalidDepartmentInput
	}

	if input.Role != models.DepartmentMemberRoleLeader && input.Role != models.DepartmentMemberRoleEmployee {
		return models.DepartmentMember{}, models.ErrInvalidDepartmentInput
	}

	if err := s.requireCompanyManager(ctx, input.CompanyUUID, input.RequestUser); err != nil {
		return models.DepartmentMember{}, err
	}

	if err := s.requireActiveCompanySubscription(ctx, input.CompanyUUID); err != nil {
		return models.DepartmentMember{}, err
	}

	if _, err := s.companyRepository.GetCompanyMember(ctx, input.CompanyUUID, input.UserUUID); err != nil {
		return models.DepartmentMember{}, err
	}

	member, err := s.departmentRepository.MoveMemberToDepartment(ctx, models.MoveDepartmentMemberInput{
		CompanyUUID:      input.CompanyUUID,
		ToDepartmentUUID: input.DepartmentUUID,
		UserUUID:         input.UserUUID,
		RequestUser:      input.RequestUser,
		Role:             input.Role,
		Now:              time.Now().UTC(),
	})
	if err != nil {
		s.log.Error(ctx, "failed to add department member", zap.String("company_id", input.CompanyUUID.String()), zap.String("department_id", input.DepartmentUUID.String()), zap.String("request_user_id", input.RequestUser.String()), zap.String("user_id", input.UserUUID.String()), zap.Error(err))
		return models.DepartmentMember{}, err
	}

	s.notify(ctx, input.UserUUID, models.NotificationTypeDepartmentMemberMoved, "Вас добавили в отдел", "Доступ к звонкам обновлён", input.DepartmentUUID)
	s.log.Info(ctx, "department member added", zap.String("company_id", input.CompanyUUID.String()), zap.String("department_id", input.DepartmentUUID.String()), zap.String("request_user_id", input.RequestUser.String()), zap.String("user_id", input.UserUUID.String()), zap.String("role", string(member.Role)))

	return member, nil
}
