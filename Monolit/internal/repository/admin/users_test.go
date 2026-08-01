//go:build integration

package admin

import (
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func (s *RepositorySuite) TestChangeAdminUserRoleInvalidatesAccessAndAudits() {
	actor := s.createUser(models.UserRoleAdmin)
	target := s.createUser(models.UserRoleUser)
	sessionID := uuid.New()
	_, err := s.db.ExecContext(s.ctx, `
		INSERT INTO refresh_sessions (session_uuid,user_uuid,refresh_token_hash,access_version,created_at,expires_at)
		VALUES ($1,$2,$3,1,$4,$5)
	`, sessionID, target.ID, uuid.NewString(), time.Now().UTC(), time.Now().UTC().Add(time.Hour))
	s.Require().NoError(err)
	reason := "support promotion"
	before := models.UserRoleUser
	updated, err := s.repository.ChangeAdminUserRole(s.ctx, models.ChangeAdminUserRoleInput{
		ActorUserUUID: actor.ID, TargetUserUUID: target.ID, ExpectedRole: before, Role: models.UserRoleHelper,
		Metadata: models.AdminMutationMetadata{Reason: reason},
	})
	s.Require().NoError(err)
	s.Require().Equal(models.UserRoleHelper, updated.Role)
	var version int64
	s.Require().NoError(s.db.QueryRowContext(s.ctx, `SELECT access_version FROM refresh_sessions WHERE session_uuid=$1`, sessionID).Scan(&version))
	s.Require().Equal(int64(2), version)
	var action string
	var beforeJSON, afterJSON []byte
	s.Require().NoError(s.db.QueryRowContext(s.ctx, `SELECT action,before_data,after_data FROM admin_audit_logs`).Scan(&action, &beforeJSON, &afterJSON))
	s.Require().Equal("user.role_changed", action)
	s.Require().JSONEq(`{"role":"user"}`, string(beforeJSON))
	s.Require().JSONEq(`{"role":"helper"}`, string(afterJSON))
}

func (s *RepositorySuite) TestAdminCannotRevokeAdminSession() {
	actor := s.createUser(models.UserRoleAdmin)
	target := s.createUser(models.UserRoleAdmin)
	reason := "incident"
	err := s.repository.RevokeAllAdminUserSessions(s.ctx, models.AdminSessionMutationInput{
		ActorUserUUID: actor.ID, TargetUserUUID: target.ID, Metadata: models.AdminMutationMetadata{Reason: reason},
	})
	s.Require().ErrorIs(err, models.ErrAdminSessionManagementForbidden)
}

func (s *RepositorySuite) TestUpdateAdminUserProfileAuditsChanges() {
	actor := s.createUser(models.UserRoleAdmin)
	target := s.createUser(models.UserRoleUser)
	name := "Updated"
	surname := "Profile"
	username := "@updated_profile"
	post := "Support"
	phone := "+79990000000"
	timezone := "Europe/Moscow"

	updated, err := s.repository.UpdateAdminUserProfile(s.ctx, models.UpdateAdminUserProfileInput{
		ActorUserUUID: actor.ID, TargetUserUUID: target.ID,
		FullName: &name, FullSurname: &surname, Username: &username, Post: &post, Phone: &phone, Timezone: &timezone,
		Metadata: models.AdminMutationMetadata{Reason: "profile correction"},
	})
	s.Require().NoError(err)
	s.Require().Equal(name, updated.FullName)
	s.Require().Equal(surname, updated.FullSurname)
	s.Require().Equal(username, updated.Username)
	s.Require().Equal(post, *updated.Post)
	s.Require().Equal(phone, *updated.Phone)
	s.Require().Equal(timezone, *updated.Timezone)

	var action string
	s.Require().NoError(s.db.QueryRowContext(s.ctx, `SELECT action FROM admin_audit_logs`).Scan(&action))
	s.Require().Equal("user.profile_updated", action)
}
