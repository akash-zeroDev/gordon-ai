// Package mcpbridge terminates MCP and forwards one tool invocation to one
// configured upstream. The immutable catalog is refreshed only by restart.
package mcpbridge

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"

	"cyberforge/gateway/internal/config"
	"cyberforge/gateway/internal/mcphttp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type Bridge struct {
	cfg       config.Config
	logger    *slog.Logger
	session   *mcp.ClientSession
	transport *http.Transport
	entries   map[string]*entry
	handler   http.Handler
	available atomic.Bool
	accepting atomic.Bool
	lifetime  context.Context
	cancel    context.CancelFunc
	closeOnce sync.Once
	closed    chan struct{}
	closeErr  error
}

func Open(ctx context.Context, cfg config.Config, logger *slog.Logger) (_ *Bridge, err error) {
	b := &Bridge{cfg: cfg, logger: logger, closed: make(chan struct{})}
	b.lifetime, b.cancel = context.WithCancel(context.Background())
	client, transport := mcphttp.Client(cfg.MCPConnectTimeout)
	b.transport = transport
	defer func() {
		if err != nil {
			// Failed initialization has no admitted calls to drain.
			b.cancel()
			if b.session != nil {
				_ = b.session.Close()
			}
			transport.CloseIdleConnections()
		}
	}()
	connectCtx, cancel := context.WithTimeout(ctx, cfg.MCPConnectTimeout)
	b.session, err = mcphttp.SDKClient("cyberforge-upstream").Connect(connectCtx, mcphttp.Transport(cfg.MCPUpstreamURL, client), &mcp.ClientSessionOptions{ProtocolVersion: mcphttp.ProtocolVersion})
	cancel()
	if err != nil {
		return nil, errors.New("upstream_connect_failed")
	}
	if b.session.InitializeResult().ProtocolVersion != mcphttp.ProtocolVersion {
		return nil, errors.New("upstream_protocol_mismatch")
	}
	if caps := b.session.InitializeResult().Capabilities; caps == nil || caps.Tools == nil {
		return nil, errors.New("upstream_tools_unsupported")
	}
	discoveryCtx, cancel := context.WithTimeout(ctx, cfg.MCPDiscoveryTimeout)
	defer cancel()
	var tools []*mcp.Tool
	cursor := ""
	seen := map[string]bool{}
	for {
		page, e := b.session.ListTools(discoveryCtx, &mcp.ListToolsParams{Cursor: cursor})
		if e != nil {
			return nil, errors.New("upstream_discovery_failed")
		}
		tools = append(tools, page.Tools...)
		if len(tools) > 1024 {
			return nil, errors.New("catalog_too_large")
		}
		cursor = page.NextCursor
		if cursor == "" {
			break
		}
		if seen[cursor] {
			return nil, errors.New("catalog_repeated_cursor")
		}
		seen[cursor] = true
	}
	b.entries, err = catalog(cfg.MCPUpstreamID, tools)
	if err != nil {
		return nil, err
	}
	b.handler, err = b.buildHandler()
	if err != nil {
		return nil, err
	}
	if discoveryCtx.Err() != nil {
		return nil, errors.New("upstream_discovery_failed")
	}
	b.available.Store(true)
	b.accepting.Store(true)
	logger.Info("upstream_ready", "upstream_id", cfg.MCPUpstreamID, "tools", len(b.entries), "protocol", mcphttp.ProtocolVersion)
	return b, nil
}

func (b *Bridge) Ready() bool    { return b.available.Load() && b.accepting.Load() }
func (b *Bridge) StopAdmission() { b.accepting.Store(false) }

func (b *Bridge) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !b.accepting.Load() {
		http.Error(w, "gateway_draining", http.StatusServiceUnavailable)
		return
	}
	b.handler.ServeHTTP(w, r)
}

// Close uses the caller's remaining shutdown budget, not a new full timeout.
// Cancelling the lifetime stops outstanding upstream requests before SDK close.
func (b *Bridge) Close(ctx context.Context) error {
	b.StopAdmission()
	b.available.Store(false)
	b.closeOnce.Do(func() {
		b.cancel()
		go func() {
			b.closeErr = b.session.Close()
			b.transport.CloseIdleConnections()
			close(b.closed)
		}()
	})
	select {
	case <-b.closed:
		if b.closeErr != nil {
			return errors.New("upstream_cleanup_failed")
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
