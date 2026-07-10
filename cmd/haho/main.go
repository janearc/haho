package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/janearc/haho/internal/config"
	"github.com/janearc/haho/internal/harness"
	"github.com/janearc/haho/internal/server"
)

func main() {
	var configPath string
	flag.StringVar(&configPath, "config", "", "path to config JSON file")
	flag.Parse()

	// Structured JSON logs to stdout. The harness runs in a container, so its
	// stdout is collected by the platform and shipped to Kafka — the harness
	// never talks to the bus directly (compose via the substrate).
	root := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(root)
	logger := root.With("component", "haho")

	var cfg *config.Config
	var err error
	if configPath != "" {
		cfg, err = config.Load(configPath)
		if err != nil {
			logger.Error("loading config", "error", err, "path", configPath)
			os.Exit(1)
		}
	} else {
		cfg = config.Default()
		logger.Info("no config specified, using defaults")
	}

	h, err := harness.New(cfg, harness.WithLogger(root.With("component", "harness")))
	if err != nil {
		logger.Error("creating harness", "error", err)
		os.Exit(1)
	}
	defer h.Close()

	srv := server.New(h, cfg)

	// Graceful shutdown on SIGINT/SIGTERM.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigCh
		logger.Info("shutting down", "signal", sig.String())
		cancel()
	}()

	logger.Info("haho starting",
		"provider", cfg.Model.Provider,
		"model", cfg.Model.ModelName,
		"listen_addr", cfg.ListenAddr,
	)

	if err := srv.ListenAndServe(ctx); err != nil {
		logger.Error("server error", "error", err)
		os.Exit(1)
	}

	logger.Info("haho stopped")
}
