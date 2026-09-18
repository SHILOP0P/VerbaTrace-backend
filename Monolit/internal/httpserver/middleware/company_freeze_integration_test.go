//go:build integration

package middleware_test

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"

	"verbatrace/monolit/internal/httpserver/middleware"
	"verbatrace/monolit/internal/repository/repositorytest"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// The guard sits in the per-route chain rather than on the router, because chi
// runs router middleware before it matches and the guard needs the pattern and
// the path parameters. This test would fail the moment somebody moves it.
func TestCompanyFreezeBlocksMutationsAndLetsReadingThrough(t *testing.T) {
	db := repositorytest.OpenTestDB(t)
	repositorytest.RunMigrations(t, db)
	repositorytest.TruncateTables(t, db)
	ctx := context.Background()
	guard := middleware.CompanyFreeze(db)

	ownerID := repositorytest.CreateUser(t, db)
	frozen := seedCompany(t, db, ownerID, "frozen")
	active := seedCompany(t, db, ownerID, "active")
	_, err := db.ExecContext(ctx, `UPDATE companies SET lifecycle_state='frozen', freeze_reason='downgrade' WHERE company_uuid=$1`, frozen)
	require.NoError(t, err)

	frozenCall := seedCall(t, db, ownerID, uuid.NullUUID{UUID: frozen, Valid: true})
	personalCall := seedCall(t, db, ownerID, uuid.NullUUID{})

	router := chi.NewRouter()
	reached := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }
	router.Route("/api/v1", func(r chi.Router) {
		r.With(guard).Get("/companies/{uuid}/departments", reached)
		r.With(guard).Post("/companies/{uuid}/departments", reached)
		r.With(guard).Post("/companies/{uuid}/leave", reached)
		r.With(guard).Patch("/calls/{uuid}", reached)
		r.With(guard).Post("/calls/{uuid}/reports", reached)
	})

	call := func(method, path string) int {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(method, path, nil))
		return recorder.Code
	}

	// Reading a frozen company works exactly as reading an active one.
	require.Equal(t, http.StatusNoContent, call(http.MethodGet, "/api/v1/companies/"+frozen.String()+"/departments"))
	// Changing it does not.
	require.Equal(t, http.StatusConflict, call(http.MethodPost, "/api/v1/companies/"+frozen.String()+"/departments"))
	// An active company is untouched by the guard.
	require.Equal(t, http.StatusNoContent, call(http.MethodPost, "/api/v1/companies/"+active.String()+"/departments"))
	// Leaving is on the allow list: a member must never be trapped.
	require.Equal(t, http.StatusNoContent, call(http.MethodPost, "/api/v1/companies/"+frozen.String()+"/leave"))

	// The company is found through the entity too, not only from the path.
	require.Equal(t, http.StatusConflict, call(http.MethodPatch, "/api/v1/calls/"+frozenCall.String()))
	// Reports keep working, which is the whole point of freezing rather than
	// deleting.
	require.Equal(t, http.StatusNoContent, call(http.MethodPost, "/api/v1/calls/"+frozenCall.String()+"/reports"))
	// A personal call belongs to no company and is never blocked.
	require.Equal(t, http.StatusNoContent, call(http.MethodPatch, "/api/v1/calls/"+personalCall.String()))
}

// Instruction routes carry the instruction rather than the company in the
// path, so the guard has to find the company through the instruction.
func TestCompanyFreezeBlocksInstructionChanges(t *testing.T) {
	db := repositorytest.OpenTestDB(t)
	repositorytest.RunMigrations(t, db)
	repositorytest.TruncateTables(t, db)
	guard := middleware.CompanyFreeze(db)

	ownerID := repositorytest.CreateUser(t, db)
	frozen := seedCompany(t, db, ownerID, "frozen")
	active := seedCompany(t, db, ownerID, "active")
	_, err := db.ExecContext(context.Background(), `UPDATE companies SET lifecycle_state='frozen', freeze_reason='downgrade' WHERE company_uuid=$1`, frozen)
	require.NoError(t, err)
	frozenInstruction := seedInstruction(t, db, ownerID, uuid.NullUUID{UUID: frozen, Valid: true})
	activeInstruction := seedInstruction(t, db, ownerID, uuid.NullUUID{UUID: active, Valid: true})
	personalInstruction := seedInstruction(t, db, ownerID, uuid.NullUUID{})

	router := chi.NewRouter()
	reached := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }
	router.Route("/api/v1", func(r chi.Router) {
		r.With(guard).Get("/instructions/{uuid}", reached)
		r.With(guard).Patch("/instructions/{uuid}", reached)
		r.With(guard).Put("/instructions/{uuid}/file", reached)
		r.With(guard).Delete("/instructions/{uuid}", reached)
	})
	call := func(method, path string) int {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(method, path, nil))
		return recorder.Code
	}

	require.Equal(t, http.StatusNoContent, call(http.MethodGet, "/api/v1/instructions/"+frozenInstruction.String()))
	for _, request := range []struct{ method, suffix string }{
		{http.MethodPatch, ""}, {http.MethodPut, "/file"}, {http.MethodDelete, ""},
	} {
		require.Equal(t, http.StatusConflict, call(request.method, "/api/v1/instructions/"+frozenInstruction.String()+request.suffix), request.method+request.suffix)
		require.Equal(t, http.StatusNoContent, call(request.method, "/api/v1/instructions/"+activeInstruction.String()+request.suffix), request.method+request.suffix)
		require.Equal(t, http.StatusNoContent, call(request.method, "/api/v1/instructions/"+personalInstruction.String()+request.suffix), request.method+request.suffix)
	}
}

// Spec section 9: every changing route added for scorecards, call employees,
// analytics, growth areas, delivery and the CRM note is listed here with what
// the guard must do with it in a frozen company. A new route belongs in this
// list, or it may slip past the freeze unnoticed.
func TestCompanyFreezeCoversTheScorecardAndAnalyticsRoutes(t *testing.T) {
	db := repositorytest.OpenTestDB(t)
	repositorytest.RunMigrations(t, db)
	repositorytest.TruncateTables(t, db)
	ctx := context.Background()
	guard := middleware.CompanyFreeze(db)

	ownerID := repositorytest.CreateUser(t, db)
	frozen := seedCompany(t, db, ownerID, "frozen")
	_, err := db.ExecContext(ctx, `UPDATE companies SET lifecycle_state='frozen', freeze_reason='downgrade' WHERE company_uuid=$1`, frozen)
	require.NoError(t, err)
	frozenCompany := uuid.NullUUID{UUID: frozen, Valid: true}
	call := seedCall(t, db, ownerID, frozenCompany)
	personalCall := seedCall(t, db, ownerID, uuid.NullUUID{})
	instruction := seedInstruction(t, db, ownerID, frozenCompany)
	personalInstruction := seedInstruction(t, db, ownerID, uuid.NullUUID{})
	area := seedGrowthArea(t, db, ownerID, frozenCompany)
	personalArea := seedGrowthArea(t, db, ownerID, uuid.NullUUID{})
	connection := seedConnection(t, db, ownerID, frozen)
	criterion := uuid.New().String()

	routes := []struct {
		method, pattern, path, personalPath string
	}{
		{http.MethodPut, "/calls/{uuid}/transcription/speakers", "/calls/" + call.String() + "/transcription/speakers", "/calls/" + personalCall.String() + "/transcription/speakers"},
		{http.MethodPut, "/calls/{uuid}/subjects", "/calls/" + call.String() + "/subjects", ""},
		{http.MethodPatch, "/companies/{uuid}/analytics-settings", "/companies/" + frozen.String() + "/analytics-settings", ""},
		{http.MethodPost, "/growth-areas/{area_uuid}/dismiss", "/growth-areas/" + area.String() + "/dismiss", "/growth-areas/" + personalArea.String() + "/dismiss"},
		{http.MethodPost, "/growth-areas/{area_uuid}/reopen", "/growth-areas/" + area.String() + "/reopen", "/growth-areas/" + personalArea.String() + "/reopen"},
		{http.MethodPut, "/integrations/{connection_uuid}/crm-notes", "/integrations/" + connection.String() + "/crm-notes", ""},
		{http.MethodPatch, "/instructions/{uuid}/scorecard", "/instructions/" + instruction.String() + "/scorecard", "/instructions/" + personalInstruction.String() + "/scorecard"},
		{http.MethodPost, "/instructions/{uuid}/scorecard/ensure", "/instructions/" + instruction.String() + "/scorecard/ensure", "/instructions/" + personalInstruction.String() + "/scorecard/ensure"},
		{http.MethodPost, "/instructions/{uuid}/scorecard/recompile", "/instructions/" + instruction.String() + "/scorecard/recompile", "/instructions/" + personalInstruction.String() + "/scorecard/recompile"},
		{http.MethodPost, "/instructions/{uuid}/scorecard/confirm", "/instructions/" + instruction.String() + "/scorecard/confirm", "/instructions/" + personalInstruction.String() + "/scorecard/confirm"},
		{http.MethodPost, "/instructions/{uuid}/scorecard/criteria/{criterion_key}/same-as", "/instructions/" + instruction.String() + "/scorecard/criteria/" + criterion + "/same-as", ""},
		{http.MethodPost, "/instructions/{uuid}/scorecard/criteria/{criterion_key}/split", "/instructions/" + instruction.String() + "/scorecard/criteria/" + criterion + "/split", ""},
	}
	// These belong to a person, not to a company, so a frozen company has no say.
	personalRoutes := []struct{ method, path string }{
		{http.MethodPatch, "/analytics/personal-settings"},
		{http.MethodPut, "/notification-subscriptions"},
		{http.MethodPost, "/auth/password-reset/request"},
		{http.MethodPost, "/auth/password-reset/confirm"},
	}

	router := chi.NewRouter()
	reached := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }
	router.Route("/api/v1", func(r chi.Router) {
		for _, route := range routes {
			r.With(guard).MethodFunc(route.method, route.pattern, reached)
		}
		for _, route := range personalRoutes {
			r.With(guard).MethodFunc(route.method, route.path, reached)
		}
	})
	serve := func(method, path string) int {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(method, "/api/v1"+path, nil))
		return recorder.Code
	}

	for _, route := range routes {
		require.Equal(t, http.StatusConflict, serve(route.method, route.path), route.method+" "+route.pattern)
		if route.personalPath != "" {
			require.Equal(t, http.StatusNoContent, serve(route.method, route.personalPath), "personal "+route.method+" "+route.pattern)
		}
	}
	for _, route := range personalRoutes {
		require.Equal(t, http.StatusNoContent, serve(route.method, route.path), route.method+" "+route.path)
	}
}

func seedGrowthArea(t *testing.T, db *sql.DB, userID uuid.UUID, companyID uuid.NullUUID) uuid.UUID {
	t.Helper()
	areaID := uuid.New()
	_, err := db.ExecContext(context.Background(), `INSERT INTO growth_areas (area_uuid, company_uuid, subject_user_uuid, title, description) VALUES ($1, $2, $3, 'Цена', 'Называет цену без выгоды')`, areaID, companyID, userID)
	require.NoError(t, err)
	return areaID
}

func seedConnection(t *testing.T, db *sql.DB, userID, companyID uuid.UUID) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	billingID, applicationID, connectionID := uuid.New(), uuid.New(), uuid.New()
	_, err := db.ExecContext(ctx, `INSERT INTO billing_accounts (billing_account_uuid, owner_type, company_uuid) VALUES ($1, 'company', $2)`, billingID, companyID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO developer_applications (application_uuid, owner_type, company_uuid, billing_account_uuid, name, environment, status)
		VALUES ($1, 'company', $2, $3, 'Freeze app', 'sandbox', 'active')`, applicationID, companyID, billingID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO integration_connections (connection_uuid, application_uuid, company_uuid, created_by_user_uuid, name, provider, status)
		VALUES ($1, $2, $3, $4, 'Bitrix24', 'bitrix24', 'draft')`, connectionID, applicationID, companyID, userID)
	require.NoError(t, err)
	return connectionID
}

func seedInstruction(t *testing.T, db *sql.DB, ownerID uuid.UUID, companyID uuid.NullUUID) uuid.UUID {
	t.Helper()
	instructionID := uuid.New()
	scope, userID := "company", uuid.NullUUID{}
	if !companyID.Valid {
		scope, userID = "personal", uuid.NullUUID{UUID: ownerID, Valid: true}
	}
	_, err := db.ExecContext(context.Background(), `
		INSERT INTO analysis_instructions (instruction_uuid, scope, user_uuid, company_uuid, title, original_filename, file_path, mime_type, size_bytes, content_sha256, sort_order, is_active, created_by_user_uuid, created_at, updated_at)
		VALUES ($1, $2, $3, $4, 'Стандарт', 'standard.md', 'standard.md', 'text/markdown', 1, 'hash', 0, true, $5, now(), now())
	`, instructionID, scope, userID, companyID, ownerID)
	require.NoError(t, err)

	return instructionID
}

func seedCompany(t *testing.T, db *sql.DB, ownerID uuid.UUID, name string) uuid.UUID {
	t.Helper()
	companyID := uuid.New()
	_, err := db.ExecContext(context.Background(),
		`INSERT INTO companies (company_uuid, name, tag, manager_user_uuid, created_at) VALUES ($1,$2,$3,$4,now())`,
		companyID, name, name+companyID.String()[:8], ownerID)
	require.NoError(t, err)
	repositorytest.InsertCompanyMember(t, db, companyID, ownerID, "company_manager", "active")

	return companyID
}

func seedCall(t *testing.T, db *sql.DB, uploaderID uuid.UUID, companyID uuid.NullUUID) uuid.UUID {
	t.Helper()
	callID := uuid.New()
	scope := "personal"
	if companyID.Valid {
		scope = "company"
	}
	_, err := db.ExecContext(context.Background(), `
		INSERT INTO calls (call_uuid, title, status, audio_path, original_filename, mime_type, size_bytes, uploaded_by_user_uuid, company_uuid, visibility_scope, created_at)
		VALUES ($1, 'call', 'analyzed', '/tmp/a.mp3', 'a.mp3', 'audio/mpeg', 1, $2, $3, $4, now())
	`, callID, uploaderID, companyID, scope)
	require.NoError(t, err)

	return callID
}
