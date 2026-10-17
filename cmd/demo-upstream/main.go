package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"cyberforge/gateway/internal/config"
	"cyberforge/gateway/internal/demotools"
	"cyberforge/gateway/internal/mcphttp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() { os.Exit(run()) }
func run() int {
	addr := flag.String("addr", "127.0.0.1:9090", "loopback IP:port")
	jsonResponse := flag.Bool("json", false, "use JSON replies instead of request-scoped SSE")
	flag.Parse()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("service", "cyberforge-demo-upstream")
	if err := config.ValidateEndpoint("http://" + *addr + "/mcp"); err != nil {
		logger.Error("invalid_addr")
		return 1
	}
	listener, err := net.Listen("tcp", *addr)
	if err != nil {
		logger.Error("bind_failed")
		return 1
	}
	server := demotools.Server()
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: *jsonResponse, PropagateRequestCancellation: true, MaxRequestBodyBytes: 1 << 20})
	mux := http.NewServeMux()
	mux.Handle("/mcp", mcphttp.Protect(handler, "http://"+*addr, 1<<20))
	httpServer := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 0, IdleTimeout: 60 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	done := make(chan error, 1)
	go func() { done <- httpServer.Serve(listener) }()
	logger.Info("listening", "address", listener.Addr().String())
	select {
	case err := <-done:
		if !errors.Is(err, http.ErrServerClosed) {
			logger.Error("serve_failed")
			return 1
		}
		return 0
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		_ = httpServer.Close()
		logger.Error("shutdown_failed")
		return 1
	}
	<-done
	logger.Info("shutdown_completed")
	return 0
}
