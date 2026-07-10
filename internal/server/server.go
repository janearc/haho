package server

import (
	"context"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/janearc/haho/internal/config"
	"github.com/janearc/haho/internal/filter"
	"github.com/janearc/haho/internal/harness"
)

type Server struct {
	harness    *harness.Harness
	config     *config.Config
	httpServer *http.Server
	logger     *log.Logger
	listener   net.Listener
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
	Stage   string                `json:"stage"` // "input" or "output"
	Filters []config.FilterEntry  `json:"filters"`
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
		harness: h,
		config:  cfg,
		logger:  log.New(os.Stderr, "[haho:server] ", log.LstdFlags),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/submit", s.handleSubmit)
	mux.HandleFunc("GET /v1/health", s.handleHealth)
	mux.HandleFunc("GET /v1/config", s.handleConfig)
	mux.HandleFunc("POST /v1/filters", s.handleSetFilters)

	s.httpServer = &http.Server{
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 5 * time.Minute,
	}

	return s
}

func (s *Server) ListenAndServe(ctx context.Context) error {
	listener, err := net.Listen("tcp", s.config.ListenAddr)
	if err != nil {
		return err
	}
	s.listener = listener
	s.logger.Printf("listening on %s", s.Addr())

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
	var req SubmitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
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

	resp, err := s.harness.Submit(r.Context(), messages)
	if err != nil {
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

func (s *Server) handleSetFilters(w http.ResponseWriter, r *http.Request) {
	var req SetFilterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if err := s.harness.SetFiltersFromConfig(req.Stage, req.Filters); err != nil {
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
