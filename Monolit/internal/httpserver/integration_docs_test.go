package httpserver

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestIntegrationDocsUI(t *testing.T) {
	recorder := httptest.NewRecorder()
	integrationDocsUI(recorder, httptest.NewRequest(http.MethodGet, "/docs/integrations", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if contentType := recorder.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "text/html") {
		t.Fatalf("Content-Type = %q, want text/html", contentType)
	}
	if !strings.Contains(recorder.Body.String(), "/docs/integrations/openapi") {
		t.Fatal("Swagger UI does not reference the integration OpenAPI document")
	}
}

func TestIntegrationOpenAPI(t *testing.T) {
	recorder := httptest.NewRecorder()
	integrationOpenAPI(recorder, httptest.NewRequest(http.MethodGet, "/docs/integrations/openapi.yaml", nil))

	body := recorder.Body.String()
	var document map[string]any
	if err := yaml.Unmarshal([]byte(body), &document); err != nil {
		t.Fatalf("OpenAPI document is not valid YAML: %v", err)
	}
	if document["openapi"] != "3.1.0" {
		t.Fatalf("OpenAPI version = %v, want 3.1.0", document["openapi"])
	}
	for _, expected := range []string{
		"openapi: 3.1.0",
		"/api/{environment}/v1/ingest/calls:",
		"/api/{environment}/v2/destinations:",
		"bearerAuth:",
		"Idempotency-Key",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("OpenAPI document does not contain %q", expected)
		}
	}
}
