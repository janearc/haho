package main

import (
	"context"
	"flag"
	"log"
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

	logger := log.New(os.Stderr, "[haho] ", log.LstdFlags|log.Lshortfile)

	// Load config
	var cfg *config.Config
	var err error
	if configPath != "" {
		cfg, err = config.Load(configPath)
		if err != nil {
			logger.Fatalf("loading config: %v", err)
		}
	} else {
		cfg = config.Default()
		logger.Println("no config specified, using defaults")
	}

	// Create harness
	h, err := harness.New(cfg, harness.WithLogger(logger))
	if err != nil {
		logger.Fatalf("creating harness: %v", err)
	}
	defer h.Close()

	// Create and start server
	srv := server.New(h, cfg)

	// Graceful shutdown on SIGINT/SIGTERM
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigCh
		logger.Printf("received %v, shutting down", sig)
		cancel()
	}()

	logger.Printf("haho starting (provider=%s, model=%s)", cfg.Model.Provider, cfg.Model.ModelName)

	if err := srv.ListenAndServe(ctx); err != nil {
		logger.Fatalf("server error: %v", err)
	}

	logger.Println("haho stopped")
}
