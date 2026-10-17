package gateway

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync/atomic"
	"time"

	"cyberforge/gateway/internal/config"
)

// Server is a single-use HTTP service. Configuration must come from config.Load.
type Server struct {
	http            *http.Server
	logger          *slog.Logger
	ready           atomic.Bool
	shutdownTimeout time.Duration
	writeTimeout    time.Duration
	mcpWriteTimeout time.Duration
	mcpMaxBytes     int64
	origin          string
	bridge          MCPService
}

// MCPService owns upstream resources. HTTP draining happens before Close.
type MCPService interface {
	http.Handler
	Ready() bool
	StopAdmission()
	Close(context.Context) error
}

func New(cfg config.Config, logger *slog.Logger, bridge MCPService) *Server {
	s := &Server{logger: logger, shutdownTimeout: cfg.ShutdownTimeout, writeTimeout: cfg.WriteTimeout,
		mcpWriteTimeout: cfg.ReadTimeout + cfg.MCPCallTimeout + cfg.WriteTimeout, mcpMaxBytes: cfg.MCPMaxRequestBytes, origin: "http://" + cfg.Address(), bridge: bridge}
	s.http = &http.Server{
		Addr: cfg.Address(), Handler: s.middleware(s.routes()),
		ReadHeaderTimeout: cfg.ReadHeaderTimeout, ReadTimeout: cfg.ReadTimeout,
		// Per-route write deadlines are installed by middleware. A short global
		// deadline would truncate legitimate MCP SSE responses.
		WriteTimeout: 0, IdleTimeout: cfg.IdleTimeout,
		ErrorLog: slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}
	return s
}

func (s *Server) Run(ctx context.Context) error {
	s.logger.Info("starting", "address", s.http.Addr)
	listener, err := net.Listen("tcp", s.http.Addr)
	if err != nil {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), s.shutdownTimeout)
		defer cancel()
		return errors.Join(fmt.Errorf("bind listener: %w", err), s.bridge.Close(cleanupCtx))
	}
	return s.serve(ctx, listener)
}

// serve owns listener and waits for draining, even after Serve has returned.
func (s *Server) serve(ctx context.Context, listener net.Listener) error {
	s.ready.Store(true)
	defer s.ready.Store(false)
	s.logger.Info("listening", "address", listener.Addr().String())
	served := make(chan error, 1)
	go func() { served <- s.http.Serve(listener) }()
	select {
	case err := <-served:
		s.ready.Store(false)
		s.bridge.StopAdmission()
		_ = s.http.Close()
		cleanupCtx, cancel := context.WithTimeout(context.Background(), s.shutdownTimeout)
		defer cancel()
		return errors.Join(fmt.Errorf("unexpected HTTP server exit: %w", err), s.bridge.Close(cleanupCtx))
	case <-ctx.Done():
	}
	s.ready.Store(false)
	s.bridge.StopAdmission()
	s.logger.Info("shutdown_started")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), s.shutdownTimeout)
	defer cancel()
	if err := s.http.Shutdown(shutdownCtx); err != nil {
		s.logger.Error("shutdown_failed", "error", err.Error())
		closeErr := s.http.Close()
		cleanupErr := s.bridge.Close(shutdownCtx)
		<-served
		return errors.Join(fmt.Errorf("graceful shutdown: %w", err), closeErr, cleanupErr)
	}
	if err := <-served; !errors.Is(err, http.ErrServerClosed) {
		return errors.Join(fmt.Errorf("serve during shutdown: %w", err), s.bridge.Close(shutdownCtx))
	}
	if err := s.bridge.Close(shutdownCtx); err != nil {
		s.logger.Error("shutdown_failed", "reason_code", "upstream_cleanup_failed")
		return err
	}
	s.logger.Info("shutdown_completed")
	return nil
}
