package call

import (
	"errors"
	"strings"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
)

func (s *ServiceSuite) TestGetAudioByUUIDSuccess() {
	callID := uuid.New()
	userID := uuid.New()
	content := newReadSeekCloser("audio")

	s.repository.EXPECT().GetByUUID(mock.Anything, callID, userID).
		Return(models.Call{
			ID:               callID,
			AudioPath:        "uploads/call.wav",
			OriginalFilename: "call.wav",
			MimeType:         "audio/wav",
			SizeBytes:        10,
		}, nil).
		Once()
	s.audioStorage.EXPECT().OpenReadSeeker(mock.Anything, "uploads/call.wav").Return(content, nil).Once()

	got, err := s.service.GetAudioByUUID(s.ctx, callID, userID)

	s.Require().NoError(err)
	s.Require().Equal("call.wav", got.OriginalFilename)
	s.Require().Equal("audio/wav", got.MimeType)
	s.Require().NotNil(got.ReadSeeker)
}

func (s *ServiceSuite) TestGetAudioByUUIDReturnsCallLookupError() {
	callID := uuid.New()
	userID := uuid.New()

	s.repository.EXPECT().GetByUUID(mock.Anything, callID, userID).
		Return(models.Call{}, models.ErrCallNotFound).
		Once()

	_, err := s.service.GetAudioByUUID(s.ctx, callID, userID)

	s.Require().ErrorIs(err, models.ErrCallNotFound)
}

func (s *ServiceSuite) TestGetAudioByUUIDReturnsOpenError() {
	callID := uuid.New()
	userID := uuid.New()
	openErr := errors.New("open failed")

	s.repository.EXPECT().GetByUUID(mock.Anything, callID, userID).
		Return(models.Call{ID: callID, AudioPath: "uploads/call.wav"}, nil).
		Once()
	s.audioStorage.EXPECT().OpenReadSeeker(mock.Anything, "uploads/call.wav").Return(nil, openErr).Once()

	_, err := s.service.GetAudioByUUID(s.ctx, callID, userID)

	s.Require().ErrorIs(err, openErr)
}

type readSeekCloser struct {
	*strings.Reader
}

func newReadSeekCloser(content string) *readSeekCloser {
	return &readSeekCloser{Reader: strings.NewReader(content)}
}

func (r *readSeekCloser) Close() error {
	return nil
}
