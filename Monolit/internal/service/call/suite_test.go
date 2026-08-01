package call

import (
	"context"
	"testing"

	"verbatrace/monolit/internal/logger"
	repositoryMocks "verbatrace/monolit/internal/repository/mocks"
	storageMocks "verbatrace/monolit/internal/storage/mocks"

	"github.com/stretchr/testify/suite"
)

type ServiceSuite struct {
	suite.Suite
	ctx          context.Context
	repository   *repositoryMocks.CallRepository
	companyRepo  *repositoryMocks.CompanyRepository
	deptRepo     *repositoryMocks.DepartmentRepository
	audioStorage *storageMocks.Storage
	service      *Service
}

func (s *ServiceSuite) SetupTest() {
	s.ctx = context.Background()
	s.repository = repositoryMocks.NewCallRepository(s.T())
	s.companyRepo = repositoryMocks.NewCompanyRepository(s.T())
	s.deptRepo = repositoryMocks.NewDepartmentRepository(s.T())
	s.audioStorage = storageMocks.NewStorage(s.T())
	s.service = NewService(s.repository, s.companyRepo, s.deptRepo, s.audioStorage, logger.NewNop())
}

func TestServiceSuite(t *testing.T) {
	suite.Run(t, new(ServiceSuite))
}
