package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"time"

	"verbatrace/monolit/internal/models"
	"verbatrace/monolit/internal/storage"
	"verbatrace/monolit/internal/username"

	"github.com/google/uuid"
)

var errAuditRepositoryNotConfigured = errors.New("admin audit repository is not configured")

type AuditRepository interface {
	CreateAdminAuditLog(ctx context.Context, audit models.AdminAuditLog) (models.AdminAuditLog, error)
	ListAdminUsers(ctx context.Context, input models.ListAdminUsersInput) (models.ListAdminUsersResult, error)
	GetAdminUserByUUID(ctx context.Context, userID uuid.UUID) (models.AdminUser, error)
	UpdateAdminUserProfile(ctx context.Context, input models.UpdateAdminUserProfileInput) (models.AdminUser, error)
	ChangeAdminUserRole(ctx context.Context, input models.ChangeAdminUserRoleInput) (models.AdminUser, error)
	ListAdminUserSessions(ctx context.Context, userID uuid.UUID) ([]models.AdminUserSession, error)
	RevokeAdminUserSession(ctx context.Context, input models.AdminSessionMutationInput) error
	RevokeAllAdminUserSessions(ctx context.Context, input models.AdminSessionMutationInput) error
	ListAdminCompanies(ctx context.Context, input models.ListAdminCompaniesInput) (models.ListAdminCompaniesResult, error)
	GetAdminCompanyByUUID(ctx context.Context, companyID uuid.UUID) (models.AdminCompany, error)
	GetAdminPersonalSubscription(ctx context.Context, userID uuid.UUID) (models.AdminSubscription, error)
	GetAdminCompanySubscription(ctx context.Context, companyID uuid.UUID) (models.AdminSubscription, error)
	GrantAdminSubscription(ctx context.Context, input models.GrantAdminSubscriptionInput) (models.AdminSubscription, error)
	CancelAdminSubscription(ctx context.Context, input models.CancelAdminSubscriptionInput) (models.AdminSubscription, error)
	ResetAdminUsage(ctx context.Context, input models.ResetAdminUsageInput) error
}

func (s *Service) ListCompanies(ctx context.Context, input models.ListAdminCompaniesInput) (models.ListAdminCompaniesResult, error) {
	if s.auditRepository == nil || input.Limit < 1 || input.Limit > 100 || input.Offset < 0 {
		return models.ListAdminCompaniesResult{}, models.ErrInvalidAdminInput
	}
	return s.auditRepository.ListAdminCompanies(ctx, input)
}
func (s *Service) GetCompany(ctx context.Context, id uuid.UUID) (models.AdminCompany, error) {
	if s.auditRepository == nil || id == uuid.Nil {
		return models.AdminCompany{}, models.ErrInvalidAdminInput
	}
	return s.auditRepository.GetAdminCompanyByUUID(ctx, id)
}
func (s *Service) GetPersonalSubscription(ctx context.Context, id uuid.UUID) (models.AdminSubscription, error) {
	if s.auditRepository == nil || id == uuid.Nil {
		return models.AdminSubscription{}, models.ErrInvalidAdminInput
	}
	return s.auditRepository.GetAdminPersonalSubscription(ctx, id)
}
func (s *Service) GetCompanySubscription(ctx context.Context, id uuid.UUID) (models.AdminSubscription, error) {
	if s.auditRepository == nil || id == uuid.Nil {
		return models.AdminSubscription{}, models.ErrInvalidAdminInput
	}
	return s.auditRepository.GetAdminCompanySubscription(ctx, id)
}
func (s *Service) GrantSubscription(ctx context.Context, in models.GrantAdminSubscriptionInput) (models.AdminSubscription, error) {
	if s.auditRepository == nil || in.ActorUserUUID == uuid.Nil || in.PlanCode == "" || in.EndsAt.IsZero() || in.StartsAt.IsZero() || !in.EndsAt.After(in.StartsAt) || strings.TrimSpace(in.Metadata.Reason) == "" || ((in.UserUUID == uuid.Nil) == (in.CompanyUUID == uuid.Nil)) || in.StartsAt.After(s.now().Add(time.Minute)) {
		return models.AdminSubscription{}, models.ErrInvalidAdminInput
	}
	return s.auditRepository.GrantAdminSubscription(ctx, in)
}
func (s *Service) CancelSubscription(ctx context.Context, in models.CancelAdminSubscriptionInput) (models.AdminSubscription, error) {
	if s.auditRepository == nil || in.ActorUserUUID == uuid.Nil || strings.TrimSpace(in.Metadata.Reason) == "" || ((in.UserUUID == uuid.Nil) == (in.CompanyUUID == uuid.Nil)) {
		return models.AdminSubscription{}, models.ErrInvalidAdminInput
	}
	return s.auditRepository.CancelAdminSubscription(ctx, in)
}

func (s *Service) ListUsers(ctx context.Context, input models.ListAdminUsersInput) (models.ListAdminUsersResult, error) {
	if s.auditRepository == nil || input.Limit < 1 || input.Limit > 100 || input.Offset < 0 || (input.Role != nil && !models.IsValidUserRole(*input.Role)) {
		return models.ListAdminUsersResult{}, models.ErrInvalidAdminInput
	}
	return s.auditRepository.ListAdminUsers(ctx, input)
}

func (s *Service) GetUser(ctx context.Context, userID uuid.UUID) (models.AdminUser, error) {
	if s.auditRepository == nil || userID == uuid.Nil {
		return models.AdminUser{}, models.ErrInvalidAdminInput
	}
	return s.auditRepository.GetAdminUserByUUID(ctx, userID)
}

func (s *Service) UpdateUserProfile(ctx context.Context, input models.UpdateAdminUserProfileInput) (models.AdminUser, error) {
	if s.auditRepository == nil || input.ActorUserUUID == uuid.Nil || input.TargetUserUUID == uuid.Nil || strings.TrimSpace(input.Metadata.Reason) == "" || (input.FullName == nil && input.FullSurname == nil && input.Username == nil && input.Post == nil) {
		return models.AdminUser{}, models.ErrInvalidAdminInput
	}
	input.FullName = normalizeRequiredPatchString(input.FullName)
	input.FullSurname = normalizeRequiredPatchString(input.FullSurname)
	input.Post = normalizeOptionalString(input.Post)
	if (input.FullName != nil && *input.FullName == "") || (input.FullSurname != nil && *input.FullSurname == "") {
		return models.AdminUser{}, models.ErrInvalidAdminInput
	}
	if input.Username != nil {
		normalized, ok := username.Normalize(*input.Username)
		if !ok {
			return models.AdminUser{}, models.ErrInvalidAdminInput
		}
		input.Username = &normalized
	}
	return s.auditRepository.UpdateAdminUserProfile(ctx, input)
}

func (s *Service) ChangeUserRole(ctx context.Context, input models.ChangeAdminUserRoleInput) (models.AdminUser, error) {
	if s.auditRepository == nil || input.ActorUserUUID == uuid.Nil || input.TargetUserUUID == uuid.Nil || !models.IsValidUserRole(input.ExpectedRole) || !models.IsValidUserRole(input.Role) || strings.TrimSpace(input.Metadata.Reason) == "" {
		return models.AdminUser{}, models.ErrInvalidAdminInput
	}
	if input.ActorUserUUID == input.TargetUserUUID {
		return models.AdminUser{}, models.ErrCannotChangeOwnRole
	}
	return s.auditRepository.ChangeAdminUserRole(ctx, input)
}

func (s *Service) ResetUsage(ctx context.Context, input models.ResetAdminUsageInput) error {
	if s.auditRepository == nil || input.ActorUserUUID == uuid.Nil || strings.TrimSpace(input.Metadata.Reason) == "" || (input.UserUUID == uuid.Nil && input.CompanyUUID == uuid.Nil) || (input.UserUUID != uuid.Nil && input.CompanyUUID != uuid.Nil) {
		return models.ErrInvalidAdminInput
	}
	return s.auditRepository.ResetAdminUsage(ctx, input)
}

type allowanceResetBatchRepository interface {
	CreateAllowanceResetBatch(context.Context, uuid.UUID, string, []uuid.UUID, string, string) (models.AllowanceResetBatch, error)
	ApproveAllowanceResetBatch(context.Context, uuid.UUID, uuid.UUID) (models.AllowanceResetBatch, error)
	ExecuteAllowanceResetBatch(context.Context, uuid.UUID, uuid.UUID) (models.AllowanceResetBatch, error)
	GetAllowanceResetBatch(context.Context, uuid.UUID, uuid.UUID) (models.AllowanceResetBatch, error)
	PreviewAllowanceResetBatch(context.Context, uuid.UUID, string, []uuid.UUID) (int, error)
}

func (s *Service) PreviewAllowanceResetBatch(ctx context.Context, actor uuid.UUID, ownerType string, owners []uuid.UUID) (int, error) {
	repo, ok := s.auditRepository.(allowanceResetBatchRepository)
	if !ok {
		return 0, errAuditRepositoryNotConfigured
	}
	return repo.PreviewAllowanceResetBatch(ctx, actor, ownerType, owners)
}

func (s *Service) CreateAllowanceResetBatch(ctx context.Context, actor uuid.UUID, ownerType string, owners []uuid.UUID, reason, key string) (models.AllowanceResetBatch, error) {
	repo, ok := s.auditRepository.(allowanceResetBatchRepository)
	if !ok {
		return models.AllowanceResetBatch{}, errAuditRepositoryNotConfigured
	}
	return repo.CreateAllowanceResetBatch(ctx, actor, ownerType, owners, reason, key)
}
func (s *Service) ApproveAllowanceResetBatch(ctx context.Context, id, actor uuid.UUID) (models.AllowanceResetBatch, error) {
	repo, ok := s.auditRepository.(allowanceResetBatchRepository)
	if !ok {
		return models.AllowanceResetBatch{}, errAuditRepositoryNotConfigured
	}
	return repo.ApproveAllowanceResetBatch(ctx, id, actor)
}
func (s *Service) ExecuteAllowanceResetBatch(ctx context.Context, id, actor uuid.UUID) (models.AllowanceResetBatch, error) {
	repo, ok := s.auditRepository.(allowanceResetBatchRepository)
	if !ok {
		return models.AllowanceResetBatch{}, errAuditRepositoryNotConfigured
	}
	return repo.ExecuteAllowanceResetBatch(ctx, id, actor)
}
func (s *Service) GetAllowanceResetBatch(ctx context.Context, id, actor uuid.UUID) (models.AllowanceResetBatch, error) {
	repo, ok := s.auditRepository.(allowanceResetBatchRepository)
	if !ok {
		return models.AllowanceResetBatch{}, errAuditRepositoryNotConfigured
	}
	return repo.GetAllowanceResetBatch(ctx, id, actor)
}

func (s *Service) ListUserSessions(ctx context.Context, actorUserID uuid.UUID, targetUserID uuid.UUID) ([]models.AdminUserSession, error) {
	if s.auditRepository == nil || actorUserID == uuid.Nil || targetUserID == uuid.Nil {
		return nil, models.ErrInvalidAdminInput
	}
	actor, err := s.auditRepository.GetAdminUserByUUID(ctx, actorUserID)
	if err != nil {
		return nil, err
	}
	target, err := s.auditRepository.GetAdminUserByUUID(ctx, targetUserID)
	if err != nil {
		return nil, err
	}
	if actor.ID == target.ID {
		return nil, models.ErrAdminSessionManagementForbidden
	}
	if err := models.ValidateAdminSessionTarget(actor.Role, target.Role); err != nil {
		return nil, err
	}
	return s.auditRepository.ListAdminUserSessions(ctx, targetUserID)
}

func (s *Service) RevokeUserSession(ctx context.Context, input models.AdminSessionMutationInput) error {
	return s.validateSessionMutation(ctx, input, false)
}
func (s *Service) RevokeAllUserSessions(ctx context.Context, input models.AdminSessionMutationInput) error {
	return s.validateSessionMutation(ctx, input, true)
}
func (s *Service) validateSessionMutation(ctx context.Context, input models.AdminSessionMutationInput, all bool) error {
	if s.auditRepository == nil || input.ActorUserUUID == uuid.Nil || input.TargetUserUUID == uuid.Nil || (!all && input.SessionUUID == uuid.Nil) || strings.TrimSpace(input.Metadata.Reason) == "" {
		return models.ErrInvalidAdminInput
	}
	if input.ActorUserUUID == input.TargetUserUUID {
		return models.ErrAdminSessionManagementForbidden
	}
	if all {
		return s.auditRepository.RevokeAllAdminUserSessions(ctx, input)
	}
	return s.auditRepository.RevokeAdminUserSession(ctx, input)
}

type Service struct {
	auditRepository AuditRepository
	now             func() time.Time
	callReader      interface {
		GetByUUIDForProcessing(context.Context, uuid.UUID) (models.Call, error)
		ListFiltered(context.Context, models.ListCallsInput) (models.ListCallsResult, error)
	}
	audioStorage storage.AudioStorage
	mediaPrivacy MediaPrivacyGuard
}

// MediaPrivacyGuard says which copy of a recording support may listen to. It is
// the privacy service; the interface keeps the admin service from depending on
// it directly.
type MediaPrivacyGuard interface {
	SupportMediaVariant(ctx context.Context, call models.Call) (string, bool, error)
}

func (s *Service) SetMediaPrivacyGuard(guard MediaPrivacyGuard) { s.mediaPrivacy = guard }

func (s *Service) SetCallReader(reader interface {
	GetByUUIDForProcessing(context.Context, uuid.UUID) (models.Call, error)
	ListFiltered(context.Context, models.ListCallsInput) (models.ListCallsResult, error)
}) {
	s.callReader = reader
}
func (s *Service) SetAudioStorage(audioStorage storage.AudioStorage) { s.audioStorage = audioStorage }
func (s *Service) GetCall(ctx context.Context, id uuid.UUID) (models.Call, error) {
	if s.callReader == nil || id == uuid.Nil {
		return models.Call{}, models.ErrInvalidAdminInput
	}
	return s.callReader.GetByUUIDForProcessing(ctx, id)
}
func (s *Service) ListUserCalls(ctx context.Context, userID uuid.UUID, limit int, offset int) (models.ListCallsResult, error) {
	if s.callReader == nil || userID == uuid.Nil || limit < 1 || limit > 100 || offset < 0 {
		return models.ListCallsResult{}, models.ErrInvalidAdminInput
	}
	return s.callReader.ListFiltered(ctx, models.ListCallsInput{UserID: userID, UploadedByUserUUID: uuid.NullUUID{UUID: userID, Valid: true}, Limit: limit, Offset: offset})
}

// GetCallAudio serves a recording to support. The customer's masking policy
// applies here too: where it is on, support hears the redacted copy, not the
// original. Opening the original file directly was the one path that ignored the
// policy entirely.
func (s *Service) GetCallAudio(ctx context.Context, id uuid.UUID) (models.File, error) {
	call, err := s.GetCall(ctx, id)
	if err != nil {
		return models.File{}, err
	}
	if s.audioStorage == nil {
		return models.File{}, errAuditRepositoryNotConfigured
	}

	path, redacted := call.AudioPath, false
	if s.mediaPrivacy != nil {
		path, redacted, err = s.mediaPrivacy.SupportMediaVariant(ctx, call)
		if err != nil {
			return models.File{}, err
		}
	}

	content, err := s.audioStorage.OpenReadSeeker(ctx, path)
	if err != nil {
		return models.File{}, err
	}

	name := call.OriginalFilename
	if redacted {
		name = "redacted-" + name
	}

	return models.File{Content: content, ReadSeeker: content, Path: path, OriginalFilename: name, MimeType: call.MimeType, SizeBytes: call.SizeBytes}, nil
}

func NewService(auditRepository AuditRepository) *Service {
	return &Service{
		auditRepository: auditRepository,
		now:             func() time.Time { return time.Now().UTC() },
	}
}

func (s *Service) GetCapabilities(_ context.Context, role models.UserRole) (models.AdminCapabilities, error) {
	if !models.HasAdminPermission(role, models.AdminPermissionPanelAccess) {
		return models.AdminCapabilities{}, models.ErrForbidden
	}

	return models.AdminCapabilities{
		Role:        role,
		Permissions: models.AdminPermissionsForRole(role),
	}, nil
}

func (s *Service) RecordAudit(ctx context.Context, input models.CreateAdminAuditLogInput) (models.AdminAuditLog, error) {
	input.Action = strings.TrimSpace(input.Action)
	input.TargetType = strings.TrimSpace(input.TargetType)

	if input.ActorUserUUID == uuid.Nil ||
		!models.HasAdminPermission(input.ActorRole, models.AdminPermissionPanelAccess) ||
		input.Action == "" ||
		input.TargetType == "" ||
		!validJSONObject(input.BeforeData) ||
		!validJSONObject(input.AfterData) ||
		!validOptionalIPAddress(input.IPAddress) {
		return models.AdminAuditLog{}, models.ErrInvalidAdminInput
	}

	if s.auditRepository == nil {
		return models.AdminAuditLog{}, errAuditRepositoryNotConfigured
	}

	auditID, err := uuid.NewV7()
	if err != nil {
		return models.AdminAuditLog{}, err
	}

	return s.auditRepository.CreateAdminAuditLog(ctx, models.AdminAuditLog{
		ID:            auditID,
		ActorUserUUID: input.ActorUserUUID,
		ActorRole:     input.ActorRole,
		Action:        input.Action,
		TargetType:    input.TargetType,
		TargetUUID:    input.TargetUUID,
		BeforeData:    input.BeforeData,
		AfterData:     input.AfterData,
		Reason:        normalizeOptionalString(input.Reason),
		RequestID:     normalizeOptionalString(input.RequestID),
		IPAddress:     normalizeOptionalString(input.IPAddress),
		UserAgent:     normalizeOptionalString(input.UserAgent),
		CreatedAt:     s.now().UTC(),
	})
}

func validJSONObject(value json.RawMessage) bool {
	if len(value) == 0 {
		return true
	}

	var object map[string]any
	return json.Unmarshal(value, &object) == nil && object != nil
}

func validOptionalIPAddress(value *string) bool {
	if value == nil || strings.TrimSpace(*value) == "" {
		return true
	}

	return net.ParseIP(strings.TrimSpace(*value)) != nil
}

func normalizeOptionalString(value *string) *string {
	if value == nil {
		return nil
	}

	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}

	return &trimmed
}

func normalizeRequiredPatchString(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	return &trimmed
}
