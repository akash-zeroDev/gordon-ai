// Package mcphttp defines the local demo's transport boundary.
package mcphttp

import (
	"errors"
	"net"
	"net/http"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const ProtocolVersion = "2026-07-28"

// Client owns its transport. No environment proxy, cookies, redirects, OAuth or
// retry middleware is involved. Callers supply per-operation contexts.
func Client(connectTimeout time.Duration) (*http.Client, *http.Transport) {
	transport := &http.Transport{
		DialContext:     (&net.Dialer{Timeout: connectTimeout}).DialContext,
		IdleConnTimeout: 30 * time.Second, MaxIdleConns: 16, MaxIdleConnsPerHost: 16,
	}
	return &http.Client{Transport: protocolTransport{transport}, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("redirect_rejected") }}, transport
}

type protocolTransport struct{ base *http.Transport }

func (t protocolTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	// The SDK can attempt legacy initialization/cancel notifications when its
	// initial discovery probe fails. This demo supports only 2026-07-28: reject
	// that fallback before I/O. In particular a detached legacy cancel request
	// must not add the SDK's 5-second cleanup wait to MCP_CONNECT_TIMEOUT.
	method := r.Header.Get("Mcp-Method")
	if r.Method != http.MethodPost || r.Header.Get("Mcp-Protocol-Version") != ProtocolVersion || (method != "server/discover" && method != "tools/list" && method != "tools/call") {
		return nil, errors.New("unsupported_protocol_operation")
	}
	return t.base.RoundTrip(r)
}

func SDKClient(name string) *mcp.Client {
	return mcp.NewClient(&mcp.Implementation{Name: name, Version: "0.2.0"}, &mcp.ClientOptions{
		Capabilities: &mcp.ClientCapabilities{}, MultiRoundTrip: &mcp.MultiRoundTripOptions{Disabled: true},
	})
}

func Transport(endpoint string, client *http.Client) *mcp.StreamableClientTransport {
	return &mcp.StreamableClientTransport{Endpoint: endpoint, HTTPClient: client, MaxRetries: -1, DisableStandaloneSSE: true}
}

func ServerOptions() *mcp.ServerOptions {
	return &mcp.ServerOptions{SupportedProtocolVersions: []string{ProtocolVersion}, Capabilities: &mcp.ServerCapabilities{Tools: &mcp.ToolCapabilities{}}}
}

// Protect checks every present Origin (including empty or repeated headers) and
// caps the raw body before the SDK parses it. No wildcard CORS is enabled.
func Protect(next http.Handler, origin string, maxBytes int64) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if values, present := r.Header["Origin"]; present && (len(values) != 1 || values[0] != origin) {
			http.Error(w, "origin_not_allowed", http.StatusForbidden)
			return
		}
		if r.ContentLength > maxBytes {
			http.Error(w, "request_too_large", http.StatusRequestEntityTooLarge)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
		next.ServeHTTP(w, r)
	})
}
