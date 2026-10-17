package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"cyberforge/gateway/internal/config"
	"cyberforge/gateway/internal/gateway"
	"cyberforge/gateway/internal/mcpbridge"
)

func main() { os.Exit(run()) }

func run() int {
	// A mutable level lets configuration errors use the same JSON logger.
	level := new(slog.LevelVar)
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})).With("service", "cyberforge-gateway")
	cfg, err := config.Load()
	if err != nil {
		logger.Error("startup_failed", "error", err.Error())
		return 1
	}
	level.Set(cfg.LogLevel)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	bridge, err := mcpbridge.Open(ctx, cfg, logger)
	if err != nil {
		logger.Error("startup_failed", "reason_code", err.Error())
		return 1
	}
	if err := gateway.New(cfg, logger, bridge).Run(ctx); err != nil {
		logger.Error("service_failed", "error", err.Error())
		return 1
	}
	return 0
}
