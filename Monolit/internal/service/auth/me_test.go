package auth

import (
	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func (s *ServiceSuite) TestMeSuccess() {
	userID := uuid.New()

	s.userRepository.On("GetUserByUUID", s.ctx, userID).
		Return(models.CurrentUser{ID: userID, Email: "user@example.com"}, nil).
		Once()

	got, err := s.service.Me(s.ctx, userID)

	s.Require().NoError(err)
	s.Require().Equal(userID, got.ID)
}

func (s *ServiceSuite) TestMeReturnsRepositoryError() {
	userID := uuid.New()

	s.userRepository.On("GetUserByUUID", s.ctx, userID).
		Return(models.CurrentUser{}, models.ErrUserNotFound).
		Once()

	_, err := s.service.Me(s.ctx, userID)

	s.Require().ErrorIs(err, models.ErrUserNotFound)
}
