// hahod — the resident daemon (RFC section 8). It holds sessions and
// queues, admits against lane budgets, and does no model work. Clients
// speak loopback HTTP; the haho CLI is a thin wrapper over the same API.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"syscall"
	"time"

	"github.com/janearc/haho/internal/hahod/admission"
	"github.com/janearc/haho/internal/hahod/server"
	"github.com/janearc/haho/internal/hahod/store"
)

// Config resolves once at startup: flag over environment over default,
// per fleet rules. It carries no credential of any kind — specs reference
// env var names, and the values never pass through here.
type config struct {
	addr      string
	storePath string
}

func loadConfig() config {
	defStore := filepath.Join(os.Getenv("HOME"), ".local", "state", "haho", "hahod.db")
	addr := flag.String("addr", envOr("HAHOD_ADDR", "127.0.0.1:8790"),
		"loopback address to serve on (env HAHOD_ADDR)")
	storePath := flag.String("store", envOr("HAHOD_STORE", defStore),
		"path to hahod's bbolt store (env HAHOD_STORE)")
	flag.Parse()
	return config{addr: *addr, storePath: *storePath}
}

func envOr(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}

// requireLoopback enforces the RFC's posture mechanically: hahod's API
// binds loopback on the host, and a routable bind is a misconfiguration
// refused at startup, not a surprise discovered by a port scan.
func requireLoopback(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("addr %q: %w", addr, err)
	}
	if !slices.Contains([]string{"127.0.0.1", "::1", "localhost"}, host) {
		return fmt.Errorf("addr %q is not loopback; hahod serves 127.0.0.1, ::1, or localhost only", addr)
	}
	return nil
}

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg := loadConfig()

	if err := requireLoopback(cfg.addr); err != nil {
		log.Error("startup refused", "error", err.Error())
		os.Exit(1)
	}
	if err := os.MkdirAll(filepath.Dir(cfg.storePath), 0o700); err != nil {
		log.Error("startup refused", "error", fmt.Sprintf("store dir: %v", err))
		os.Exit(1)
	}
	st, err := store.Open(cfg.storePath)
	if err != nil {
		log.Error("startup refused", "error", err.Error())
		os.Exit(1)
	}
	defer st.Close()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	srv := server.New(ctx, st, admission.New(admission.Defaults()), log, "http://"+cfg.addr)
	httpSrv := &http.Server{
		Addr:              cfg.addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		<-ctx.Done()
		// Drain-and-land: give open requests a moment, then exit. In-flight
		// runs are bounded by the signal context and land FAILED with the
		// cancellation as their reason — loud, never half-recorded.
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpSrv.Shutdown(shutdownCtx); err != nil {
			log.Error("shutdown", "error", err.Error())
		}
	}()

	log.Info("hahod serving", "addr", cfg.addr, "store", cfg.storePath,
		"lanes", "api only; metal/ane/cpu REFUSED until served")
	if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Error("serve", "error", err.Error())
		os.Exit(1)
	}
	log.Info("hahod stopped")
}
