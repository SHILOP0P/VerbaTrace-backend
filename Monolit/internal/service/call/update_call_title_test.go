package call

import (
	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
)

func (s *ServiceSuite) TestUpdateCallTitleTrimsTitleAndUpdates() {
	callID := uuid.New()
	userID := uuid.New()

	s.repository.EXPECT().
		UpdateCallTitle(mock.Anything, callID, userID, "new title").
		Return(models.Call{ID: callID, Title: "new title"}, nil).
		Once()

	got, err := s.service.UpdateCallTitle(s.ctx, callID, userID, "  new title  ")

	s.Require().NoError(err)
	s.Require().Equal("new title", got.Title)
}

func (s *ServiceSuite) TestUpdateCallTitleRejectsEmptyTitle() {
	_, err := s.service.UpdateCallTitle(s.ctx, uuid.New(), uuid.New(), "   ")

	s.Require().ErrorIs(err, models.ErrInvalidCallTitle)
}
