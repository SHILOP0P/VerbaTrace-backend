package analysis

import (
	"context"
	"strings"
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

// rerunRequestRepository is implemented by the analysis repository. It stays a
// separate interface so the analysis service keeps working without it in tests
// that never touch reruns.
type rerunRequestRepository interface {
	CreateRerunRequest(context.Context, models.AnalysisRerunRequest) (models.AnalysisRerunRequest, error)
	GetRerunRequest(context.Context, uuid.UUID) (models.AnalysisRerunRequest, error)
	ListRerunRequests(context.Context, uuid.UUID, models.AnalysisRerunRequestStatus) ([]models.AnalysisRerunRequest, error)
	DecideRerunRequest(context.Context, uuid.UUID, uuid.UUID, bool, string, time.Time) (models.AnalysisRerunRequest, error)
}

type NotificationSender interface {
	Create(ctx context.Context, input models.CreateNotificationInput) (models.Notification, error)
}

func (s *Service) SetNotificationService(sender NotificationSender) { s.notifications = sender }

// canRerun answers who may start the analysis of a call again: the person who
// owns a personal call, and for a company call the leader of its department,
// the deputy and the owner. A plain employee has to ask.
func (s *Service) canRerun(ctx context.Context, call models.Call, userID uuid.UUID) (bool, error) {
	if !call.CompanyUUID.Valid {
		return call.UploadedByUserUUID.Valid && call.UploadedByUserUUID.UUID == userID, nil
	}
	if s.companyRepository == nil {
		return false, nil
	}
	member, err := s.companyRepository.GetCompanyMember(ctx, call.CompanyUUID.UUID, userID)
	if err == nil && member.Status == models.MembershipStatusActive && member.Role.ManagesCompany() {
		return true, nil
	}
	if !call.DepartmentUUID.Valid || s.departmentRepository == nil {
		return false, nil
	}
	leader, err := s.departmentRepository.GetDepartmentMember(ctx, call.CompanyUUID.UUID, call.DepartmentUUID.UUID, userID)
	if err != nil {
		return false, nil
	}

	return leader.Status == models.MembershipStatusActive && leader.Role == models.DepartmentMemberRoleLeader, nil
}

// RequestRerun records an employee's ask. The leader of the department decides,
// and when the department has no leader the deputy or the owner does.
func (s *Service) RequestRerun(ctx context.Context, input models.CreateAnalysisRerunRequestInput) (models.AnalysisRerunRequest, error) {
	requests, ok := s.analysisRepository.(rerunRequestRepository)
	if !ok || input.CallUUID == uuid.Nil || input.UserUUID == uuid.Nil {
		return models.AnalysisRerunRequest{}, models.ErrInvalidAnalysisInput
	}

	call, err := s.callRepository.GetByUUID(ctx, input.CallUUID, input.UserUUID)
	if err != nil {
		return models.AnalysisRerunRequest{}, err
	}
	if !call.CompanyUUID.Valid {
		// Nobody has to approve a re-run of your own personal call.
		return models.AnalysisRerunRequest{}, models.ErrInvalidAnalysisInput
	}

	now := time.Now().UTC()
	request, err := requests.CreateRerunRequest(ctx, models.AnalysisRerunRequest{
		ID:                  uuid.New(),
		CallUUID:            call.ID,
		CompanyUUID:         call.CompanyUUID.UUID,
		DepartmentUUID:      call.DepartmentUUID,
		RequestedByUserUUID: input.UserUUID,
		Reason:              optionalText(input.Reason),
		CreatedAt:           now,
	})
	if err != nil {
		return models.AnalysisRerunRequest{}, err
	}

	for _, approver := range s.rerunApprovers(ctx, call) {
		s.notify(ctx, approver, "analysis_rerun_requested", "Запрос на повторный анализ", "Сотрудник просит перезапустить анализ звонка", request.ID)
	}

	return request, nil
}

// DecideRerun approves or rejects the request; approval runs the analysis.
func (s *Service) DecideRerun(ctx context.Context, input models.DecideAnalysisRerunRequestInput) (models.AnalysisRerunRequest, error) {
	requests, ok := s.analysisRepository.(rerunRequestRepository)
	if !ok || input.RequestUUID == uuid.Nil || input.UserUUID == uuid.Nil {
		return models.AnalysisRerunRequest{}, models.ErrInvalidAnalysisInput
	}

	request, err := requests.GetRerunRequest(ctx, input.RequestUUID)
	if err != nil {
		return models.AnalysisRerunRequest{}, err
	}
	if request.Status != models.AnalysisRerunRequestStatusPending {
		return models.AnalysisRerunRequest{}, models.ErrAnalysisRerunRequestNotFound
	}

	call, err := s.callRepository.GetByUUID(ctx, request.CallUUID, input.UserUUID)
	if err != nil {
		return models.AnalysisRerunRequest{}, err
	}
	allowed, err := s.canRerun(ctx, call, input.UserUUID)
	if err != nil {
		return models.AnalysisRerunRequest{}, err
	}
	if !allowed {
		return models.AnalysisRerunRequest{}, models.ErrAnalysisRerunForbidden
	}

	decided, err := requests.DecideRerunRequest(ctx, request.ID, input.UserUUID, input.Approve, input.Comment, time.Now().UTC())
	if err != nil {
		return models.AnalysisRerunRequest{}, err
	}

	if input.Approve {
		if _, err := s.AnalyzeCall(ctx, models.AnalyzeCallInput{CallUUID: request.CallUUID, UserUUID: input.UserUUID}); err != nil {
			return models.AnalysisRerunRequest{}, err
		}
	}

	s.notify(ctx, request.RequestedByUserUUID, "analysis_rerun_decided", "Решение по повторному анализу", rerunDecisionBody(input.Approve), request.ID)

	return decided, nil
}

// ListRerunRequests shows the queue to the people who may decide on it.
func (s *Service) ListRerunRequests(ctx context.Context, input models.ListAnalysisRerunRequestsInput) ([]models.AnalysisRerunRequest, error) {
	requests, ok := s.analysisRepository.(rerunRequestRepository)
	if !ok || input.CompanyUUID == uuid.Nil || input.UserUUID == uuid.Nil || s.companyRepository == nil {
		return nil, models.ErrInvalidAnalysisInput
	}

	member, err := s.companyRepository.GetCompanyMember(ctx, input.CompanyUUID, input.UserUUID)
	if err != nil || member.Status != models.MembershipStatusActive {
		return nil, models.ErrForbidden
	}

	all, err := requests.ListRerunRequests(ctx, input.CompanyUUID, input.Status)
	if err != nil {
		return nil, err
	}
	if member.Role.ManagesCompany() {
		return all, nil
	}

	// A leader sees the calls of their own department and nothing else.
	departments, err := s.departmentRepository.ListUserDepartments(ctx, input.CompanyUUID, input.UserUUID)
	if err != nil {
		return nil, err
	}
	led := map[uuid.UUID]struct{}{}
	for _, department := range departments {
		if department.Role == models.DepartmentMemberRoleLeader {
			led[department.DepartmentUUID] = struct{}{}
		}
	}
	if len(led) == 0 {
		return nil, models.ErrForbidden
	}

	visible := make([]models.AnalysisRerunRequest, 0, len(all))
	for _, request := range all {
		if !request.DepartmentUUID.Valid {
			continue
		}
		if _, ok := led[request.DepartmentUUID.UUID]; ok {
			visible = append(visible, request)
		}
	}

	return visible, nil
}

// rerunApprovers is the leader of the call's department, and the deputy or the
// owner when the department has no leader.
func (s *Service) rerunApprovers(ctx context.Context, call models.Call) []uuid.UUID {
	if s.departmentRepository != nil && call.DepartmentUUID.Valid {
		members, err := s.departmentRepository.ListDepartmentMembers(ctx, call.CompanyUUID.UUID, call.DepartmentUUID.UUID)
		if err == nil {
			leaders := make([]uuid.UUID, 0, 1)
			for _, member := range members {
				if member.Role == models.DepartmentMemberRoleLeader && member.Status == models.MembershipStatusActive {
					leaders = append(leaders, member.UserUUID)
				}
			}
			if len(leaders) > 0 {
				return leaders
			}
		}
	}
	if s.companyRepository == nil {
		return nil
	}
	overview, err := s.companyRepository.GetCompanyMembersOverview(ctx, call.CompanyUUID.UUID)
	if err != nil {
		return nil
	}
	if overview.Deputy != nil {
		return []uuid.UUID{overview.Deputy.UserUUID}
	}
	if overview.Manager != nil {
		return []uuid.UUID{overview.Manager.UserUUID}
	}

	return nil
}

func (s *Service) notify(ctx context.Context, userID uuid.UUID, notificationType, title, body string, entityID uuid.UUID) {
	if s.notifications == nil || userID == uuid.Nil {
		return
	}
	entityType := "call_analysis_rerun_request"
	_, _ = s.notifications.Create(ctx, models.CreateNotificationInput{
		UserUUID:   userID,
		Type:       models.NotificationType(notificationType),
		Title:      title,
		Body:       body,
		EntityType: &entityType,
		EntityUUID: uuid.NullUUID{UUID: entityID, Valid: true},
		CreatedAt:  time.Now().UTC(),
	})
}

func rerunDecisionBody(approved bool) string {
	if approved {
		return "Повторный анализ запущен"
	}
	return "Повторный анализ отклонён"
}

func optionalText(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
