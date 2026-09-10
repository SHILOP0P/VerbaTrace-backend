package assistant

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestEmbeddingResponseValidation(t *testing.T) {
	vector := make([]float32, 1536)
	vector[0] = 1
	for _, tc := range []struct {
		name      string
		indices   []int
		zero      bool
		wantError bool
	}{
		{"ordered", []int{0, 1}, false, false}, {"reordered", []int{1, 0}, false, false},
		{"duplicate", []int{0, 0}, false, true}, {"out of bounds", []int{0, 2}, false, true},
		{"missing", []int{0}, false, true}, {"zero vector", []int{0, 1}, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := NewOpenRouterProvider("test", "", "test", "", 1536)
			p.client = &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
				data := []map[string]any{}
				v := vector
				if tc.zero {
					v = make([]float32, 1536)
				}
				for _, i := range tc.indices {
					data = append(data, map[string]any{"index": i, "embedding": v})
				}
				body, _ := json.Marshal(map[string]any{"data": data})
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(body)))}, nil
			})}
			_, _, err := p.Embed(context.Background(), []string{"one", "two"}, "search_query")
			if (err != nil) != tc.wantError {
				t.Fatalf("error=%v", err)
			}
		})
	}
}
