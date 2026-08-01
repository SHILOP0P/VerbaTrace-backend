package auth

import (
	"context"
	"time"

	"verbatrace/monolit/internal/logger"
	"verbatrace/monolit/internal/models"
	repo "verbatrace/monolit/internal/repository"
	"verbatrace/monolit/internal/storage"
)

type BillingRepository interface {
	UpsertSubscription(ctx context.Context, input models.UpsertSubscriptionInput) (models.Subscription, error)
}

type Service struct {
	userRepository           repo.UserRepository
	refreshSessionRepository repo.RefreshSessionRepository
	billingRepository        BillingRepository
	companyRepository        repo.CompanyRepository
	preferencesRepository    repo.UserPreferencesRepository
	avatarStorage            storage.AvatarStorage

	passwordPepper     string
	jwtSecret          string
	accessTokenTTL     time.Duration
	refreshTokenSecret string
	refreshTokenTTL    time.Duration
	sessionTrustAge    time.Duration
	now                func() time.Time
	log                logger.Logger
}

func (s *Service) SetBillingRepository(repository BillingRepository) {
	s.billingRepository = repository
}

func (s *Service) SetCompanyRepository(repository repo.CompanyRepository) {
	s.companyRepository = repository
}

func (s *Service) SetPreferencesRepository(repository repo.UserPreferencesRepository) {
	s.preferencesRepository = repository
}

func (s *Service) SetAvatarStorage(avatarStorage storage.AvatarStorage) {
	s.avatarStorage = avatarStorage
}

func NewService(
	userRepository repo.UserRepository,
	refreshSessionRepository repo.RefreshSessionRepository,
	passwordPepper string,
	jwtSecret string,
	accessTokenTTL time.Duration,
	refreshTokenSecret string,
	refreshTokenTTL time.Duration,
	log logger.Logger,
) *Service {
	if log == nil {
		log = logger.NewNop()
	}

	return &Service{
		userRepository:           userRepository,
		refreshSessionRepository: refreshSessionRepository,
		passwordPepper:           passwordPepper,
		jwtSecret:                jwtSecret,
		accessTokenTTL:           accessTokenTTL,
		refreshTokenSecret:       refreshTokenSecret,
		refreshTokenTTL:          refreshTokenTTL,
		sessionTrustAge:          24 * time.Hour,
		now:                      func() time.Time { return time.Now().UTC() },
		log:                      log,
	}
}

func (s *Service) SetSessionTrustAge(age time.Duration) {
	if age >= 0 {
		s.sessionTrustAge = age
	}
}
