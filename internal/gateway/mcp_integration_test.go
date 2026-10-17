package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"cyberforge/gateway/internal/config"
	"cyberforge/gateway/internal/mcpbridge"
	"cyberforge/gateway/internal/mcphttp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type fixture struct {
	server    *httptest.Server
	calls     atomic.Int64
	pages     atomic.Int64
	started   chan struct{}
	cancelled chan struct{}
	release   chan struct{}
	mu        sync.Mutex
	headers   []http.Header
}

func newFixture(t *testing.T, jsonReply bool) *fixture {
	t.Helper()
	f := &fixture{started: make(chan struct{}, 16), cancelled: make(chan struct{}, 16), release: make(chan struct{}, 16)}
	opts := mcphttp.ServerOptions()
	opts.PageSize = 1
	server := mcp.NewServer(&mcp.Implementation{Name: "not-the-routing-identity", Version: "test"}, opts)
	for _, name := range []string{"echo", "fail", "wait", "raw", "input"} {
		schema := json.RawMessage(`{"type":"object","properties":{"message":{"type":"string","x-mcp-header":"message"}},"required":["message"],"additionalProperties":false}`)
		if name == "raw" {
			schema = json.RawMessage(`{"type":"object"}`)
		}
		tool := &mcp.Tool{Name: name, Description: "fixture " + name, InputSchema: schema, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}
		if name == "echo" {
			tool.OutputSchema = json.RawMessage(`{"type":"object","properties":{"message":{"type":"string"}},"required":["message"]}`)
		}
		server.AddTool(tool, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			f.calls.Add(1)
			var input struct {
				Message string `json:"message"`
			}
			_ = json.Unmarshal(req.Params.Arguments, &input)
			switch name {
			case "wait":
				f.started <- struct{}{}
				select {
				case <-ctx.Done():
					f.cancelled <- struct{}{}
					return nil, ctx.Err()
				case <-f.release:
				}
			case "fail":
				return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: "synthetic tool error"}}}, nil
			case "raw":
				return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(req.Params.Arguments)}}}, nil
			case "input":
				return &mcp.CallToolResult{InputRequests: mcp.InputRequestMap{"ask": &mcp.ElicitParams{Message: "do not retry"}}}, nil
			}
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: input.Message}}, StructuredContent: map[string]string{"message": input.Message}, Meta: mcp.Meta{"com.cyberforge/demo": "fixture", "io.modelcontextprotocol/private": "drop-me", "unreviewed": "drop-me"}}, nil
		})
	}
	sdk := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: jsonReply, PropagateRequestCancellation: true})
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.headers = append(f.headers, r.Header.Clone())
		f.mu.Unlock()
		if r.Header.Get("Mcp-Method") == "tools/list" {
			f.pages.Add(1)
		}
		sdk.ServeHTTP(w, r)
	}))
	t.Cleanup(f.server.Close)
	return f
}

func baseConfig(endpoint string) config.Config {
	return config.Config{Host: "127.0.0.1", Port: 8080, ReadHeaderTimeout: time.Second, ReadTimeout: time.Second, WriteTimeout: time.Second, IdleTimeout: time.Second, ShutdownTimeout: time.Second, MCPUpstreamID: "demo", MCPUpstreamURL: endpoint, MCPConnectTimeout: time.Second, MCPDiscoveryTimeout: time.Second, MCPCallTimeout: time.Second, MCPMaxRequestBytes: 4096}
}

type flow struct {
	gateway  *Server
	fixture  *fixture
	session  *mcp.ClientSession
	logs     *logBuffer
	endpoint string
	origin   string
	stop     context.CancelFunc
	done     chan error
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func newFlow(t *testing.T, jsonReply bool, configure func(*config.Config)) *flow {
	t.Helper()
	f := newFixture(t, jsonReply)
	cfg := baseConfig(f.server.URL + "/mcp")
	l := listen(t)
	_, port, _ := strings.Cut(l.Addr().String(), ":")
	cfg.Port, _ = strconv.Atoi(port)
	if configure != nil {
		configure(&cfg)
	}
	logs := new(logBuffer)
	logger := slog.New(slog.NewJSONHandler(logs, nil)).With("service", "cyberforge-gateway")
	bridge, err := mcpbridge.Open(t.Context(), cfg, logger)
	if err != nil {
		t.Fatal(err)
	}
	s := New(cfg, logger, bridge)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.serve(ctx, l) }()
	origin := "http://" + l.Addr().String()
	client, transport := mcphttp.Client(time.Second)
	client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		r.Header.Set("Authorization", "Bearer credential-secret")
		r.Header.Set("Cookie", "session=cookie-secret")
		r.Header.Set("X-Request-ID", "caller-secret")
		r.Header.Set("X-Arbitrary", "arbitrary-secret")
		return transport.RoundTrip(r)
	})
	session, err := mcphttp.SDKClient("test-client").Connect(t.Context(), mcphttp.Transport(origin+"/mcp", client), &mcp.ClientSessionOptions{ProtocolVersion: mcphttp.ProtocolVersion})
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = session.Close()
		transport.CloseIdleConnections()
		cancel()
		_ = s.http.Close()
		cleanup, c := context.WithTimeout(context.Background(), time.Second)
		defer c()
		_ = bridge.Close(cleanup)
	})
	return &flow{s, f, session, logs, origin + "/mcp", origin, cancel, done}
}

func call(t *testing.T, session *mcp.ClientSession, name string, args any) (*mcp.CallToolResult, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
}

func TestMCPRoundTripAndFailures(t *testing.T) {
	for _, jsonReply := range []bool{false, true} {
		t.Run(fmt.Sprintf("upstreamJSON=%v", jsonReply), func(t *testing.T) {
			f := newFlow(t, jsonReply, nil)
			if f.session.InitializeResult().ProtocolVersion != mcphttp.ProtocolVersion || f.session.ID() != "" {
				t.Fatal("unexpected protocol/session")
			}
			caps := f.session.InitializeResult().Capabilities
			if caps.Tools == nil || caps.Tools.ListChanged || caps.Resources != nil || caps.Prompts != nil || caps.Logging != nil {
				t.Fatalf("wrong capabilities: %+v", caps)
			}
			tools, err := f.session.ListTools(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(tools.Tools) != 5 || f.fixture.pages.Load() != 5 {
				t.Fatalf("pagination/catalog mismatch: %d / %d", len(tools.Tools), f.fixture.pages.Load())
			}
			var echo *mcp.Tool
			for _, tool := range tools.Tools {
				if tool.Name == "demo.echo" {
					echo = tool
				}
			}
			if echo == nil || echo.Description != "fixture echo" || echo.OutputSchema == nil || !echo.Annotations.ReadOnlyHint {
				t.Fatal("definition not preserved")
			}
			message := "private-argument-result-秘密"
			result, err := call(t, f.session, "demo.echo", map[string]string{"message": message})
			if err != nil {
				t.Fatal(err)
			}
			if result.IsError || result.Content[0].(*mcp.TextContent).Text != message || result.StructuredContent.(map[string]any)["message"] != message || result.Meta["com.cyberforge/demo"] != "fixture" || result.Meta["unreviewed"] != nil || result.Meta["io.modelcontextprotocol/private"] != nil {
				t.Fatalf("result not preserved/filtered: %+v", result)
			}
			if f.fixture.calls.Load() != 1 {
				t.Fatal("call executed more than once")
			}
			result, err = call(t, f.session, "demo.fail", map[string]string{"message": "ignored"})
			if err != nil || !result.IsError || result.Content[0].(*mcp.TextContent).Text != "synthetic tool error" {
				t.Fatalf("lost tool error: %+v %v", result, err)
			}
			raw := json.RawMessage(`{"number":9007199254740993,"nested":{"float":1.234567890123456789}}`)
			result, err = call(t, f.session, "demo.raw", raw)
			if err != nil || result.Content[0].(*mcp.TextContent).Text != string(raw) {
				t.Fatalf("arguments changed: %+v %v", result, err)
			}
			before := f.fixture.calls.Load()
			for _, args := range []any{map[string]string{}, map[string]int{"message": 2}, json.RawMessage(`null`), map[string]string{"message": "ok", "extra": "bad"}} {
				if _, err := call(t, f.session, "demo.echo", args); err == nil {
					t.Fatalf("invalid args accepted: %v", args)
				}
			}
			if _, err := call(t, f.session, "other.echo", map[string]string{"message": "ok"}); err == nil {
				t.Fatal("unknown tool accepted")
			}
			if f.fixture.calls.Load() != before {
				t.Fatal("invalid call invoked upstream")
			}
			// A request for extra client input must never cause an automatic replay.
			_, _ = call(t, f.session, "demo.input", map[string]string{"message": "ok"})
			if f.fixture.calls.Load() != before+1 {
				t.Fatal("multi-round-trip replay")
			}
			f.fixture.mu.Lock()
			for _, headers := range f.fixture.headers {
				for _, name := range []string{"Authorization", "Cookie", "X-Request-ID", "X-Arbitrary", "Mcp-Session-Id"} {
					if headers.Get(name) != "" {
						t.Errorf("forwarded private header %s", name)
					}
				}
				if headers.Get("Mcp-Method") == "tools/call" && strings.HasPrefix(headers.Get("Mcp-Name"), "demo.") {
					t.Error("tool name header was not regenerated")
				}
			}
			f.fixture.mu.Unlock()
			logs := f.logs.String()
			for _, secret := range []string{message, "credential-secret", "cookie-secret", "caller-secret", "arbitrary-secret", "drop-me"} {
				if strings.Contains(logs, secret) {
					t.Errorf("log leaked %q", secret)
				}
			}
			decoder := json.NewDecoder(strings.NewReader(logs))
			forwardIDs := map[string]bool{}
			accessIDs := map[string]bool{}
			for {
				var e map[string]any
				if decoder.Decode(&e) != nil {
					break
				}
				if e["msg"] == "mcp_call_completed" {
					forwardIDs[e["request_id"].(string)] = true
				}
				if e["msg"] == "request_completed" {
					accessIDs[e["request_id"].(string)] = true
				}
			}
			for id := range forwardIDs {
				if id == "" || !accessIDs[id] {
					t.Fatalf("uncorrelated forwarding log %q", id)
				}
			}
		})
	}
}

func wireCall() map[string]any {
	return map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": "demo.echo", "arguments": map[string]string{"message": "wire"}, "_meta": map[string]any{mcp.MetaKeyProtocolVersion: mcphttp.ProtocolVersion, mcp.MetaKeyClientInfo: map[string]string{"name": "wire", "version": "test"}, mcp.MetaKeyClientCapabilities: map[string]any{}}}}
}

func TestMCPTransportRejections(t *testing.T) {
	f := newFlow(t, false, nil)
	body, _ := json.Marshal(wireCall())
	for _, tc := range []struct {
		name    string
		headers map[string]string
		body    []byte
		chunked bool
		status  int
	}{
		{name: "origin", headers: map[string]string{"Origin": "https://evil.example"}, body: body, status: 403},
		{name: "empty origin", headers: map[string]string{"Origin": ""}, body: body, status: 403},
		{name: "oversized", body: bytes.Repeat([]byte("x"), 5000), status: 413},
		{name: "chunked oversized", body: bytes.Repeat([]byte("x"), 5000), chunked: true, status: 413},
		{name: "version", headers: map[string]string{"Mcp-Protocol-Version": "2099-01-01"}, body: body, status: 400},
		{name: "method mismatch", headers: map[string]string{"Mcp-Method": "tools/list"}, body: body, status: 400},
		{name: "name mismatch", headers: map[string]string{"Mcp-Name": "echo"}, body: body, status: 400},
		{name: "parameter mismatch", headers: map[string]string{"Mcp-Param-message": "wrong"}, body: body, status: 400},
		{name: "malformed JSON", body: []byte(`{"jsonrpc":`), status: 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req, _ := http.NewRequest("POST", f.endpoint, bytes.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "application/json, text/event-stream")
			req.Header.Set("Mcp-Protocol-Version", mcphttp.ProtocolVersion)
			req.Header.Set("Mcp-Method", "tools/call")
			req.Header.Set("Mcp-Name", "demo.echo")
			req.Header.Set("Mcp-Param-message", "wire")
			for k, v := range tc.headers {
				req.Header.Set(k, v)
			}
			if tc.chunked {
				req.ContentLength = -1
				req.GetBody = nil
			}
			res, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			data, _ := io.ReadAll(res.Body)
			if res.StatusCode != tc.status {
				t.Fatalf("got %d want %d: %s", res.StatusCode, tc.status, data)
			}
			if f.fixture.calls.Load() != 0 {
				t.Fatal("rejected request invoked tool")
			}
		})
	}
}

func TestMCPDeadlineAndCancellation(t *testing.T) {
	for _, clientCancel := range []bool{false, true} {
		t.Run(fmt.Sprintf("clientCancel=%v", clientCancel), func(t *testing.T) {
			f := newFlow(t, false, func(c *config.Config) {
				if !clientCancel {
					c.MCPCallTimeout = 80 * time.Millisecond
				}
			})
			// Populate schema/header cache for the wait tool.
			if _, err := f.session.ListTools(t.Context(), nil); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() {
				_, err := f.session.CallTool(ctx, &mcp.CallToolParams{Name: "demo.wait", Arguments: map[string]string{"message": "wait"}})
				done <- err
			}()
			await(t, f.fixture.started)
			if clientCancel {
				cancel()
			}
			if err := result(t, done); err == nil {
				t.Fatal("expected bounded failure")
			}
			await(t, f.fixture.cancelled)
			if f.fixture.calls.Load() != 1 {
				t.Fatal("call replayed")
			}
			if !f.gateway.bridge.Ready() {
				t.Fatal("cancellation/deadline poisoned bridge")
			}
			if _, err := call(t, f.session, "demo.echo", map[string]string{"message": "still usable"}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestMCPDisconnectLatchesNotReady(t *testing.T) {
	f := newFlow(t, false, nil)
	_, _ = f.session.ListTools(t.Context(), nil)
	f.fixture.server.Close()
	_, err := call(t, f.session, "demo.echo", map[string]string{"message": "lost"})
	if err == nil || !strings.Contains(err.Error(), "upstream_unavailable") {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.gateway.bridge.Ready() {
		t.Fatal("ready after fatal loss")
	}
	response, err := http.Get(f.origin + "/readyz")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 503 {
		t.Fatal("readiness endpoint did not fail")
	}
	_, err = call(t, f.session, "demo.echo", map[string]string{"message": "again"})
	if err == nil || f.fixture.calls.Load() != 0 {
		t.Fatal("retried failed upstream")
	}
}

func TestMCPConcurrentCalls(t *testing.T) {
	f := newFlow(t, false, nil)
	_, _ = f.session.ListTools(t.Context(), nil)
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Go(func() {
			message := fmt.Sprintf("message-%d", i)
			res, err := call(t, f.session, "demo.echo", map[string]string{"message": message})
			if err != nil {
				t.Error(err)
				return
			}
			if res.Content[0].(*mcp.TextContent).Text != message {
				t.Error("wrong response association")
			}
		})
	}
	wg.Wait()
	if f.fixture.calls.Load() != 20 {
		t.Fatal("missing or repeated calls")
	}
}

func TestMCPDrainAndForcedShutdown(t *testing.T) {
	for _, force := range []bool{false, true} {
		t.Run(fmt.Sprintf("force=%v", force), func(t *testing.T) {
			f := newFlow(t, false, func(c *config.Config) {
				c.MCPCallTimeout = 3 * time.Second
				if force {
					c.ShutdownTimeout = 80 * time.Millisecond
				}
			})
			_, _ = f.session.ListTools(t.Context(), nil)
			callDone := make(chan error, 1)
			go func() {
				_, err := call(t, f.session, "demo.wait", map[string]string{"message": "draining"})
				callDone <- err
			}()
			await(t, f.fixture.started)
			f.stop()
			deadline := time.Now().Add(time.Second)
			for f.gateway.bridge.Ready() && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			if f.gateway.bridge.Ready() {
				t.Fatal("still accepting work")
			}
			if !force {
				f.fixture.release <- struct{}{}
			}
			err := result(t, f.done)
			if force {
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("expected forced deadline: %v", err)
				}
				await(t, f.fixture.cancelled)
				if result(t, callDone) == nil {
					t.Fatal("forced call succeeded")
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if err := result(t, callDone); err != nil {
					t.Fatal(err)
				}
			}
			cleanup, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := f.gateway.bridge.Close(cleanup); err != nil {
				t.Fatal("SDK did not finish cleanup", err)
			}
		})
	}
}

func TestMCPStartupFailures(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	for _, mode := range []string{"unreachable", "connect_timeout", "discovery_timeout", "redirect", "empty"} {
		t.Run(mode, func(t *testing.T) {
			destinationCalls := atomic.Int64{}
			destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { destinationCalls.Add(1) }))
			defer destination.Close()
			server := mcp.NewServer(&mcp.Implementation{Name: "empty", Version: "test"}, mcphttp.ServerOptions())
			handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true, PropagateRequestCancellation: true})
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch mode {
				case "connect_timeout":
					_, _ = io.Copy(io.Discard, r.Body)
					<-r.Context().Done()
				case "discovery_timeout":
					if r.Header.Get("Mcp-Method") == "tools/list" {
						_, _ = io.Copy(io.Discard, r.Body)
						<-r.Context().Done()
						return
					}
					handler.ServeHTTP(w, r)
				case "redirect":
					http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
				default:
					handler.ServeHTTP(w, r)
				}
			}))
			defer upstream.Close()
			if mode == "unreachable" {
				upstream.Close()
			}
			cfg := baseConfig(upstream.URL + "/mcp")
			cfg.MCPConnectTimeout = 100 * time.Millisecond
			cfg.MCPDiscoveryTimeout = 100 * time.Millisecond
			start := time.Now()
			b, err := mcpbridge.Open(t.Context(), cfg, logger)
			if err == nil {
				cleanup, c := context.WithTimeout(context.Background(), time.Second)
				defer c()
				_ = b.Close(cleanup)
				t.Fatal("startup unexpectedly succeeded")
			}
			if time.Since(start) > 2*time.Second {
				t.Fatal("startup exceeded budget")
			}
			if destinationCalls.Load() != 0 {
				t.Fatal("redirect followed")
			}
			if strings.Contains(err.Error(), upstream.URL) {
				t.Fatal("unsanitized startup error")
			}
		})
	}
}
