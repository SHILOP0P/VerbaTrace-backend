package auth

import (
	"context"
	"testing"
	"time"

	"verbatrace/monolit/internal/logger"
	repositoryMocks "verbatrace/monolit/internal/repository/mocks"

	"github.com/stretchr/testify/suite"
)

type ServiceSuite struct {
	suite.Suite
	ctx                      context.Context
	userRepository           *repositoryMocks.UserRepository
	refreshSessionRepository *repositoryMocks.RefreshSessionRepository
	service                  *Service
}

func (s *ServiceSuite) SetupTest() {
	s.ctx = context.Background()
	s.userRepository = repositoryMocks.NewUserRepository(s.T())
	s.refreshSessionRepository = repositoryMocks.NewRefreshSessionRepository(s.T())
	s.service = NewService(
		s.userRepository,
		s.refreshSessionRepository,
		"password-pepper",
		"jwt-secret",
		time.Minute,
		"refresh-secret",
		time.Hour,
		logger.NewNop(),
	)
}

func TestServiceSuite(t *testing.T) {
	suite.Run(t, new(ServiceSuite))
}
