package middleware

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/models"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// CompanyFreeze refuses to change anything inside a company that is frozen or on
// its way out.
//
// It lives here rather than in every service on purpose. The rule used to hold
// only where the code happened to ask for a subscription, and about forty
// mutations across a dozen packages simply did not ask: folders, privacy
// corrections, instructions, ownership, integrations. One guard in front of the
// routes cannot be forgotten by the next handler somebody adds, which is exactly
// why the earlier per-service attempt kept leaking.
//
// Reading is never blocked: a frozen company stays fully readable, and so do
// reports and analytics built on top of it.
func CompanyFreeze(db *sql.DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if db == nil || !changesData(r.Method) {
				next.ServeHTTP(w, r)
				return
			}
			if frozenCompanyAllows[routePattern(r)] {
				next.ServeHTTP(w, r)
				return
			}

			companyID, err := companyFromRequest(r.Context(), db, r)
			if err != nil {
				response.WriteError(w, http.StatusInternalServerError, response.CodeInternalServerError, "failed to resolve the company of the request")
				return
			}
			if companyID == uuid.Nil {
				// Either the path names no company, or the entity is personal. Both
				// are outside this rule.
				next.ServeHTTP(w, r)
				return
			}

			var state string
			err = db.QueryRowContext(r.Context(), `SELECT lifecycle_state FROM companies WHERE company_uuid=$1 AND deleted_at IS NULL`, companyID).Scan(&state)
			if errors.Is(err, sql.ErrNoRows) {
				// There is no such company, or it is already gone. Answering here
				// would turn every unrelated mistake into a company error, so the
				// handler gets to say what it actually means.
				next.ServeHTTP(w, r)
				return
			}
			if err != nil {
				response.WriteError(w, http.StatusInternalServerError, response.CodeInternalServerError, "failed to check company state")
				return
			}
			if models.CompanyLifecycleState(state) != models.CompanyLifecycleActive {
				response.WriteError(w, http.StatusConflict, response.CodeCompanyFrozen, "company is frozen")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// frozenCompanyAllows is the list the owner agreed on: what still works while a
// company is frozen. Everything here either takes data out rather than changing
// it, is how the freeze itself is undone, or frees resources instead of spending
// them.
var frozenCompanyAllows = map[string]bool{
	"/api/v1/companies/{uuid}/activate":        true,
	"/api/v1/companies/{uuid}/freeze":          true,
	"/api/v1/companies/{uuid}/cancel-deletion": true,
	"/api/v1/companies/{uuid}":                 true,
	// Leaving is the member's own decision and must never be trapped.
	"/api/v1/companies/{uuid}/leave": true,
	// Reports and analytics keep working, which includes building an export and
	// removing one that is no longer needed.
	"/api/v1/reports":               true,
	"/api/v1/reports/{report_uuid}": true,
	"/api/v1/calls/{uuid}/reports":  true,
	// The assistant is read-only in a frozen company, but deleting a chat is
	// housekeeping rather than business data.
	"/api/v1/assistant/chats/{chat_uuid}": true,
	// Stopping work that is already queued.
	"/api/v1/ingest-items/{ingest_item_uuid}/cancel": true,
	"/api/v1/calls/{uuid}/cancel-processing":         true,
}

// companyEntityLookups maps a path parameter to the query that answers which
// company the entity belongs to. They are tried in this order, so the most
// direct answer comes first.
var companyEntityLookups = []struct {
	param string
	query string
}{
	{"department_uuid", `SELECT company_uuid FROM departments WHERE department_uuid=$1`},
	{"folder_uuid", `SELECT company_uuid FROM call_folders WHERE folder_uuid=$1`},
	{"action_uuid", `SELECT company_uuid FROM call_actions WHERE action_uuid=$1`},
	{"review_uuid", `SELECT company_uuid FROM call_quality_reviews WHERE review_uuid=$1`},
	{"connection_uuid", `SELECT company_uuid FROM integration_connections WHERE connection_uuid=$1`},
	{"call_uuid", `SELECT company_uuid FROM calls WHERE call_uuid=$1`},
}

// companyFromRequest finds the company a request is about, from whatever the
// path gives it. A personal entity has no company and is never blocked.
func companyFromRequest(ctx context.Context, db *sql.DB, r *http.Request) (uuid.UUID, error) {
	if raw := chi.URLParam(r, "company_uuid"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			return uuid.Nil, nil
		}
		return id, nil
	}

	pattern := routePattern(r)

	// Under /companies/{uuid} the bare parameter is the company itself.
	if strings.HasPrefix(pattern, "/api/v1/companies/{uuid}") {
		id, err := uuid.Parse(chi.URLParam(r, "uuid"))
		if err != nil {
			return uuid.Nil, nil
		}
		return id, nil
	}

	// Under /calls/{uuid} the bare parameter is a call.
	if strings.HasPrefix(pattern, "/api/v1/calls/{uuid}") {
		return lookupCompany(ctx, db, `SELECT company_uuid FROM calls WHERE call_uuid=$1`, chi.URLParam(r, "uuid"))
	}

	for _, lookup := range companyEntityLookups {
		raw := chi.URLParam(r, lookup.param)
		if raw == "" {
			continue
		}
		return lookupCompany(ctx, db, lookup.query, raw)
	}

	return uuid.Nil, nil
}

// lookupCompany answers uuid.Nil both when the entity does not exist and when it
// is personal. The handler is the right place to explain either of those.
func lookupCompany(ctx context.Context, db *sql.DB, query string, raw string) (uuid.UUID, error) {
	entityID, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, nil
	}

	var companyID uuid.NullUUID
	err = db.QueryRowContext(ctx, query, entityID).Scan(&companyID)
	if errors.Is(err, sql.ErrNoRows) {
		return uuid.Nil, nil
	}
	if err != nil {
		return uuid.Nil, err
	}
	if !companyID.Valid {
		return uuid.Nil, nil
	}

	return companyID.UUID, nil
}

func changesData(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func routePattern(r *http.Request) string {
	routeContext := chi.RouteContext(r.Context())
	if routeContext == nil {
		return ""
	}

	return routeContext.RoutePattern()
}
