package call

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
)

func (s *APISuite) TestGetByUUIDValidationAndFallbackError() {
	rec, req := s.request(http.MethodGet, "/", "", uuid.Nil, nil)
	s.api.GetByUUID(rec, req)
	s.Equal(http.StatusUnauthorized, rec.Code)

	rec, req = s.request(http.MethodGet, "/", "", uuid.New(), map[string]string{"uuid": "bad"})
	s.api.GetByUUID(rec, req)
	s.Equal(http.StatusBadRequest, rec.Code)

	userID := uuid.New()
	callID := uuid.New()
	s.service.EXPECT().GetByUUID(mock.Anything, callID, userID).
		Return(models.Call{}, errors.New("db")).Once()
	rec, req = s.request(http.MethodGet, "/", "", userID, map[string]string{"uuid": callID.String()})
	s.api.GetByUUID(rec, req)
	s.Equal(http.StatusInternalServerError, rec.Code)
}

func (s *APISuite) TestEventsValidationAndServiceErrors() {
	rec, req := s.request(http.MethodGet, "/", "", uuid.Nil, nil)
	s.api.Events(rec, req)
	s.Equal(http.StatusUnauthorized, rec.Code)

	rec, req = s.request(http.MethodGet, "/", "", uuid.New(), map[string]string{"uuid": "bad"})
	s.api.Events(rec, req)
	s.Equal(http.StatusBadRequest, rec.Code)

	userID := uuid.New()
	callID := uuid.New()
	s.service.EXPECT().GetByUUID(mock.Anything, callID, userID).
		Return(models.Call{}, models.ErrCallNotFound).Once()
	rec, req = s.request(http.MethodGet, "/", "", userID, map[string]string{"uuid": callID.String()})
	s.api.Events(rec, req)
	s.Equal(http.StatusNotFound, rec.Code)

	s.service.EXPECT().GetByUUID(mock.Anything, callID, userID).
		Return(models.Call{}, errors.New("db")).Once()
	rec, req = s.request(http.MethodGet, "/", "", userID, map[string]string{"uuid": callID.String()})
	s.api.Events(rec, req)
	s.Equal(http.StatusInternalServerError, rec.Code)
}

func (s *APISuite) TestEventsRequiresFlusher() {
	userID := uuid.New()
	callID := uuid.New()
	_, req := s.request(http.MethodGet, "/", "", userID, map[string]string{"uuid": callID.String()})
	writer := &plainResponseWriter{header: make(http.Header)}
	s.api.Events(writer, req)
	s.Equal(http.StatusInternalServerError, writer.status)
}

func (s *APISuite) TestEventsWritesStreamErrorAfterPollingFailure() {
	callID := uuid.New()
	userID := uuid.New()
	original := callEventsPollInterval
	callEventsPollInterval = time.Millisecond
	defer func() { callEventsPollInterval = original }()

	s.service.EXPECT().GetByUUID(mock.Anything, callID, userID).
		Return(models.Call{ID: callID, Status: models.CallStatusProcessing}, nil).Once()
	s.service.EXPECT().GetByUUID(mock.Anything, callID, userID).
		Return(models.Call{}, errors.New("db")).Once()

	rec, req := s.request(http.MethodGet, "/", "", userID, map[string]string{"uuid": callID.String()})
	s.api.Events(rec, req)
	s.Contains(rec.Body.String(), "event: error")
}

func TestWriteCallStreamError(t *testing.T) {
	rec := httptest.NewRecorder()
	if err := writeCallStreamError(rec, rec, "failed"); err != nil {
		t.Fatalf("writeCallStreamError: %v", err)
	}
	if body := rec.Body.String(); body == "" {
		t.Fatal("empty stream error")
	}
}

type plainResponseWriter struct {
	header http.Header
	status int
}

func (w *plainResponseWriter) Header() http.Header { return w.header }
func (w *plainResponseWriter) Write(data []byte) (int, error) {
	return len(data), nil
}
func (w *plainResponseWriter) WriteHeader(statusCode int) { w.status = statusCode }
