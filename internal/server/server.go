package server

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"sync/atomic"
	"time"

	"github.com/janearc/haho/internal/config"
	"github.com/janearc/haho/internal/filter"
	"github.com/janearc/haho/internal/harness"
)

// controlTokenEnv names the environment variable holding the bearer token that
// guards the mutating control endpoints. When unset, those endpoints are open
// (a loud startup warning is logged).
const controlTokenEnv = "HAHO_CONTROL_TOKEN"

type Server struct {
	harness    *harness.Harness
	config     *config.Config
	httpServer *http.Server
	logger     *slog.Logger
	listener   net.Listener
	authToken  string

	// metrics (atomic).
	submitTotal int64
	submitBusy  int64
	submitError int64
}

type SubmitRequest struct {
	Messages      []MessageRequest  `json:"messages"`
	ModelOverride string            `json:"model_override,omitempty"`
	Params        map[string]string `json:"params,omitempty"`
}

type MessageRequest struct {
	Role     string            `json:"role"`
	Content  string            `json:"content"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

type SubmitResponse struct {
	Content    string            `json:"content"`
	TokensUsed int               `json:"tokens_used"`
	Model      string            `json:"model"`
	Cached     bool              `json:"cached"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

type HealthResponse struct {
	State            string `json:"state"`
	StateDescription string `json:"state_description"`
	ModelName        string `json:"model_name"`
	UptimeSeconds    int64  `json:"uptime_seconds"`
	Message          string `json:"message,omitempty"`
}

type SetFilterRequest struct {
	Stage   string               `json:"stage"` // "input" or "output"
	Filters []config.FilterEntry `json:"filters"`
}

type SetFilterResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}

func New(h *harness.Harness, cfg *config.Config) *Server {
	s := &Server{
		harness:   h,
		config:    cfg,
		logger:    slog.Default().With("component", "server"),
		authToken: os.Getenv(controlTokenEnv),
	}

	if s.authToken == "" {
		s.logger.Warn("control API is unauthenticated", "env", controlTokenEnv)
	}

	mux := http.NewServeMux()
	// Mutating / cost-bearing endpoints require the bearer token when set.
	mux.HandleFunc("POST /v1/submit", s.withAuth(s.handleSubmit))
	mux.HandleFunc("POST /v1/filters", s.withAuth(s.handleSetFilters))
	// Read-only endpoints are open for liveness probes and scrapers.
	mux.HandleFunc("GET /v1/health", s.handleHealth)
	mux.HandleFunc("GET /v1/config", s.handleConfig)
	mux.HandleFunc("GET /v1/metrics", s.handleMetrics)
	mux.HandleFunc("GET /metrics", s.handleMetrics)

	s.httpServer = &http.Server{
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 5 * time.Minute,
	}

	return s
}

// withAuth enforces the bearer token on a handler when one is configured.
func (s *Server) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.authToken != "" {
			const prefix = "Bearer "
			got := r.Header.Get("Authorization")
			if len(got) <= len(prefix) || subtle.ConstantTimeCompare([]byte(got[len(prefix):]), []byte(s.authToken)) != 1 {
				writeError(w, http.StatusUnauthorized, "unauthorized")
				return
			}
		}
		next(w, r)
	}
}

func (s *Server) ListenAndServe(ctx context.Context) error {
	listener, err := net.Listen("tcp", s.config.ListenAddr)
	if err != nil {
		return err
	}
	s.listener = listener
	s.logger.Info("listening", "addr", s.Addr())

	go func() {
		<-ctx.Done()
		s.Stop()
	}()

	err = s.httpServer.Serve(listener)
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

func (s *Server) Stop() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return s.httpServer.Shutdown(ctx)
}

func (s *Server) Addr() string {
	if s.listener != nil {
		return s.listener.Addr().String()
	}
	return s.config.ListenAddr
}

func (s *Server) handleSubmit(w http.ResponseWriter, r *http.Request) {
	atomic.AddInt64(&s.submitTotal, 1)

	var req SubmitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		atomic.AddInt64(&s.submitError, 1)
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	var messages []filter.Message
	for _, m := range req.Messages {
		messages = append(messages, filter.Message{
			Role:     m.Role,
			Content:  m.Content,
			Metadata: m.Metadata,
		})
	}

	resp, err := s.harness.Submit(r.Context(), messages, harness.WithModelOverride(req.ModelOverride))
	if err != nil {
		if errors.Is(err, harness.ErrBusy) {
			atomic.AddInt64(&s.submitBusy, 1)
			writeError(w, http.StatusTooManyRequests, err.Error())
			return
		}
		atomic.AddInt64(&s.submitError, 1)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, SubmitResponse{
		Content:    resp.Content,
		TokensUsed: resp.TokensUsed,
		Model:      resp.Model,
		Cached:     resp.Cached,
		Metadata:   resp.Metadata,
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	status := s.harness.Health()
	writeJSON(w, http.StatusOK, HealthResponse{
		State:            status.StateStr,
		StateDescription: status.State.String(),
		ModelName:        status.Model,
		UptimeSeconds:    int64(status.Uptime.Seconds()),
		Message:          status.Message,
	})
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.harness.Config())
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	status := s.harness.Health()
	writeJSON(w, http.StatusOK, map[string]any{
		"uptime_seconds": int64(status.Uptime.Seconds()),
		"state":          status.StateStr,
		"model":          status.Model,
		"submit_total":   atomic.LoadInt64(&s.submitTotal),
		"submit_busy":    atomic.LoadInt64(&s.submitBusy),
		"submit_error":   atomic.LoadInt64(&s.submitError),
	})
}

func (s *Server) handleSetFilters(w http.ResponseWriter, r *http.Request) {
	var req SetFilterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// trusted=false: filter entries arriving over the network may not choose
	// which binary the RTK filter executes.
	if err := s.harness.SetFiltersFromConfig(req.Stage, req.Filters, false); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, SetFilterResponse{Success: true, Message: "filters updated"})
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, ErrorResponse{Error: msg})
}
