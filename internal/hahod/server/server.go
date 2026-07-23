// Package server is hahod's loopback HTTP surface (RFC section 8): the
// haho CLI and the client library are thin wrappers over exactly these
// routes. On the wire, protojson — a task body IS a haho.job.v1.JobSpec,
// and a completion reads back byte-identical to what landed.
package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"time"

	"google.golang.org/protobuf/encoding/protojson"

	"github.com/janearc/haho/gen/jobpb"
	"github.com/janearc/haho/internal/hahod/admission"
	"github.com/janearc/haho/internal/hahod/runner"
	"github.com/janearc/haho/internal/hahod/store"
)

// maxSpecBytes bounds a task body. A spec is a descriptor plus a payload;
// 32 MiB accommodates a large transcript while keeping a runaway body from
// exhausting the daemon.
const maxSpecBytes = 32 << 20

type Server struct {
	store   *store.Store
	adm     *admission.Admission
	log     *slog.Logger
	addr    string          // the advertised base for status addresses
	baseCtx context.Context // bounds every run; the daemon's lifetime, not a request's
	started time.Time
	metrics *metrics
}

func New(ctx context.Context, st *store.Store, adm *admission.Admission, log *slog.Logger, addr string) *Server {
	return &Server{
		store:   st,
		adm:     adm,
		log:     log,
		addr:    addr,
		baseCtx: ctx,
		started: time.Now(),
		metrics: &metrics{},
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /sessions", s.handleNew)
	mux.HandleFunc("POST /sessions/{id}/task", s.handleTask)
	mux.HandleFunc("GET /sessions/{id}", s.handleStatus)
	mux.HandleFunc("GET /sessions/{id}/completion", s.handleCompletion)
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("GET /metrics", s.metrics.handler)
	return s.logged(mux)
}

func (s *Server) logged(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		s.log.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"remote", r.RemoteAddr,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	})
}

// handleNew creates a quiescent session: it exists before its task arrives,
// and that gap lives in hahod's store (RFC section 8).
func (s *Server) handleNew(w http.ResponseWriter, r *http.Request) {
	id, err := newSessionID()
	if err != nil {
		s.fail(w, http.StatusInternalServerError, "mint session id: %v", err)
		return
	}
	if _, err := s.store.Create(id); err != nil {
		s.fail(w, http.StatusInternalServerError, "create session: %v", err)
		return
	}
	s.metrics.sessionsCreated.Add(1)
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "created": true})
}

// handleTask is admission's door. The body is a JobSpec in protojson,
// strict: an off-contract field never arrives. A refusal is loud, specific,
// and spends nothing.
func (s *Server) handleTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sess, err := s.store.Get(id)
	if err != nil {
		s.notFoundOr500(w, id, err)
		return
	}
	if sess.State != store.StateCreated {
		s.fail(w, http.StatusConflict, "session %s is %s; a session is tasked exactly once", id, sess.State)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxSpecBytes+1))
	if err != nil {
		s.fail(w, http.StatusBadRequest, "read task body: %v", err)
		return
	}
	if len(body) > maxSpecBytes {
		s.fail(w, http.StatusRequestEntityTooLarge, "task body exceeds %d bytes", maxSpecBytes)
		return
	}
	spec := &jobpb.JobSpec{}
	if err := protojson.Unmarshal(body, spec); err != nil {
		s.fail(w, http.StatusBadRequest, "task body is not a haho.job.v1.JobSpec: %v", err)
		return
	}

	ticket, refusal := s.adm.Admit(spec)
	if refusal != nil {
		s.metrics.jobsRefused.Add(1)
		s.log.Warn("job refused", "session", id, "job_id", spec.GetJobId(),
			"code", refusal.Code, "reason", refusal.Reason)
		status := http.StatusUnprocessableEntity
		if refusal.Code == "queue_full" {
			status = http.StatusTooManyRequests
		}
		writeJSON(w, status, map[string]any{
			"id": id, "accepted": false, "refused": true,
			"code": refusal.Code, "reason": refusal.Reason,
		})
		return
	}

	first := store.StateRunning
	if ticket.Queued {
		first = store.StateQueued
	}
	if _, err := s.store.Transition(id, first, func(sess *store.Session) {
		sess.JobID = spec.GetJobId()
	}); err != nil {
		ticket.Release()
		s.fail(w, http.StatusInternalServerError, "record tasking: %v", err)
		return
	}
	s.metrics.jobsAdmitted.Add(1)
	s.log.Info("job admitted", "session", id, "job_id", spec.GetJobId(),
		"lane", ticket.Lane.String(), "queued", ticket.Queued)

	go s.execute(id, spec, ticket)

	writeJSON(w, http.StatusAccepted, map[string]any{
		"id": id, "accepted": true,
		"status": fmt.Sprintf("%s/sessions/%s", s.addr, id),
	})
}

// execute runs the admitted job to its landing. It owns the ticket: the
// lane slot is held for exactly the run and released on every path.
func (s *Server) execute(id string, spec *jobpb.JobSpec, ticket *admission.Ticket) {
	if ticket.Queued {
		ticket.Wait()
		if _, err := s.store.Transition(id, store.StateRunning, nil); err != nil {
			// The row disagrees with the machine; surface it and put the
			// slot back — never run against a state we cannot record.
			s.log.Error("queued session cannot enter running", "session", id, "error", err)
			ticket.Release()
			return
		}
	}
	defer ticket.Release()

	s.metrics.jobsInFlight.Add(1)
	// The daemon's lifetime is the bound here, not a request context: the
	// requestor already got its 202 and reads the landing from the store.
	completion := runner.Run(s.baseCtx, spec)
	s.metrics.jobsInFlight.Add(-1)
	s.metrics.landed(completion.GetOutcome())

	raw, err := protojson.Marshal(completion)
	if err != nil {
		s.log.Error("marshal completion", "session", id, "error", err)
		raw = []byte(fmt.Sprintf(`{"jobId":%q,"outcome":"FAILED","reason":"completion failed to marshal"}`, spec.GetJobId()))
	}
	if _, err := s.store.Transition(id, store.StateLanded, func(sess *store.Session) {
		sess.Completion = raw
	}); err != nil {
		s.log.Error("record landing", "session", id, "error", err)
		return
	}
	s.log.Info("job landed", "session", id, "job_id", spec.GetJobId(),
		"outcome", completion.GetOutcome().String(), "wall_ms", completion.GetWallMs())
}

// handleStatus answers the RFC's status shape: id, in_flight, disposition.
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sess, err := s.store.Get(id)
	if err != nil {
		s.notFoundOr500(w, id, err)
		return
	}
	disposition := ""
	if sess.State == store.StateLanded {
		var landed struct {
			Outcome string `json:"outcome"`
		}
		// Stored completions are protojson we wrote; a parse failure here is
		// corruption and worth being loud about rather than shrugging blank.
		if err := json.Unmarshal(sess.Completion, &landed); err != nil {
			s.fail(w, http.StatusInternalServerError, "stored completion for %s does not parse: %v", id, err)
			return
		}
		disposition = landed.Outcome
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":          id,
		"in_flight":   sess.State == store.StateQueued || sess.State == store.StateRunning,
		"disposition": disposition,
	})
}

func (s *Server) handleCompletion(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sess, err := s.store.Get(id)
	if err != nil {
		s.notFoundOr500(w, id, err)
		return
	}
	if sess.State != store.StateLanded {
		s.fail(w, http.StatusNotFound, "session %s is %s; no completion has landed", id, sess.State)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(sess.Completion)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"healthy":        true,
		"uptime_seconds": int(time.Since(s.started).Seconds()),
		"pid":            os.Getpid(),
	})
}

func (s *Server) notFoundOr500(w http.ResponseWriter, id string, err error) {
	if errors.Is(err, store.ErrNotFound) {
		s.fail(w, http.StatusNotFound, "no session %s", id)
		return
	}
	s.fail(w, http.StatusInternalServerError, "read session %s: %v", id, err)
}

// fail answers with a structured error and logs it: the surface stays
// JSON everywhere, and nothing fails silently.
func (s *Server) fail(w http.ResponseWriter, status int, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	s.log.Warn("request failed", "status", status, "error", msg)
	writeJSON(w, status, map[string]any{"error": msg})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// newSessionID mints "haho-<pid>-<5 hex>", the RFC's session id shape.
func newSessionID() (string, error) {
	var b [3]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return fmt.Sprintf("haho-%d-%s", os.Getpid(), hex.EncodeToString(b[:])[:5]), nil
}
