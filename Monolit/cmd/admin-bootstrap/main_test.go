//go:build integration

package main

import (
	"context"
	"testing"
	"time"

	"calllens/monolit/internal/models"
	refreshRepo "calllens/monolit/internal/repository/refresh_session"
	"calllens/monolit/internal/repository/repositorytest"
	userRepo "calllens/monolit/internal/repository/user"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestBootstrapSuperAdminPromotesOnceAndInvalidatesAccess(t *testing.T) {
	db := repositorytest.OpenTestDB(t)
	repositorytest.RunMigrations(t, db)
	repositorytest.TruncateTables(t, db)

	ctx := context.Background()
	users := userRepo.NewUserRepository(db)
	sessions := refreshRepo.NewRepository(db)
	now := time.Now().UTC().Truncate(time.Microsecond)
	user := models.CurrentUser{
		ID:           uuid.New(),
		Email:        "owner@example.com",
		PasswordHash: "hash",
		FullName:     "Dmitry",
		FullSurname:  "Mukhachev",
		Username:     "owner",
		Role:         models.UserRoleUser,
		CreatedAt:    now,
	}
	_, err := users.CreateUser(ctx, user)
	require.NoError(t, err)
	sessionID := uuid.New()
	_, err = sessions.CreateRefreshSession(ctx, models.RefreshSession{
		ID:               sessionID,
		UserID:           user.ID,
		RefreshTokenHash: uuid.NewString(),
		AccessVersion:    1,
		CreatedAt:        now,
		ExpiresAt:        now.Add(time.Hour),
	})
	require.NoError(t, err)

	userID, changed, err := bootstrapSuperAdmin(ctx, db, " OWNER@example.com ")
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, user.ID, userID)

	promoted, err := users.GetUserByUUID(ctx, user.ID)
	require.NoError(t, err)
	require.Equal(t, models.UserRoleSuperAdmin, promoted.Role)
	updatedSession, err := sessions.GetRefreshSessionByUUID(ctx, sessionID)
	require.NoError(t, err)
	require.Equal(t, int64(2), updatedSession.AccessVersion)

	_, changed, err = bootstrapSuperAdmin(ctx, db, user.Email)
	require.NoError(t, err)
	require.False(t, changed)
}
