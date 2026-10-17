package gateway

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"cyberforge/gateway/internal/config"
	"cyberforge/gateway/internal/demotools"
	"cyberforge/gateway/internal/mcphttp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMiddlewareSDKJSONAndSSE(t *testing.T) {
	for _, jsonReply := range []bool{false, true} {
		t.Run(fmt.Sprint(jsonReply), func(t *testing.T) {
			s, _ := testServer()
			sdk := demotools.Server()
			handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return sdk }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: jsonReply, PropagateRequestCancellation: true})
			httpServer := httptest.NewServer(s.middleware(handler))
			defer httpServer.Close()
			client, transport := mcphttp.Client(time.Second)
			defer transport.CloseIdleConnections()
			session, err := mcphttp.SDKClient("framing-test").Connect(t.Context(), mcphttp.Transport(httpServer.URL+"/mcp", client), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close()
			if _, err := session.ListTools(t.Context(), nil); err != nil {
				t.Fatal(err)
			}
			res, err := call(t, session, "echo", map[string]string{"message": "framed"})
			if err != nil || res.Content[0].(*mcp.TextContent).Text != "framed" {
				t.Fatalf("framing failed: %+v %v", res, err)
			}
		})
	}
}

func TestResponseRecorderFlushesBeforeHandlerReturns(t *testing.T) {
	s, logs := testServer()
	release := make(chan struct{})
	ts := httptest.NewServer(s.middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: first\n\n")
		if err := http.NewResponseController(w).Flush(); err != nil {
			t.Error(err)
		}
		<-release
		panic("must-not-append-json-after-flush")
	})))
	defer ts.Close()
	defer close(release)
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+"/mcp", nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal("flush was buffered", err)
	}
	defer res.Body.Close()
	scanner := bufio.NewScanner(res.Body)
	if !scanner.Scan() || scanner.Text() != "data: first" || res.Header.Get("X-Request-ID") == "" {
		t.Fatal("missing first streaming event")
	}
	release <- struct{}{}
	for scanner.Scan() {
		if scanner.Text() != "" {
			t.Fatalf("recovery appended content: %q", scanner.Text())
		}
	}
	if logs.String() == "" {
		t.Fatal("missing streaming access log")
	}
}

func TestMCPOutlivesOldWriteDeadline(t *testing.T) {
	f := newFlow(t, false, func(c *config.Config) { c.WriteTimeout = 20 * time.Millisecond; c.MCPCallTimeout = time.Second })
	_, _ = f.session.ListTools(t.Context(), nil)
	done := make(chan error, 1)
	go func() {
		_, err := call(t, f.session, "demo.wait", map[string]string{"message": "longer than old write timeout"})
		done <- err
	}()
	await(t, f.fixture.started)
	time.Sleep(80 * time.Millisecond)
	f.fixture.release <- struct{}{}
	if err := result(t, done); err != nil {
		t.Fatal("old global write deadline truncated MCP", err)
	}
}
