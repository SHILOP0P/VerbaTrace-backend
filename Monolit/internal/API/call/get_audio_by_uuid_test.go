package call

import (
	"errors"
	"net/http"
	"strings"

	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
)

func (s *APISuite) TestGetAudioByUUIDSuccess() {
	callID := uuid.New()
	userID := uuid.New()

	s.service.On("GetAudioByUUID", mock.Anything, callID, userID).
		Return(models.File{
			Content:          newReadSeekCloser("audio"),
			ReadSeeker:       newReadSeekCloser("audio"),
			OriginalFilename: "call.wav",
			MimeType:         "audio/wav",
			SizeBytes:        5,
		}, nil).
		Once()

	rec, req := s.request(http.MethodGet, "/api/v1/calls/"+callID.String()+"/audio", "", userID, map[string]string{"uuid": callID.String()})

	s.api.GetAudioByUUID(rec, req)

	s.Require().Equal(http.StatusOK, rec.Code)
	s.Require().Equal("audio/wav", rec.Header().Get("Content-Type"))
	s.Require().Equal("5", rec.Header().Get("Content-Length"))
	s.Require().Equal("bytes", rec.Header().Get("Accept-Ranges"))
	s.Require().Equal(`inline; filename=call.wav`, rec.Header().Get("Content-Disposition"))
	s.Require().Equal("audio", rec.Body.String())
}

func (s *APISuite) TestGetAudioByUUIDDefaultsContentType() {
	callID := uuid.New()
	userID := uuid.New()

	s.service.On("GetAudioByUUID", mock.Anything, callID, userID).
		Return(models.File{Content: newReadSeekCloser("audio"), ReadSeeker: newReadSeekCloser("audio"), OriginalFilename: "call.bin"}, nil).
		Once()

	rec, req := s.request(http.MethodGet, "/api/v1/calls/"+callID.String()+"/audio", "", userID, map[string]string{"uuid": callID.String()})

	s.api.GetAudioByUUID(rec, req)

	s.Require().Equal(http.StatusOK, rec.Code)
	s.Require().Equal("application/octet-stream", rec.Header().Get("Content-Type"))
}

func (s *APISuite) TestGetAudioByUUIDRangeRequest() {
	callID := uuid.New()
	userID := uuid.New()

	s.service.On("GetAudioByUUID", mock.Anything, callID, userID).
		Return(models.File{
			Content:          newReadSeekCloser("audio"),
			ReadSeeker:       newReadSeekCloser("audio"),
			OriginalFilename: `..\unsafe\call.wav`,
			MimeType:         "audio/wav",
			SizeBytes:        5,
		}, nil).
		Once()

	rec, req := s.request(http.MethodGet, "/api/v1/calls/"+callID.String()+"/audio", "", userID, map[string]string{"uuid": callID.String()})
	req.Header.Set("Range", "bytes=1-3")

	s.api.GetAudioByUUID(rec, req)

	s.Require().Equal(http.StatusPartialContent, rec.Code)
	s.Require().Equal("bytes 1-3/5", rec.Header().Get("Content-Range"))
	s.Require().Equal(`inline; filename=call.wav`, rec.Header().Get("Content-Disposition"))
	s.Require().Equal("udi", rec.Body.String())
}

func (s *APISuite) TestGetAudioByUUIDMapsCallNotFound() {
	callID := uuid.New()
	userID := uuid.New()

	s.service.On("GetAudioByUUID", mock.Anything, callID, userID).Return(models.File{}, models.ErrCallNotFound).Once()

	rec, req := s.request(http.MethodGet, "/api/v1/calls/"+callID.String()+"/audio", "", userID, map[string]string{"uuid": callID.String()})

	s.api.GetAudioByUUID(rec, req)

	s.Require().Equal(http.StatusNotFound, rec.Code)
	s.requireErrorCode(rec, response.CodeCallNotFound)
}

func (s *APISuite) TestGetAudioByUUIDMapsMissingFile() {
	callID := uuid.New()
	userID := uuid.New()

	s.service.On("GetAudioByUUID", mock.Anything, callID, userID).Return(models.File{}, models.ErrAudioFileNotFound).Once()

	rec, req := s.request(http.MethodGet, "/api/v1/calls/"+callID.String()+"/audio", "", userID, map[string]string{"uuid": callID.String()})

	s.api.GetAudioByUUID(rec, req)

	s.Require().Equal(http.StatusGone, rec.Code)
	s.requireErrorCode(rec, response.CodeAudioFileNotFound)
}

func (s *APISuite) TestGetAudioByUUIDMapsUnexpectedError() {
	callID := uuid.New()
	userID := uuid.New()

	s.service.On("GetAudioByUUID", mock.Anything, callID, userID).Return(models.File{}, errors.New("open failed")).Once()

	rec, req := s.request(http.MethodGet, "/api/v1/calls/"+callID.String()+"/audio", "", userID, map[string]string{"uuid": callID.String()})

	s.api.GetAudioByUUID(rec, req)

	s.Require().Equal(http.StatusInternalServerError, rec.Code)
	s.requireErrorCode(rec, response.CodeFailedToGetAudio)
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
