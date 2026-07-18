package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/highland-io/highland/apps/api/internal/app"
	"github.com/highland-io/highland/apps/api/internal/config"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		slog.Error("config load failed", "err", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	application, err := app.Build(ctx, app.Dependencies{
		Cfg:    cfg,
		Logger: logger,
	})
	if err != nil {
		slog.Error("application build failed", "err", err)
		os.Exit(1)
	}

	if err := application.Run(ctx); err != nil {
		slog.Error("application run failed", "err", err)
		os.Exit(1)
	}
}
