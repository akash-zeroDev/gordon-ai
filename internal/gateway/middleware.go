package gateway

import (
	"context"
	"crypto/rand"
	"cyberforge/gateway/internal/requestid"
	"encoding/hex"
	"net/http"
	"time"
)

func RequestID(ctx context.Context) string {
	return requestid.From(ctx)
}

// responseWriter tracks final status while preserving SDK flushing and
// ResponseController access. This transport does not use hijacked connections.
type responseWriter struct {
	http.ResponseWriter
	status int
}

func (w *responseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *responseWriter) FlushError() error {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return http.NewResponseController(w.ResponseWriter).Flush()
}
func (w *responseWriter) Flush() { _ = w.FlushError() }

func (w *responseWriter) WriteHeader(code int) {
	if w.status != 0 {
		return
	}
	if code >= 200 || code == http.StatusSwitchingProtocols {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *responseWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(p)
}

func (s *Server) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var random [16]byte
		// Go 1.26 crypto/rand.Read always fills the buffer or terminates the process.
		_, _ = rand.Read(random[:])
		id := hex.EncodeToString(random[:])
		r = r.WithContext(requestid.With(r.Context(), id))
		w.Header().Set("X-Request-ID", id)
		writeBudget := s.writeTimeout
		if r.URL.Path == "/mcp" {
			writeBudget = s.mcpWriteTimeout
		}
		// Includes MCP body-read + call + final-write budgets. Never buffers SSE.
		_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(writeBudget))
		tracked := &responseWriter{ResponseWriter: w}
		started := time.Now()
		defer func() {
			if tracked.status == 0 {
				tracked.status = http.StatusOK
			}
			s.logger.Info("request_completed", "request_id", id, "method", r.Method,
				"route", routeLabel(r.URL.Path), "status", tracked.status,
				"duration_ms", float64(time.Since(started))/float64(time.Millisecond))
		}()
		defer func() {
			if recovered := recover(); recovered != nil {
				// Panic values can contain credentials or bodies; do not log them.
				s.logger.Error("handler_panic", "request_id", id, "response_committed", tracked.status != 0)
				if tracked.status == 0 {
					writeJSON(tracked, http.StatusInternalServerError, "error", "internal_server_error")
				}
			}
		}()
		next.ServeHTTP(tracked, r)
	})
}

func routeLabel(path string) string {
	switch path {
	case "/healthz", "/readyz", "/mcp":
		return path
	default:
		return "unmatched"
	}
}
