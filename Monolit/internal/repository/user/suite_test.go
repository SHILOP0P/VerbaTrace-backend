//go:build integration

package user

import (
	"context"
	"database/sql"
	"testing"

	"verbatrace/monolit/internal/repository/repositorytest"

	"github.com/stretchr/testify/suite"
)

type RepositorySuite struct {
	suite.Suite
	ctx        context.Context
	db         *sql.DB
	repository *Repository
}

func (s *RepositorySuite) SetupSuite() {
	if testing.Short() {
		s.T().Skip("skip integration tests in short mode")
	}

	s.ctx = context.Background()
	s.db = repositorytest.OpenTestDB(s.T())
	repositorytest.RunMigrations(s.T(), s.db)
}

func (s *RepositorySuite) SetupTest() {
	repositorytest.TruncateTables(s.T(), s.db)
	s.repository = NewUserRepository(s.db)
}

func TestRepositorySuite(t *testing.T) {
	suite.Run(t, new(RepositorySuite))
}
