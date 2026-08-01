package department

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/httpserver/middleware"
	serviceMocks "verbatrace/monolit/internal/service/mocks"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"
)

type APISuite struct {
	suite.Suite
	ctx     context.Context
	service *serviceMocks.DepartmentService
	api     *Handler
}

func (s *APISuite) SetupTest() {
	s.ctx = context.Background()
	s.service = serviceMocks.NewDepartmentService(s.T())
	s.api = NewDepartmentHandler(s.service)
}

func TestAPISuite(t *testing.T) {
	suite.Run(t, new(APISuite))
}

func (s *APISuite) request(method string, path string, body string, userID uuid.UUID, params map[string]string) (*httptest.ResponseRecorder, *http.Request) {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req = req.WithContext(s.ctx)

	if userID != uuid.Nil {
		req = req.WithContext(middleware.ContextWithUserID(req.Context(), userID))
	}

	if len(params) > 0 {
		routeCtx := chi.NewRouteContext()
		for key, value := range params {
			routeCtx.URLParams.Add(key, value)
		}
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx))
	}

	return httptest.NewRecorder(), req
}

func (s *APISuite) requireErrorCode(rec *httptest.ResponseRecorder, expectedCode string) {
	var resp response.ErrorResponse
	s.Require().NoError(json.Unmarshal(rec.Body.Bytes(), &resp))
	s.Require().Equal(expectedCode, resp.Error.Code)
}
