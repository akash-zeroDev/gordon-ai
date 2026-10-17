package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"cyberforge/gateway/internal/config"
)

type logBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *logBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}
func (b *logBuffer) String() string { b.mu.Lock(); defer b.mu.Unlock(); return b.b.String() }

func testServer() (*Server, *logBuffer) {
	logs := new(logBuffer)
	logger := slog.New(slog.NewJSONHandler(logs, nil)).With("service", "cyberforge-gateway")
	cfg := config.Config{Host: "127.0.0.1", Port: 8080, ReadHeaderTimeout: time.Second, ReadTimeout: 2 * time.Second, WriteTimeout: 3 * time.Second, IdleTimeout: 4 * time.Second, ShutdownTimeout: time.Second}
	cfg.MCPMaxRequestBytes = 1 << 20
	return New(cfg, logger, foundationMCP{}), logs
}

// Foundation tests isolate HTTP behavior; real MCP is covered by loopback tests.
type foundationMCP struct{}

func (foundationMCP) ServeHTTP(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }
func (foundationMCP) Ready() bool                                      { return true }
func (foundationMCP) StopAdmission()                                   {}
func (foundationMCP) Close(context.Context) error                      { return nil }

func TestEndpointContracts(t *testing.T) {
	s, _ := testServer()
	s.ready.Store(true)
	for _, tc := range []struct {
		method, path string
		status       int
		body, allow  string
	}{
		{"GET", "/healthz", 200, `{"status":"ok"}`, ""},
		{"GET", "/readyz", 200, `{"status":"ready"}`, ""},
		{"POST", "/healthz", 405, `{"error":"method_not_allowed"}`, "GET"},
		{"HEAD", "/readyz", 405, `{"error":"method_not_allowed"}`, "GET"},
		{"OPTIONS", "/readyz", 405, `{"error":"method_not_allowed"}`, "GET"},
		{"GET", "/missing", 404, `{"error":"not_found"}`, ""},
		{"GET", "/healthz/extra", 404, `{"error":"not_found"}`, ""},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			s.http.Handler.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
			if w.Code != tc.status || strings.TrimSpace(w.Body.String()) != tc.body || w.Header().Get("Content-Type") != "application/json" || w.Header().Get("Allow") != tc.allow {
				t.Fatalf("unexpected response: %d %v %s", w.Code, w.Header(), w.Body)
			}
		})
	}
	s.ready.Store(false)
	w := httptest.NewRecorder()
	s.http.Handler.ServeHTTP(w, httptest.NewRequest("GET", "/readyz", nil))
	if w.Code != 503 || strings.TrimSpace(w.Body.String()) != `{"status":"not_ready"}` {
		t.Fatalf("unexpected draining response: %v", w)
	}
}

func TestCorrelationAndSafeLogs(t *testing.T) {
	s, logs := testServer()
	var contextID string
	handler := s.middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contextID = RequestID(r.Context())
		_, _ = w.Write([]byte("ok")) // Implicit 200 must be recorded.
	}))
	r := httptest.NewRequest("POST", "/path-secret?token=query-secret", strings.NewReader("body-secret"))
	r.Header.Set("Authorization", "Bearer auth-secret")
	r.Header.Set("Cookie", "session=cookie-secret")
	r.Header.Set("X-Request-ID", "caller-secret")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	id := w.Header().Get("X-Request-ID")
	if !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(id) || id != contextID {
		t.Fatalf("bad request ID %q / %q", id, contextID)
	}
	text := logs.String()
	for _, secret := range []string{"path-secret", "query-secret", "body-secret", "auth-secret", "cookie-secret", "caller-secret"} {
		if strings.Contains(text, secret) {
			t.Fatalf("log leaked %s", secret)
		}
	}
	var event map[string]any
	if err := json.Unmarshal([]byte(text), &event); err != nil {
		t.Fatal(err)
	}
	if event["request_id"] != id || event["status"] != float64(200) || event["route"] != "unmatched" || event["method"] != "POST" || event["service"] != "cyberforge-gateway" || event["duration_ms"] == nil {
		t.Fatalf("bad access log: %v", event)
	}
}

func TestPanicRecovery(t *testing.T) {
	for _, committed := range []bool{false, true} {
		s, logs := testServer()
		handler := s.middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if committed {
				w.WriteHeader(202)
				_, _ = w.Write([]byte("already written"))
			}
			panic("private-panic-secret")
		}))
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("GET", "/test-only", nil))
		wantStatus, wantBody := 500, "{\"error\":\"internal_server_error\"}\n"
		if committed {
			wantStatus, wantBody = 202, "already written"
		}
		if w.Code != wantStatus || w.Body.String() != wantBody {
			t.Fatalf("bad recovery response: %d %s", w.Code, w.Body)
		}
		if strings.Contains(logs.String(), "private-panic-secret") || !strings.Contains(logs.String(), w.Header().Get("X-Request-ID")) {
			t.Fatal("unsafe or uncorrelated panic log")
		}
		decoder := json.NewDecoder(strings.NewReader(logs.String()))
		var panicLog, accessLog map[string]any
		if err := decoder.Decode(&panicLog); err != nil {
			t.Fatal(err)
		}
		if err := decoder.Decode(&accessLog); err != nil {
			t.Fatal(err)
		}
		if accessLog["status"] != float64(wantStatus) {
			t.Fatalf("incorrect recovery status: %v", accessLog)
		}
		healthy := httptest.NewRecorder()
		s.http.Handler.ServeHTTP(healthy, httptest.NewRequest("GET", "/healthz", nil))
		if healthy.Code != 200 {
			t.Fatal("service unhealthy after panic")
		}
	}
}

func TestConcurrentRequests(t *testing.T) {
	s, _ := testServer()
	const count = 100
	ids := make(chan string, count)
	var wg sync.WaitGroup
	for range count {
		wg.Go(func() {
			s.ready.Store(true)
			w := httptest.NewRecorder()
			s.http.Handler.ServeHTTP(w, httptest.NewRequest("GET", "/readyz", nil))
			ids <- w.Header().Get("X-Request-ID")
			s.ready.Store(false)
		})
	}
	wg.Wait()
	close(ids)
	seen := map[string]bool{}
	for id := range ids {
		if seen[id] || id == "" {
			t.Fatalf("duplicate/empty ID: %s", id)
		}
		seen[id] = true
	}
}

func TestServerTimeouts(t *testing.T) {
	s, _ := testServer()
	if s.http.Addr != "127.0.0.1:8080" || s.http.ReadHeaderTimeout != time.Second || s.http.ReadTimeout != 2*time.Second || s.http.WriteTimeout != 0 || s.writeTimeout != 3*time.Second || s.http.IdleTimeout != 4*time.Second {
		t.Fatalf("timeouts not wired: %+v", s.http)
	}
}

func listen(t *testing.T) net.Listener {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	return l
}

func await(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for handler")
	}
}

func result(t *testing.T, ch <-chan error) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-time.After(3 * time.Second):
		t.Fatal("server did not stop")
		return nil
	}
}

func TestShutdownDrainsRequest(t *testing.T) {
	s, logs := testServer()
	l := listen(t)
	started, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	s.http.Handler = s.middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		if r.Context().Err() != nil {
			t.Error("request context canceled before draining")
		}
		_, _ = w.Write([]byte("finished"))
	}))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.serve(ctx, l) }()
	response := make(chan error, 1)
	go func() {
		client := &http.Client{Timeout: 3 * time.Second}
		r, err := client.Get("http://" + l.Addr().String())
		if err == nil {
			defer r.Body.Close()
			body, readErr := io.ReadAll(r.Body)
			err = readErr
			if string(body) != "finished" {
				err = errors.New("incomplete response")
			}
		}
		response <- err
	}()
	await(t, started)
	cancel()
	deadline := time.Now().Add(time.Second)
	for s.ready.Load() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if s.ready.Load() {
		t.Fatal("readiness did not clear")
	}
	w := httptest.NewRecorder()
	s.routes().ServeHTTP(w, httptest.NewRequest("GET", "/readyz", nil))
	if w.Code != 503 {
		t.Fatal("draining endpoint should return 503")
	}
	select {
	case err := <-done:
		t.Fatalf("returned before drain: %v", err)
	default:
	}
	release <- struct{}{}
	if err := result(t, response); err != nil {
		t.Fatal(err)
	}
	if err := result(t, done); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logs.String(), "shutdown_completed") {
		t.Fatal("missing completed log")
	}
}

func TestShutdownDeadlineClosesConnections(t *testing.T) {
	s, logs := testServer()
	s.shutdownTimeout = 30 * time.Millisecond
	l := listen(t)
	started, stopped := make(chan struct{}), make(chan struct{})
	s.http.Handler = s.middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
		close(stopped)
	}))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.serve(ctx, l) }()
	clientDone := make(chan struct{})
	go func() {
		defer close(clientDone)
		client := &http.Client{Timeout: 3 * time.Second}
		r, err := client.Get("http://" + l.Addr().String())
		if err == nil {
			_ = r.Body.Close()
		}
	}()
	await(t, started)
	cancel()
	if err := result(t, done); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected shutdown deadline: %v", err)
	}
	await(t, stopped)
	await(t, clientDone)
	if s.ready.Load() || !strings.Contains(logs.String(), "shutdown_failed") || strings.Contains(logs.String(), "shutdown_completed") {
		t.Fatal("incorrect forced shutdown state/logs")
	}
}

func TestBindFailure(t *testing.T) {
	s, logs := testServer()
	l := listen(t)
	s.http.Addr = l.Addr().String()
	if err := s.Run(context.Background()); err == nil {
		t.Fatal("expected occupied port error")
	}
	if strings.Contains(logs.String(), "listening") {
		t.Fatal("false successful startup")
	}
}

func TestUnexpectedServeFailure(t *testing.T) {
	s, _ := testServer()
	l := listen(t)
	_ = l.Close()
	if err := s.serve(context.Background(), l); err == nil {
		t.Fatal("expected serve error")
	}
	if s.ready.Load() {
		t.Fatal("ready after failure")
	}
}
