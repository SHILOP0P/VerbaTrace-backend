package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"verbatrace/monolit/internal/auth/password"
	"verbatrace/monolit/internal/models"
	repositoryMocks "verbatrace/monolit/internal/repository/mocks"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
)

func TestUpdateUsername(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()

	tests := []struct {
		name      string
		input     models.UpdateUsernameInput
		setup     func(*repositoryMocks.UserRepository)
		wantError error
	}{
		{name: "nil user", input: models.UpdateUsernameInput{Username: "@valid_name"}, wantError: models.ErrInvalidUserInput},
		{name: "invalid username", input: models.UpdateUsernameInput{UserUUID: userID, Username: "x"}, wantError: models.ErrInvalidUserInput},
		{
			name:  "taken",
			input: models.UpdateUsernameInput{UserUUID: userID, Username: "Taken Name"},
			setup: func(repo *repositoryMocks.UserRepository) {
				repo.EXPECT().GetUserByUsername(mock.Anything, "@taken_name").
					Return(models.CurrentUser{ID: uuid.New()}, nil).Once()
			},
			wantError: models.ErrUserAlreadyExists,
		},
		{
			name:  "lookup error",
			input: models.UpdateUsernameInput{UserUUID: userID, Username: "valid name"},
			setup: func(repo *repositoryMocks.UserRepository) {
				repo.EXPECT().GetUserByUsername(mock.Anything, "@valid_name").
					Return(models.CurrentUser{}, errors.New("db")).Once()
			},
		},
		{
			name:  "success",
			input: models.UpdateUsernameInput{UserUUID: userID, Username: "Valid Name"},
			setup: func(repo *repositoryMocks.UserRepository) {
				repo.EXPECT().GetUserByUsername(mock.Anything, "@valid_name").
					Return(models.CurrentUser{}, models.ErrUserNotFound).Once()
				repo.EXPECT().UpdateUsername(mock.Anything, models.UpdateUsernameInput{
					UserUUID: userID, Username: "@valid_name",
				}).Return(models.CurrentUser{ID: userID, Username: "@valid_name"}, nil).Once()
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := repositoryMocks.NewUserRepository(t)
			if tt.setup != nil {
				tt.setup(repo)
			}
			service := NewService(repo, nil, "pepper", "jwt", 0, "refresh", 0, nil)
			got, err := service.UpdateUsername(ctx, tt.input)
			if tt.wantError != nil {
				if !errors.Is(err, tt.wantError) {
					t.Fatalf("error = %v, want %v", err, tt.wantError)
				}
				return
			}
			if tt.name == "lookup error" {
				if err == nil {
					t.Fatal("expected lookup error")
				}
				return
			}
			if err != nil || got.Username != "@valid_name" {
				t.Fatalf("UpdateUsername = %+v, %v", got, err)
			}
		})
	}
}

func TestGetUserByUsernameAndHelpers(t *testing.T) {
	repo := repositoryMocks.NewUserRepository(t)
	service := NewService(repo, nil, "pepper", "jwt", 0, "refresh", 0, nil)
	if _, err := service.GetUserByUsername(context.Background(), "x"); !errors.Is(err, models.ErrInvalidUserInput) {
		t.Fatalf("invalid username error = %v", err)
	}
	repo.EXPECT().GetUserByUsername(mock.Anything, "@valid_name").
		Return(models.CurrentUser{Username: "@valid_name"}, nil).Once()
	got, err := service.GetUserByUsername(context.Background(), "Valid Name")
	if err != nil || got.Username != "@valid_name" {
		t.Fatalf("GetUserByUsername = %+v, %v", got, err)
	}

	value := "  developer  "
	if got := normalizeOptionalString(&value); got == nil || *got != "developer" {
		t.Fatalf("normalizeOptionalString = %v", got)
	}
	blank := " "
	if normalizeOptionalString(&blank) != nil || normalizeOptionalString(nil) != nil {
		t.Fatal("blank optional strings must normalize to nil")
	}

	service.SetBillingRepository(nil)
	if service.billingRepository != nil {
		t.Fatal("billing repository should be nil")
	}
}

func TestUsernameForNewUserGeneratedAndCollisions(t *testing.T) {
	input := models.CreateUserInput{
		Email: "user@example.com", FullName: "Dmitry", FullSurname: "Mukhachev",
	}

	repo := repositoryMocks.NewUserRepository(t)
	repo.On("GetUserByUsername", mock.Anything, mock.MatchedBy(func(value string) bool {
		return len(value) > 7 && value[0] == '@'
	})).Return(models.CurrentUser{}, models.ErrUserNotFound).Once()
	service := NewService(repo, nil, "pepper", "jwt", time.Minute, "refresh", time.Hour, nil)
	got, err := service.usernameForNewUser(context.Background(), input)
	if err != nil || got == "" {
		t.Fatalf("usernameForNewUser = %q, %v", got, err)
	}

	repo = repositoryMocks.NewUserRepository(t)
	repo.On("GetUserByUsername", mock.Anything, mock.Anything).
		Return(models.CurrentUser{ID: uuid.New()}, nil).Times(10)
	service = NewService(repo, nil, "pepper", "jwt", time.Minute, "refresh", time.Hour, nil)
	if _, err := service.usernameForNewUser(context.Background(), input); !errors.Is(err, models.ErrUserAlreadyExists) {
		t.Fatalf("collision error = %v", err)
	}
}

func TestRegisterAdditionalErrors(t *testing.T) {
	valid := models.CreateUserInput{
		Email: "user@example.com", Password: "password123",
		FullName: "Dmitry", FullSurname: "Mukhachev", Username: "valid_name",
	}

	t.Run("email lookup error", func(t *testing.T) {
		repo := repositoryMocks.NewUserRepository(t)
		repo.EXPECT().GetUserByEmail(mock.Anything, valid.Email).
			Return(models.CurrentUser{}, errors.New("db")).Once()
		service := NewService(repo, nil, "pepper", "jwt", time.Minute, "refresh", time.Hour, nil)
		if _, err := service.Register(context.Background(), valid); err == nil {
			t.Fatal("expected lookup error")
		}
	})

	t.Run("invalid explicit username", func(t *testing.T) {
		repo := repositoryMocks.NewUserRepository(t)
		repo.EXPECT().GetUserByEmail(mock.Anything, valid.Email).
			Return(models.CurrentUser{}, models.ErrUserNotFound).Once()
		input := valid
		input.Username = "x"
		service := NewService(repo, nil, "pepper", "jwt", time.Minute, "refresh", time.Hour, nil)
		if _, err := service.Register(context.Background(), input); !errors.Is(err, models.ErrInvalidUserInput) {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("create error", func(t *testing.T) {
		repo := repositoryMocks.NewUserRepository(t)
		repo.EXPECT().GetUserByEmail(mock.Anything, valid.Email).
			Return(models.CurrentUser{}, models.ErrUserNotFound).Once()
		repo.EXPECT().GetUserByUsername(mock.Anything, "@valid_name").
			Return(models.CurrentUser{}, models.ErrUserNotFound).Once()
		repo.EXPECT().CreateUser(mock.Anything, mock.Anything).
			Return(models.CurrentUser{}, errors.New("create")).Once()
		service := NewService(repo, nil, "pepper", "jwt", time.Minute, "refresh", time.Hour, nil)
		if _, err := service.Register(context.Background(), valid); err == nil {
			t.Fatal("expected create error")
		}
	})
}

func TestLoginRefreshHashError(t *testing.T) {
	repo := repositoryMocks.NewUserRepository(t)
	sessionRepo := repositoryMocks.NewRefreshSessionRepository(t)
	hash, err := password.Hash("password123", "pepper")
	if err != nil {
		t.Fatal(err)
	}
	repo.EXPECT().GetUserByEmail(mock.Anything, "user@example.com").
		Return(models.CurrentUser{
			ID: uuid.New(), Email: "user@example.com", PasswordHash: hash, Role: models.UserRoleUser,
		}, nil).Once()
	service := NewService(repo, sessionRepo, "pepper", "jwt", time.Minute, "", time.Hour, nil)
	if _, _, _, err := service.Login(context.Background(), models.LoginInput{
		Email: "user@example.com", Password: "password123",
	}); err == nil {
		t.Fatal("expected empty refresh secret error")
	}
}
