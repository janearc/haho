// hahod — the resident daemon (RFC section 8). It holds sessions and
// queues and admits against lane budgets. In the RFC's shape it does no
// model work; until the harness process split lands, this skeleton runs
// the chute in-process (the divergence is surfaced in the runner package
// and the README). Clients speak loopback HTTP; the haho CLI, when built,
// will be a thin wrapper over the same API.
//
// Trust model, v1: loopback is the trust boundary. A client that can
// reach hahod is a same-uid process on the operator's host, and specs
// deliberately name arbitrary base_urls and env-var references (the RFC's
// worked specs dial local ollama and remote APIs alike) — so a hostile
// loopback client could point the daemon's env secrets at a hostile URL.
// Requestor authentication and a base_url allowlist are open questions
// for the operator, recorded in the README's divergence list; they are
// not silently pre-decided here.
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

// config resolves once at startup: flag over environment over default,
// per fleet rules. It carries no credential of any kind — specs reference
// env var names, and the values never pass through here.
type config struct {
	addr      string
	storePath string
}

// loadConfig parses args (argv minus the program name). It owns its own
// FlagSet so tests can exercise precedence without touching globals.
func loadConfig(args []string) (config, error) {
	fs := flag.NewFlagSet("hahod", flag.ContinueOnError)
	defStore := filepath.Join(os.Getenv("HOME"), ".local", "state", "haho", "hahod.db")
	addr := fs.String("addr", envOr("HAHOD_ADDR", "127.0.0.1:8790"),
		"loopback address to serve on (env HAHOD_ADDR)")
	storePath := fs.String("store", envOr("HAHOD_STORE", defStore),
		"path to hahod's bbolt store (env HAHOD_STORE)")
	if err := fs.Parse(args); err != nil {
		return config{}, err
	}
	return config{addr: *addr, storePath: *storePath}, nil
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
	cfg, err := loadConfig(os.Args[1:])
	if err != nil {
		log.Error("startup refused", "error", err.Error())
		os.Exit(1)
	}

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

	// Any queued/running row at startup is a crash's leftover: land it
	// FAILED, loudly, before serving — a dead flight must never report
	// alive.
	if n, err := server.RecoverOrphans(st, log); err != nil {
		log.Error("startup refused", "error", fmt.Sprintf("orphan recovery: %v", err))
		os.Exit(1)
	} else if n > 0 {
		log.Warn("recovered orphaned sessions", "count", n)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	srv := server.New(ctx, st, admission.New(admission.Defaults()), log, "http://"+cfg.addr)
	httpSrv := &http.Server{
		Addr:              cfg.addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	go func() {
		<-ctx.Done()
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
	// Drain-and-land: the signal context is dead, so every in-flight run
	// fails fast and lands FAILED with the cancellation as its reason;
	// queued jobs land FAILED without running. Only then does the store
	// close — nothing exits half-recorded.
	srv.Drain()
	log.Info("hahod stopped")
}
