//go:build integration

package admin

import (
	"encoding/json"
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func (s *RepositorySuite) TestCreateAdminAuditLog() {
	actor := s.createUser(models.UserRoleAdmin)
	targetID := uuid.New()
	reason := "manual incident response"
	requestID := "request-123"
	ipAddress := "127.0.0.1"
	userAgent := "Chrome"
	createdAt := time.Now().UTC().Truncate(time.Microsecond)

	created, err := s.repository.CreateAdminAuditLog(s.ctx, models.AdminAuditLog{
		ID:            uuid.New(),
		ActorUserUUID: actor.ID,
		ActorRole:     actor.Role,
		Action:        "session.revoked",
		TargetType:    "refresh_session",
		TargetUUID:    uuid.NullUUID{UUID: targetID, Valid: true},
		BeforeData:    json.RawMessage(`{"revoked":false}`),
		AfterData:     json.RawMessage(`{"revoked":true}`),
		Reason:        &reason,
		RequestID:     &requestID,
		IPAddress:     &ipAddress,
		UserAgent:     &userAgent,
		CreatedAt:     createdAt,
	})

	s.Require().NoError(err)
	s.Require().Equal(actor.ID, created.ActorUserUUID)
	s.Require().Equal(models.UserRoleAdmin, created.ActorRole)
	s.Require().Equal("session.revoked", created.Action)
	s.Require().True(created.TargetUUID.Valid)
	s.Require().Equal(targetID, created.TargetUUID.UUID)
	s.Require().JSONEq(`{"revoked":false}`, string(created.BeforeData))
	s.Require().JSONEq(`{"revoked":true}`, string(created.AfterData))
	s.Require().Equal(reason, *created.Reason)
	s.Require().Equal(requestID, *created.RequestID)
	s.Require().Equal("127.0.0.1/32", *created.IPAddress)
	s.Require().Equal(userAgent, *created.UserAgent)
	s.Require().True(createdAt.Equal(created.CreatedAt))
}

func (s *RepositorySuite) TestRoleConstraintAndSingletonSuperadmin() {
	for _, role := range []models.UserRole{
		models.UserRoleUser,
		models.UserRoleHelper,
		models.UserRoleAdmin,
		models.UserRoleSuperAdmin,
	} {
		_, err := s.userRepository.CreateUser(s.ctx, testAdminUser(role))
		s.Require().NoError(err, role)
	}

	_, err := s.userRepository.CreateUser(s.ctx, testAdminUser("unsupported"))
	s.Require().Error(err)

	_, err = s.userRepository.CreateUser(s.ctx, testAdminUser(models.UserRoleSuperAdmin))
	s.Require().Error(err)
}

func (s *RepositorySuite) createUser(role models.UserRole) models.CurrentUser {
	created, err := s.userRepository.CreateUser(s.ctx, testAdminUser(role))
	s.Require().NoError(err)
	return created
}

func testAdminUser(role models.UserRole) models.CurrentUser {
	id := uuid.New()
	return models.CurrentUser{
		ID:           id,
		Email:        id.String() + "@example.com",
		PasswordHash: "hash",
		FullName:     "Dmitry",
		FullSurname:  "Mukhachev",
		Username:     "user_" + id.String()[:8],
		Role:         role,
		CreatedAt:    time.Now().UTC().Truncate(time.Microsecond),
	}
}
