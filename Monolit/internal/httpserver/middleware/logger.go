package middleware

import (
	"net/http"
	"time"

	"verbatrace/monolit/internal/logger"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"go.uber.org/zap"
)

func RequestLogger(log logger.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Docker probes are expected and frequent; do not let them obscure
			// worker and request diagnostics in the application log.
			if isHealthCheckPath(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			start := time.Now()
			requestID := chimiddleware.GetReqID(r.Context())
			ctx := logger.ContextWithTraceID(r.Context(), requestID)
			ww := chimiddleware.NewWrapResponseWriter(w, r.ProtoMajor)

			defer func() {
				status := ww.Status()
				if status == 0 {
					status = http.StatusOK
				}

				fields := []zap.Field{
					zap.String("method", r.Method),
					zap.String("path", r.URL.Path),
					zap.String("query", r.URL.RawQuery),
					zap.Int("status", status),
					zap.Int("bytes", ww.BytesWritten()),
					zap.Duration("duration", time.Since(start)),
					zap.String("remote_addr", r.RemoteAddr),
					zap.String("user_agent", r.UserAgent()),
				}

				if routeContext := chi.RouteContext(r.Context()); routeContext != nil {
					if pattern := routeContext.RoutePattern(); pattern != "" {
						fields = append(fields, zap.String("route", pattern))
					}
				}

				switch {
				case status >= http.StatusInternalServerError:
					log.Error(ctx, "http request completed", fields...)
				case status >= http.StatusBadRequest:
					log.Warn(ctx, "http request completed", fields...)
				default:
					log.Info(ctx, "http request completed", fields...)
				}
			}()

			next.ServeHTTP(ww, r.WithContext(ctx))
		})
	}
}

func isHealthCheckPath(path string) bool {
	switch path {
	case "/health", "/health/live", "/health/ready", "/health/startup":
		return true
	default:
		return false
	}
}
