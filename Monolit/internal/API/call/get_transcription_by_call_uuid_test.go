package call

import (
	"errors"
	"net/http"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
)

func (s *APISuite) TestGetTranscriptionByCallUUIDSuccess() {
	userID := uuid.New()
	callID := uuid.New()
	text := "hello"
	s.service.EXPECT().GetTranscriptionByCallUUID(mock.Anything, callID, userID).
		Return(models.Transcription{ID: uuid.New(), CallUUID: callID, Text: &text}, nil).Once()
	rec, req := s.request(http.MethodGet, "/", "", userID, map[string]string{"uuid": callID.String()})
	s.api.GetTranscriptionByCallUUID(rec, req)
	s.Equal(http.StatusOK, rec.Code)
}

func (s *APISuite) TestGetTranscriptionByCallUUIDValidationAndErrors() {
	rec, req := s.request(http.MethodGet, "/", "", uuid.Nil, nil)
	s.api.GetTranscriptionByCallUUID(rec, req)
	s.Equal(http.StatusUnauthorized, rec.Code)

	rec, req = s.request(http.MethodGet, "/", "", uuid.New(), map[string]string{"uuid": "invalid"})
	s.api.GetTranscriptionByCallUUID(rec, req)
	s.Equal(http.StatusBadRequest, rec.Code)

	for _, tt := range []struct {
		err  error
		code int
	}{
		{models.ErrCallNotFound, http.StatusNotFound},
		{models.ErrTranscriptionNotFound, http.StatusNotFound},
		{errors.New("db"), http.StatusInternalServerError},
	} {
		userID := uuid.New()
		callID := uuid.New()
		s.service.EXPECT().GetTranscriptionByCallUUID(mock.Anything, callID, userID).
			Return(models.Transcription{}, tt.err).Once()
		rec, req = s.request(http.MethodGet, "/", "", userID, map[string]string{"uuid": callID.String()})
		s.api.GetTranscriptionByCallUUID(rec, req)
		s.Equal(tt.code, rec.Code)
	}
}
