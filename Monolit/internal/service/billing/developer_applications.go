package billing

import (
	"context"
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

type developerRepository interface {
	CreateDeveloperApplication(context.Context, models.CreateDeveloperApplicationInput) (models.DeveloperApplication, error)
	ListDeveloperApplications(context.Context, string, uuid.UUID) ([]models.DeveloperApplication, error)
	CreateIntegrationAPIKey(context.Context, uuid.UUID, uuid.UUID, models.CreateIntegrationAPIKeyInput) (models.IntegrationAPIKey, string, error)
	AuthenticateIntegrationKey(context.Context, string, string, string) (models.IntegrationPrincipal, error)
	RevokeIntegrationAPIKey(context.Context, uuid.UUID, uuid.UUID) error
	RotateIntegrationAPIKey(context.Context, uuid.UUID, uuid.UUID, time.Duration) (models.IntegrationAPIKey, string, error)
	CreateIntegrationServiceAccount(context.Context, uuid.UUID, uuid.UUID, string, []string) (models.IntegrationServiceAccount, error)
	ListIntegrationServiceAccounts(context.Context, uuid.UUID, uuid.UUID) ([]models.IntegrationServiceAccount, error)
	CreateIntegrationAPIKeyForServiceAccount(context.Context, uuid.UUID, uuid.UUID, models.CreateIntegrationAPIKeyInput) (models.IntegrationAPIKey, string, error)
	GetDeveloperApplication(context.Context, uuid.UUID, uuid.UUID) (models.DeveloperApplication, error)
	ChangeDeveloperApplicationStatus(context.Context, uuid.UUID, uuid.UUID, string) (models.DeveloperApplication, error)
	AdjustSandboxWallet(context.Context, uuid.UUID, uuid.UUID, string, int64, string) (int64, error)
	GetSandboxWallet(context.Context, uuid.UUID, uuid.UUID) (models.SandboxWalletDashboard, error)
	ListIntegrationAPIKeys(context.Context, uuid.UUID, uuid.UUID) ([]models.IntegrationAPIKey, error)
	UpdateDeveloperApplication(context.Context, models.UpdateDeveloperApplicationInput) (models.DeveloperApplication, error)
	RevokeIntegrationServiceAccount(context.Context, uuid.UUID, uuid.UUID) error
}

func (s *Service) RevokeIntegrationServiceAccount(ctx context.Context, service, actor uuid.UUID) error {
	return s.developerRepository.RevokeIntegrationServiceAccount(ctx, service, actor)
}

func (s *Service) UpdateDeveloperApplication(ctx context.Context, input models.UpdateDeveloperApplicationInput) (models.DeveloperApplication, error) {
	return s.developerRepository.UpdateDeveloperApplication(ctx, input)
}
func (s *Service) ListIntegrationAPIKeys(ctx context.Context, service, actor uuid.UUID) ([]models.IntegrationAPIKey, error) {
	return s.developerRepository.ListIntegrationAPIKeys(ctx, service, actor)
}

func (s *Service) AdjustSandboxWallet(ctx context.Context, app, actor uuid.UUID, mode string, credits int64, requestID string) (int64, error) {
	return s.developerRepository.AdjustSandboxWallet(ctx, app, actor, mode, credits, requestID)
}

func (s *Service) GetSandboxWallet(ctx context.Context, app, actor uuid.UUID) (models.SandboxWalletDashboard, error) {
	return s.developerRepository.GetSandboxWallet(ctx, app, actor)
}

func (s *Service) GetDeveloperApplication(ctx context.Context, app, actor uuid.UUID) (models.DeveloperApplication, error) {
	return s.developerRepository.GetDeveloperApplication(ctx, app, actor)
}
func (s *Service) ChangeDeveloperApplicationStatus(ctx context.Context, app, actor uuid.UUID, status string) (models.DeveloperApplication, error) {
	return s.developerRepository.ChangeDeveloperApplicationStatus(ctx, app, actor, status)
}

func (s *Service) CreateIntegrationServiceAccount(ctx context.Context, connection, actor uuid.UUID, name string, scopes []string) (models.IntegrationServiceAccount, error) {
	return s.developerRepository.CreateIntegrationServiceAccount(ctx, connection, actor, name, scopes)
}
func (s *Service) ListIntegrationServiceAccounts(ctx context.Context, connection, actor uuid.UUID) ([]models.IntegrationServiceAccount, error) {
	return s.developerRepository.ListIntegrationServiceAccounts(ctx, connection, actor)
}
func (s *Service) CreateIntegrationAPIKeyForServiceAccount(ctx context.Context, serviceAccount, actor uuid.UUID, input models.CreateIntegrationAPIKeyInput) (models.IntegrationAPIKey, string, error) {
	return s.developerRepository.CreateIntegrationAPIKeyForServiceAccount(ctx, serviceAccount, actor, input)
}

func (s *Service) RevokeIntegrationAPIKey(ctx context.Context, id, actor uuid.UUID) error {
	return s.developerRepository.RevokeIntegrationAPIKey(ctx, id, actor)
}
func (s *Service) RotateIntegrationAPIKey(ctx context.Context, id, actor uuid.UUID, overlap time.Duration) (models.IntegrationAPIKey, string, error) {
	return s.developerRepository.RotateIntegrationAPIKey(ctx, id, actor, overlap)
}

func (s *Service) AuthenticateIntegrationKey(ctx context.Context, key, environment, scope string) (models.IntegrationPrincipal, error) {
	return s.developerRepository.AuthenticateIntegrationKey(ctx, key, environment, scope)
}

func (s *Service) CreateDeveloperApplication(ctx context.Context, input models.CreateDeveloperApplicationInput) (models.DeveloperApplication, error) {
	if input.OwnerType == "user" {
		if input.OwnerUUID != input.CreatedByUserUUID {
			return models.DeveloperApplication{}, models.ErrForbidden
		}
		subscription, err := s.GetPersonalSubscription(ctx, input.OwnerUUID)
		if err != nil {
			return models.DeveloperApplication{}, err
		}
		if !subscription.Plan.APIAccessEnabled {
			return models.DeveloperApplication{}, models.ErrAPIAccessDenied
		}
	} else if err := s.requireCompanyManager(ctx, input.OwnerUUID, input.CreatedByUserUUID); err != nil {
		return models.DeveloperApplication{}, err
	} else if err := s.CanAccessAPI(ctx, input.OwnerUUID); err != nil {
		return models.DeveloperApplication{}, err
	}
	return s.developerRepository.CreateDeveloperApplication(ctx, input)
}

func (s *Service) ListDeveloperApplications(ctx context.Context, ownerType string, ownerID, actorID uuid.UUID) ([]models.DeveloperApplication, error) {
	if ownerType == "user" {
		if ownerID != actorID {
			return nil, models.ErrForbidden
		}
		subscription, err := s.GetPersonalSubscription(ctx, ownerID)
		if err != nil {
			return nil, err
		}
		if !subscription.Plan.APIAccessEnabled {
			return nil, models.ErrAPIAccessDenied
		}
	} else {
		if err := s.requireCompanyManager(ctx, ownerID, actorID); err != nil {
			return nil, err
		}
		if err := s.CanAccessAPI(ctx, ownerID); err != nil {
			return nil, err
		}
	}
	return s.developerRepository.ListDeveloperApplications(ctx, ownerType, ownerID)
}

func (s *Service) CreateIntegrationAPIKey(ctx context.Context, applicationID, actorID uuid.UUID, input models.CreateIntegrationAPIKeyInput) (models.IntegrationAPIKey, string, error) {
	return s.developerRepository.CreateIntegrationAPIKey(ctx, applicationID, actorID, input)
}
