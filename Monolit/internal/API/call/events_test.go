package call

import (
	"context"
	"net/http"
	"strings"
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
)

func (s *APISuite) TestEventsWritesCurrentTerminalStatus() {
	callID := uuid.New()
	userID := uuid.New()

	s.service.On("GetByUUID", mock.Anything, callID, userID).
		Return(models.Call{ID: callID, Status: models.CallStatusAnalyzed, CreatedAt: time.Now().UTC()}, nil).
		Once()

	rec, req := s.request(http.MethodGet, "/api/v1/calls/"+callID.String()+"/events", "", userID, map[string]string{"uuid": callID.String()})

	s.api.Events(rec, req)

	s.Require().Equal(http.StatusOK, rec.Code)
	s.Require().Equal("text/event-stream", rec.Header().Get("Content-Type"))
	s.Require().Contains(rec.Body.String(), "event: status")
	s.Require().Contains(rec.Body.String(), `"status":"analyzed"`)
	s.Require().Contains(rec.Body.String(), `"terminal":true`)
	s.Require().True(rec.Flushed)
}

func (s *APISuite) TestEventsStreamsStatusChangesUntilTerminalStatus() {
	callID := uuid.New()
	userID := uuid.New()
	originalPollInterval := callEventsPollInterval
	callEventsPollInterval = time.Millisecond
	defer func() {
		callEventsPollInterval = originalPollInterval
	}()

	statuses := []models.CallStatus{
		models.CallStatusProcessing,
		models.CallStatusAnalyzed,
	}
	nextStatus := 0

	s.service.On("GetByUUID", mock.Anything, callID, userID).
		Return(func(ctx context.Context, id uuid.UUID, requestUserID uuid.UUID) models.Call {
			status := statuses[nextStatus]
			if nextStatus < len(statuses)-1 {
				nextStatus++
			}

			return models.Call{ID: id, Status: status, CreatedAt: time.Now().UTC()}
		}, nil).
		Times(2)

	rec, req := s.request(http.MethodGet, "/api/v1/calls/"+callID.String()+"/events", "", userID, map[string]string{"uuid": callID.String()})

	s.api.Events(rec, req)

	body := rec.Body.String()
	s.Require().Equal(http.StatusOK, rec.Code)
	s.Require().Equal(2, strings.Count(body, "event: status"))
	s.Require().Contains(body, `"status":"processing"`)
	s.Require().Contains(body, `"status":"analyzed"`)
	s.Require().Contains(body, `"terminal":false`)
	s.Require().Contains(body, `"terminal":true`)
}
