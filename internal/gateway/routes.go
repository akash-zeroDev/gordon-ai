package gateway

import (
	"cyberforge/gateway/internal/mcphttp"
	"encoding/json"
	"net/http"
)

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if !healthMethod(w, r) {
			return
		}
		writeJSON(w, http.StatusOK, "status", "ok")
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if !healthMethod(w, r) {
			return
		}
		if !s.ready.Load() || !s.bridge.Ready() {
			writeJSON(w, http.StatusServiceUnavailable, "status", "not_ready")
			return
		}
		writeJSON(w, http.StatusOK, "status", "ready")
	})
	mux.Handle("/mcp", mcphttp.Protect(s.bridge, s.origin, s.mcpMaxBytes))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusNotFound, "error", "not_found")
	})
	return mux
}

// Explicitly GET-only, including rejecting HEAD with Allow: GET.
func healthMethod(w http.ResponseWriter, r *http.Request) bool {
	if r.Method == http.MethodGet {
		return true
	}
	w.Header().Set("Allow", http.MethodGet)
	writeJSON(w, http.StatusMethodNotAllowed, "error", "method_not_allowed")
	return false
}

func writeJSON(w http.ResponseWriter, status int, key, value string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{key: value})
}
