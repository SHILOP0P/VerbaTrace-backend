package department

import (
	"context"
	"strings"
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// departmentTransferTTL keeps an unanswered request from hanging around forever.
const departmentTransferTTL = 14 * 24 * time.Hour

// MoveMember moves somebody between departments directly. Only the owner and
// the deputy can do it, and the person's calls follow them.
func (s *Service) MoveMember(ctx context.Context, input models.MoveDepartmentMemberInput) (models.DepartmentMember, error) {
	if input.CompanyUUID == uuid.Nil || input.ToDepartmentUUID == uuid.Nil || input.UserUUID == uuid.Nil || input.RequestUser == uuid.Nil {
		return models.DepartmentMember{}, models.ErrInvalidDepartmentInput
	}
	if input.Role != "" && input.Role != models.DepartmentMemberRoleEmployee && input.Role != models.DepartmentMemberRoleLeader {
		return models.DepartmentMember{}, models.ErrInvalidDepartmentInput
	}

	if err := s.requireCompanyManager(ctx, input.CompanyUUID, input.RequestUser); err != nil {
		return models.DepartmentMember{}, err
	}
	if err := s.requireActiveCompanySubscription(ctx, input.CompanyUUID); err != nil {
		return models.DepartmentMember{}, err
	}

	input.Now = time.Now().UTC()
	member, err := s.departmentRepository.MoveMemberToDepartment(ctx, input)
	if err != nil {
		s.log.Error(ctx, "failed to move department member", zap.String("company_id", input.CompanyUUID.String()), zap.String("user_id", input.UserUUID.String()), zap.Error(err))
		return models.DepartmentMember{}, err
	}

	s.notify(ctx, input.UserUUID, models.NotificationTypeDepartmentMemberMoved, "Вас перевели в другой отдел", "Доступ к звонкам обновлён", input.ToDepartmentUUID)
	return member, nil
}

// RequestTransfer lets a department leader ask for a colleague from another
// department instead of taking them.
func (s *Service) RequestTransfer(ctx context.Context, input models.CreateDepartmentTransferInput) (models.DepartmentTransferRequest, error) {
	if input.CompanyUUID == uuid.Nil || input.ToDepartmentUUID == uuid.Nil || input.UserUUID == uuid.Nil || input.RequestUser == uuid.Nil {
		return models.DepartmentTransferRequest{}, models.ErrInvalidDepartmentInput
	}

	leader, err := s.departmentRepository.GetDepartmentMember(ctx, input.CompanyUUID, input.ToDepartmentUUID, input.RequestUser)
	if err != nil {
		return models.DepartmentTransferRequest{}, err
	}
	if leader.Role != models.DepartmentMemberRoleLeader {
		return models.DepartmentTransferRequest{}, models.ErrForbidden
	}

	if _, err := s.companyRepository.GetCompanyMember(ctx, input.CompanyUUID, input.UserUUID); err != nil {
		return models.DepartmentTransferRequest{}, err
	}

	current, err := s.departmentRepository.ListUserDepartments(ctx, input.CompanyUUID, input.UserUUID)
	if err != nil {
		return models.DepartmentTransferRequest{}, err
	}

	var from uuid.NullUUID
	for _, department := range current {
		if department.DepartmentUUID == input.ToDepartmentUUID {
			return models.DepartmentTransferRequest{}, models.ErrInvalidDepartmentInput
		}
		// A leader must not poach another department's leader.
		if department.Role == models.DepartmentMemberRoleLeader {
			return models.DepartmentTransferRequest{}, models.ErrForbidden
		}
		from = uuid.NullUUID{UUID: department.DepartmentUUID, Valid: true}
	}

	now := time.Now().UTC()
	request, err := s.departmentRepository.CreateDepartmentTransfer(ctx, models.DepartmentTransferRequest{
		ID:                  uuid.New(),
		CompanyUUID:         input.CompanyUUID,
		UserUUID:            input.UserUUID,
		FromDepartmentUUID:  from,
		ToDepartmentUUID:    input.ToDepartmentUUID,
		RequestedByUserUUID: input.RequestUser,
		Reason:              optionalText(input.Reason),
		CreatedAt:           now,
		ExpiresAt:           now.Add(departmentTransferTTL),
	})
	if err != nil {
		return models.DepartmentTransferRequest{}, err
	}

	if approver, err := s.approverForCompany(ctx, input.CompanyUUID); err == nil {
		s.notify(ctx, approver, models.NotificationTypeDepartmentTransferRequested, "Запрос на перевод сотрудника", "Лидер отдела просит перевести сотрудника", request.ID)
	}

	return request, nil
}

// DecideTransfer approves or rejects the request. Approval performs the move.
func (s *Service) DecideTransfer(ctx context.Context, input models.DecideDepartmentTransferInput) (models.DepartmentTransferRequest, error) {
	if input.RequestUUID == uuid.Nil || input.RequestUser == uuid.Nil {
		return models.DepartmentTransferRequest{}, models.ErrInvalidDepartmentInput
	}

	request, err := s.departmentRepository.GetDepartmentTransfer(ctx, input.RequestUUID)
	if err != nil {
		return models.DepartmentTransferRequest{}, err
	}
	if request.Status != models.DepartmentTransferStatusPending {
		return models.DepartmentTransferRequest{}, models.ErrDepartmentTransferNotFound
	}
	if err := s.requireCompanyManager(ctx, request.CompanyUUID, input.RequestUser); err != nil {
		return models.DepartmentTransferRequest{}, err
	}

	now := time.Now().UTC()
	decided, err := s.departmentRepository.DecideDepartmentTransfer(ctx, request.ID, input.RequestUser, input.Approve, strings.TrimSpace(input.Comment), now)
	if err != nil {
		return models.DepartmentTransferRequest{}, err
	}

	if input.Approve {
		if _, err := s.departmentRepository.MoveMemberToDepartment(ctx, models.MoveDepartmentMemberInput{
			CompanyUUID:      request.CompanyUUID,
			ToDepartmentUUID: request.ToDepartmentUUID,
			UserUUID:         request.UserUUID,
			Role:             models.DepartmentMemberRoleEmployee,
			Now:              now,
		}); err != nil {
			return models.DepartmentTransferRequest{}, err
		}
		s.notify(ctx, request.UserUUID, models.NotificationTypeDepartmentMemberMoved, "Вас перевели в другой отдел", "Доступ к звонкам обновлён", request.ToDepartmentUUID)
	}

	s.notify(ctx, request.RequestedByUserUUID, models.NotificationTypeDepartmentTransferDecided, "Решение по переводу", transferDecisionBody(input.Approve), request.ID)

	return decided, nil
}

// ListTransfers shows the owner and the deputy what leaders are asking for.
func (s *Service) ListTransfers(ctx context.Context, companyID uuid.UUID, requestUser uuid.UUID, status models.DepartmentTransferStatus) ([]models.DepartmentTransferRequest, error) {
	if companyID == uuid.Nil || requestUser == uuid.Nil {
		return nil, models.ErrInvalidDepartmentInput
	}
	if err := s.requireCompanyManager(ctx, companyID, requestUser); err != nil {
		return nil, err
	}

	return s.departmentRepository.ListDepartmentTransfers(ctx, companyID, status)
}

// ExpirePendingTransfers is the worker entry point.
func (s *Service) ExpirePendingTransfers(ctx context.Context) (int64, error) {
	return s.departmentRepository.ExpireDepartmentTransfers(ctx, time.Now().UTC())
}

func transferDecisionBody(approved bool) string {
	if approved {
		return "Перевод одобрен"
	}
	return "Перевод отклонён"
}

func optionalText(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
