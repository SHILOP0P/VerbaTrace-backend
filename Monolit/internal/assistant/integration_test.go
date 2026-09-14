//go:build integration

package assistant

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"verbatrace/monolit/internal/models"
	"verbatrace/monolit/internal/repository/repositorytest"
	userrepo "verbatrace/monolit/internal/repository/user"
)

func TestPersonalAssistantScopeAndMultipleReplies(t *testing.T) {
	db := repositorytest.OpenTestDB(t)
	repositorytest.RunMigrations(t, db)
	repositorytest.TruncateTables(t, db)
	ctx := context.Background()
	id := uuid.New()
	_, err := userrepo.NewUserRepository(db).CreateUser(ctx, models.CurrentUser{ID: id, Email: id.String() + "@example.com", PasswordHash: "hash", FullName: "Test", FullSurname: "User", Username: "@" + id.String()[:8], Role: models.UserRoleUser, CreatedAt: time.Now().UTC()})
	require.NoError(t, err)

	s := NewService(db, nil, nil)
	cap, err := s.Capabilities(ctx, id, uuid.Nil)
	require.NoError(t, err)
	require.True(t, cap.SearchEnabled)
	require.False(t, cap.ChatEnabled)
	_, err = s.CreateChat(ctx, id, uuid.Nil, "Test", "auto")
	require.ErrorIs(t, err, ErrForbidden)
	_, err = db.ExecContext(ctx, `UPDATE subscriptions SET plan_uuid=(SELECT plan_uuid FROM plans WHERE code='personal_pro') WHERE user_uuid=$1`, id)
	require.NoError(t, err)
	chat, err := s.CreateChat(ctx, id, uuid.Nil, "Test", "auto")
	require.NoError(t, err)
	for i := 0; i < 2; i++ {
		in := models.CreateAssistantMessageInput{UserUUID: id, ChatUUID: chat.ID, Text: "нет подтверждений", ClientMessageID: uuid.NewString(), IdempotencyKey: uuid.NewString(), ResponseDetail: "auto"}
		run, err := s.CreateMessage(ctx, in)
		require.NoError(t, err)
		require.Equal(t, "queued", run.State)
		require.NoError(t, s.recoverRuns(ctx, 5))
		run, err = s.GetRun(ctx, id, run.ID)
		require.NoError(t, err)
		require.Equal(t, "completed", run.State)
		repeated, err := s.CreateMessage(ctx, in)
		require.NoError(t, err)
		require.Equal(t, run.ID, repeated.ID)
		in.FolderIDs = []uuid.UUID{uuid.New()}
		_, err = s.CreateMessage(ctx, in)
		require.ErrorIs(t, err, ErrInvalidInput)
	}
	messages, err := s.ListMessages(ctx, id, chat.ID)
	require.NoError(t, err)
	require.Len(t, messages, 4)
	manifest, _ := json.Marshal([]models.ContentSearchItem{{CallUUID: uuid.New(), Revision: 1}})
	_, err = db.ExecContext(ctx, `UPDATE assistant_runs SET source_manifest_json=$2 WHERE assistant_chat_uuid=$1`, chat.ID, manifest)
	require.NoError(t, err)
	hidden, err := s.ListMessages(ctx, id, chat.ID)
	require.NoError(t, err)
	for _, m := range hidden {
		if m.Role == "assistant" {
			require.Equal(t, "unavailable", m.Status)
			require.Empty(t, m.Sources)
			require.Empty(t, m.Blocks)
		}
	}
	_, err = s.ListMessages(ctx, uuid.New(), chat.ID)
	require.ErrorIs(t, err, ErrNotFound)
	_, err = db.ExecContext(ctx, `UPDATE subscriptions SET plan_uuid=(SELECT plan_uuid FROM plans WHERE code='personal_start') WHERE user_uuid=$1`, id)
	require.NoError(t, err)
	_, err = s.ListMessages(ctx, id, chat.ID)
	require.ErrorIs(t, err, ErrForbidden)
}
