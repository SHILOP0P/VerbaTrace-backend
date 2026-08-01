package health

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"verbatrace/monolit/internal/API/response"
)

type healthResponse struct {
	Status    string                 `json:"status"`
	StartedAt string                 `json:"started_at,omitempty"`
	Checks    map[string]checkResult `json:"checks,omitempty"`
}

type checkResult struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

type Check struct {
	Name string
	Run  func(ctx context.Context) error
}

type Handler struct {
	checks    []Check
	startedAt time.Time
}

func NewHandler(checks ...Check) *Handler {
	return &Handler{
		checks:    checks,
		startedAt: time.Now().UTC(),
	}
}

func Health(w http.ResponseWriter, r *http.Request) {
	NewHandler().Live(w, r)
}

func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	h.Live(w, r)
}

func (h *Handler) Live(w http.ResponseWriter, r *http.Request) {
	writeHealth(w, http.StatusOK, healthResponse{Status: "ok"})
}

func (h *Handler) Startup(w http.ResponseWriter, r *http.Request) {
	writeHealth(w, http.StatusOK, healthResponse{
		Status:    "ok",
		StartedAt: h.startedAt.Format(time.RFC3339),
		Checks: map[string]checkResult{
			"started": {Status: "ok"},
		},
	})
}

func (h *Handler) Ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	payload := healthResponse{
		Status: "ok",
		Checks: make(map[string]checkResult, len(h.checks)),
	}
	statusCode := http.StatusOK

	for _, check := range h.checks {
		result := checkResult{Status: "ok"}
		if check.Name == "" || check.Run == nil {
			result.Status = "failed"
			result.Error = "invalid readiness check"
			payload.Status = "failed"
			statusCode = http.StatusServiceUnavailable
			payload.Checks[check.Name] = result
			continue
		}

		if err := check.Run(ctx); err != nil {
			result.Status = "failed"
			result.Error = err.Error()
			payload.Status = "failed"
			statusCode = http.StatusServiceUnavailable
		}
		payload.Checks[check.Name] = result
	}

	writeHealth(w, statusCode, payload)
}

func DatabaseCheck(db *sql.DB) Check {
	return Check{
		Name: "postgres",
		Run: func(ctx context.Context) error {
			if db == nil {
				return errors.New("postgres connection is not configured")
			}
			return db.PingContext(ctx)
		},
	}
}

func WritableDirectoryCheck(name string, path string) Check {
	return Check{
		Name: name,
		Run: func(ctx context.Context) error {
			if path == "" {
				return errors.New("path is empty")
			}
			info, err := os.Stat(path)
			if err != nil {
				return err
			}
			if !info.IsDir() {
				return errors.New("path is not a directory")
			}

			file, err := os.CreateTemp(path, ".readiness-*")
			if err != nil {
				return err
			}
			name := file.Name()
			defer func() {
				_ = os.Remove(name)
			}()

			if _, err := file.WriteString("ok"); err != nil {
				_ = file.Close()
				return err
			}
			if err := file.Close(); err != nil {
				return err
			}

			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
				return nil
			}
		},
	}
}

func BinaryCheck(name string, path string) Check {
	return Check{
		Name: name,
		Run: func(ctx context.Context) error {
			if path == "" {
				return errors.New("binary path is empty")
			}
			if filepath.IsAbs(path) {
				if _, err := os.Stat(path); err != nil {
					return err
				}
			} else if _, err := exec.LookPath(path); err != nil {
				return err
			}

			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
				return nil
			}
		},
	}
}

func writeHealth(w http.ResponseWriter, status int, payload healthResponse) {
	if err := response.WriteJSON(w, status, payload); err != nil {
		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToEncodeResponse, "failed to encode response")
	}
}
