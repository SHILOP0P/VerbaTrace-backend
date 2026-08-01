//go:build integration

package refresh_session

import (
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func (s *RepositorySuite) createUser() models.CurrentUser {
	user := models.CurrentUser{
		ID:           uuid.New(),
		Email:        uuid.NewString() + "@example.com",
		PasswordHash: "hash",
		FullName:     "Dmitry",
		FullSurname:  "Mukhachev",
		Username:     "muxa",
		Role:         models.UserRoleUser,
		CreatedAt:    time.Now().UTC().Truncate(time.Microsecond),
	}

	created, err := s.userRepository.CreateUser(s.ctx, user)
	s.Require().NoError(err)

	return created
}

func testRefreshSession(userID uuid.UUID) models.RefreshSession {
	userAgent := "Postman"
	ipAddress := "127.0.0.1"
	now := time.Now().UTC().Truncate(time.Microsecond)

	return models.RefreshSession{
		ID:               uuid.New(),
		UserID:           userID,
		RefreshTokenHash: uuid.NewString(),
		AccessVersion:    1,
		UserAgent:        &userAgent,
		IPAddress:        &ipAddress,
		CreatedAt:        now,
		ExpiresAt:        now.Add(time.Hour),
	}
}

func (s *RepositorySuite) TestCreateRefreshSessionAndGetters() {
	user := s.createUser()
	session := testRefreshSession(user.ID)

	created, err := s.repository.CreateRefreshSession(s.ctx, session)
	s.Require().NoError(err)
	s.Require().Equal(session.ID, created.ID)
	s.Require().Equal(session.UserID, created.UserID)
	s.Require().Equal(session.RefreshTokenHash, created.RefreshTokenHash)
	s.Require().Equal(session.AccessVersion, created.AccessVersion)
	s.Require().NotNil(created.UserAgent)
	s.Require().Equal(*session.UserAgent, *created.UserAgent)
	s.Require().NotNil(created.IPAddress)
	s.Require().Equal("127.0.0.1/32", *created.IPAddress)
	s.Require().Nil(created.LastUsedAt)
	s.Require().Nil(created.RevokedAt)
	s.Require().Nil(created.RevokedReason)

	byID, err := s.repository.GetRefreshSessionByUUID(s.ctx, session.ID)
	s.Require().NoError(err)
	s.Require().Equal(created, byID)

	byHash, err := s.repository.GetRefreshSessionByHash(s.ctx, session.RefreshTokenHash)
	s.Require().NoError(err)
	s.Require().Equal(created, byHash)
}

func (s *RepositorySuite) TestGetRefreshSessionNotFound() {
	_, err := s.repository.GetRefreshSessionByUUID(s.ctx, uuid.New())
	s.Require().ErrorIs(err, models.ErrRefreshSessionNotFound)

	_, err = s.repository.GetRefreshSessionByHash(s.ctx, "missing")
	s.Require().ErrorIs(err, models.ErrRefreshSessionNotFound)
}

func (s *RepositorySuite) TestRotateRefreshSession() {
	user := s.createUser()
	session := testRefreshSession(user.ID)
	_, err := s.repository.CreateRefreshSession(s.ctx, session)
	s.Require().NoError(err)

	newHash := uuid.NewString()
	newExpiresAt := time.Now().UTC().Add(2 * time.Hour).Truncate(time.Microsecond)

	rotated, err := s.repository.RotateRefreshSession(s.ctx, session.RefreshTokenHash, newHash, newExpiresAt)
	s.Require().NoError(err)
	s.Require().Equal(session.ID, rotated.ID)
	s.Require().Equal(newHash, rotated.RefreshTokenHash)
	s.Require().NotNil(rotated.LastUsedAt)
	s.Require().True(rotated.ExpiresAt.Equal(newExpiresAt))

	_, err = s.repository.GetRefreshSessionByHash(s.ctx, session.RefreshTokenHash)
	s.Require().ErrorIs(err, models.ErrRefreshSessionNotFound)
}

func (s *RepositorySuite) TestRotateRefreshSessionRejectsRevokedOrExpiredSession() {
	user := s.createUser()

	revoked := testRefreshSession(user.ID)
	_, err := s.repository.CreateRefreshSession(s.ctx, revoked)
	s.Require().NoError(err)
	s.Require().NoError(s.repository.RevokeRefreshSession(s.ctx, revoked.ID, "logout"))

	_, err = s.repository.RotateRefreshSession(s.ctx, revoked.RefreshTokenHash, uuid.NewString(), time.Now().UTC().Add(time.Hour))
	s.Require().ErrorIs(err, models.ErrRefreshSessionNotFound)

	expired := testRefreshSession(user.ID)
	expired.ID = uuid.New()
	expired.RefreshTokenHash = uuid.NewString()
	expired.CreatedAt = time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Microsecond)
	expired.ExpiresAt = time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	_, err = s.repository.CreateRefreshSession(s.ctx, expired)
	s.Require().NoError(err)

	_, err = s.repository.RotateRefreshSession(s.ctx, expired.RefreshTokenHash, uuid.NewString(), time.Now().UTC().Add(time.Hour))
	s.Require().ErrorIs(err, models.ErrRefreshSessionNotFound)
}

func (s *RepositorySuite) TestRevokeRefreshSession() {
	user := s.createUser()
	session := testRefreshSession(user.ID)
	_, err := s.repository.CreateRefreshSession(s.ctx, session)
	s.Require().NoError(err)

	err = s.repository.RevokeRefreshSession(s.ctx, session.ID, "logout")
	s.Require().NoError(err)

	revoked, err := s.repository.GetRefreshSessionByUUID(s.ctx, session.ID)
	s.Require().NoError(err)
	s.Require().NotNil(revoked.RevokedAt)
	s.Require().NotNil(revoked.RevokedReason)
	s.Require().Equal("logout", *revoked.RevokedReason)
}

func (s *RepositorySuite) TestRevokeRefreshSessionNotFound() {
	err := s.repository.RevokeRefreshSession(s.ctx, uuid.New(), "logout")

	s.Require().ErrorIs(err, models.ErrRefreshSessionNotFound)
}

func (s *RepositorySuite) TestRevokeAllUserRefreshSessions() {
	user := s.createUser()
	first := testRefreshSession(user.ID)
	second := testRefreshSession(user.ID)
	second.ID = uuid.New()
	second.RefreshTokenHash = uuid.NewString()

	_, err := s.repository.CreateRefreshSession(s.ctx, first)
	s.Require().NoError(err)
	_, err = s.repository.CreateRefreshSession(s.ctx, second)
	s.Require().NoError(err)

	err = s.repository.RevokeAllUserRefreshSessions(s.ctx, user.ID, "logout_all")
	s.Require().NoError(err)

	firstRevoked, err := s.repository.GetRefreshSessionByUUID(s.ctx, first.ID)
	s.Require().NoError(err)
	s.Require().NotNil(firstRevoked.RevokedAt)
	s.Require().NotNil(firstRevoked.RevokedReason)
	s.Require().Equal("logout_all", *firstRevoked.RevokedReason)

	secondRevoked, err := s.repository.GetRefreshSessionByUUID(s.ctx, second.ID)
	s.Require().NoError(err)
	s.Require().NotNil(secondRevoked.RevokedAt)
	s.Require().NotNil(secondRevoked.RevokedReason)
	s.Require().Equal("logout_all", *secondRevoked.RevokedReason)
}

func (s *RepositorySuite) TestInvalidateAccessVersions() {
	user := s.createUser()
	first := testRefreshSession(user.ID)
	second := testRefreshSession(user.ID)
	second.ID = uuid.New()
	second.RefreshTokenHash = uuid.NewString()

	_, err := s.repository.CreateRefreshSession(s.ctx, first)
	s.Require().NoError(err)
	_, err = s.repository.CreateRefreshSession(s.ctx, second)
	s.Require().NoError(err)

	s.Require().NoError(s.repository.InvalidateSessionAccess(s.ctx, first.ID))
	updatedFirst, err := s.repository.GetRefreshSessionByUUID(s.ctx, first.ID)
	s.Require().NoError(err)
	s.Require().Equal(int64(2), updatedFirst.AccessVersion)
	unchangedSecond, err := s.repository.GetRefreshSessionByUUID(s.ctx, second.ID)
	s.Require().NoError(err)
	s.Require().Equal(int64(1), unchangedSecond.AccessVersion)

	s.Require().NoError(s.repository.InvalidateAllUserAccess(s.ctx, user.ID))
	updatedFirst, err = s.repository.GetRefreshSessionByUUID(s.ctx, first.ID)
	s.Require().NoError(err)
	updatedSecond, err := s.repository.GetRefreshSessionByUUID(s.ctx, second.ID)
	s.Require().NoError(err)
	s.Require().Equal(int64(3), updatedFirst.AccessVersion)
	s.Require().Equal(int64(2), updatedSecond.AccessVersion)
}
