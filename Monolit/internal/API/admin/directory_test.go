package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"verbatrace/monolit/internal/models"
)

// Directory metadata is intentionally available through the admin read
// permissions. Business records still use support-access checks on their own
// endpoints, but hiding the directory would make it impossible to identify a
// support-access subject in the first place.
func TestListUsersDoesNotRequireSupportAccess(t *testing.T) {
	h := NewHandler(&adminServiceStub{})
	h.SetSupportAccessAuthorizer(denySupportAccessAuthorizer{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users?limit=50&offset=0", nil)
	rec := httptest.NewRecorder()

	h.ListUsers(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
}

func TestListCompaniesDoesNotRequireSupportAccess(t *testing.T) {
	h := NewHandler(&adminServiceStub{})
	h.SetSupportAccessAuthorizer(denySupportAccessAuthorizer{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/companies?limit=50&offset=0", nil)
	rec := httptest.NewRecorder()

	h.ListCompanies(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
}

type denySupportAccessAuthorizer struct{}

func (denySupportAccessAuthorizer) AuthorizedSubjects(context.Context, uuid.UUID, string) ([]uuid.UUID, []uuid.UUID, error) {
	return nil, nil, models.ErrForbidden
}
func (denySupportAccessAuthorizer) AuthorizeUser(context.Context, uuid.UUID, uuid.UUID, string, string) error {
	return models.ErrForbidden
}
func (denySupportAccessAuthorizer) AuthorizeCompany(context.Context, uuid.UUID, uuid.UUID, string, string) error {
	return models.ErrForbidden
}
func (denySupportAccessAuthorizer) AuthorizeCall(context.Context, uuid.UUID, uuid.UUID, string, string) error {
	return models.ErrForbidden
}
