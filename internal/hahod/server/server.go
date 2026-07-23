// Package server is hahod's loopback HTTP surface (RFC section 8). The
// haho CLI and client library, when built, will be thin wrappers over
// exactly these routes; today the PoC binary of the same name speaks the
// old control API and is not that CLI. On the wire, protojson — a task
// body IS a haho.job.v1.JobSpec, and a completion reads back
// byte-identical to what landed.
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
	"sync"
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
	wg      sync.WaitGroup         // every execute goroutine; Drain joins them
	idFn    func() (string, error) // session id mint; swappable in tests
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
		idFn:    newSessionID,
	}
}

// Drain joins every in-flight execute goroutine, bounded by ctx. Call
// after http.Server.Shutdown has RETURNED (not merely begun) and before
// the store closes: Shutdown returning is what guarantees every handler
// reached its wg.Add, so no execute goroutine can be born after the wait
// starts. With the base context cancelled, runs land FAILED quickly and
// nothing exits half-recorded; a ctx expiry here means a run ignored its
// context — the caller logs it loudly and abandons.
func (s *Server) Drain(ctx context.Context) error {
	done := make(chan struct{})
	go func() { s.wg.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// InFlight reports the running-job gauge; the drain-timeout log names it.
func (s *Server) InFlight() int64 { return s.metrics.jobsInFlight.Load() }

// RecoverOrphans is the method form of the startup sweep: it lands the
// orphans and counts them on this process's metrics, so a scraper can
// reconcile recovered rows against the landed counters.
func (s *Server) RecoverOrphans() (int, error) {
	n, err := RecoverOrphans(s.store, s.log)
	s.metrics.orphansRecovered.Add(int64(n))
	return n, err
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
// and that gap lives in hahod's store (RFC section 8). The id space is 20
// bits behind a constant pid and the store is never pruned (the sweeper is
// future work), so collisions are expected at scale — the mint retries
// loudly instead of surfacing a spurious 500.
func (s *Server) handleNew(w http.ResponseWriter, r *http.Request) {
	const mintAttempts = 8
	for range mintAttempts {
		id, err := s.idFn()
		if err != nil {
			s.fail(w, http.StatusInternalServerError, "mint session id: %v", err)
			return
		}
		_, err = s.store.Create(id)
		if err == nil {
			s.metrics.sessionsCreated.Add(1)
			writeJSON(w, http.StatusCreated, map[string]any{"id": id, "created": true})
			return
		}
		if errors.Is(err, store.ErrExists) {
			s.log.Warn("session id collision, re-minting", "id", id)
			continue
		}
		s.fail(w, http.StatusInternalServerError, "create session: %v", err)
		return
	}
	s.fail(w, http.StatusInternalServerError, "session id space exhausted after %d mints; the store needs pruning", mintAttempts)
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

	// The Transition is the atomic claim on the session: of two racing
	// tasks both passing the state check above, exactly one claims it here;
	// the loser is the client's conflict (409), never a server fault, and
	// its ticket settles cleanly (queue position surrendered, no slot
	// touched). Known, accepted edge: between the loser's Admit and its
	// Release the lane briefly counts a phantom reservation, so a third
	// job arriving at exactly the queue boundary can draw a spurious
	// queue_full — a client double-tasking narrows the queue for the
	// duration of one bolt write, and that is the client's fault priced
	// at one transient 429, not machinery here.
	queued := ticket.Queued()
	first := store.StateRunning
	if queued {
		first = store.StateQueued
	}
	if _, err := s.store.Transition(id, first, func(sess *store.Session) {
		sess.JobID = spec.GetJobId()
	}); err != nil {
		ticket.Release()
		switch {
		case errors.Is(err, store.ErrIllegalTransition):
			s.fail(w, http.StatusConflict, "session %s already claimed: %v", id, err)
		case errors.Is(err, store.ErrNotFound):
			s.fail(w, http.StatusNotFound, "no session %s", id)
		default:
			s.fail(w, http.StatusInternalServerError, "record tasking: %v", err)
		}
		return
	}
	s.metrics.jobsAdmitted.Add(1)
	s.log.Info("job admitted", "session", id, "job_id", spec.GetJobId(),
		"lane", ticket.Lane.String(), "queued", queued)

	s.wg.Add(1)
	go s.execute(id, spec, ticket, queued)

	writeJSON(w, http.StatusAccepted, map[string]any{
		"id": id, "accepted": true,
		"status": fmt.Sprintf("%s/sessions/%s", s.addr, id),
	})
}

// execute runs the admitted job to its landing. It owns the ticket (the
// lane claim settles on every path) and it is tracked by the drain group:
// an admitted job always lands a completion — PRODUCED, or FAILED with the
// specific reason, including "the daemon shut down" — before the daemon
// exits.
func (s *Server) execute(id string, spec *jobpb.JobSpec, ticket *admission.Ticket, queued bool) {
	defer s.wg.Done()
	defer ticket.Release()

	if queued {
		if err := ticket.Wait(s.baseCtx); err != nil {
			// Shutdown reached a job still waiting on a slot: it never
			// runs, and that fact lands loudly instead of stranding the
			// session queued forever.
			s.land(id, &jobpb.JobCompletion{
				JobId:   spec.GetJobId(),
				Outcome: jobpb.JobCompletion_FAILED,
				Reason:  fmt.Sprintf("the daemon shut down before a lane slot freed: %v", err),
			})
			return
		}
		if _, err := s.store.Transition(id, store.StateRunning, nil); err != nil {
			// The row disagrees with the machine; surface it loudly —
			// never run against a state we cannot record.
			s.log.Error("queued session cannot enter running", "session", id, "error", err)
			s.metrics.landingWriteFailures.Add(1)
			return
		}
	}

	s.metrics.jobsInFlight.Add(1)
	// The daemon's lifetime is the bound here, not a request context: the
	// requestor already got its 202 and reads the landing from the store.
	completion := runner.Run(s.baseCtx, spec)
	s.metrics.jobsInFlight.Add(-1)
	s.land(id, completion)
}

// land records the completion. Metrics count a landing only once the store
// write succeeds — /metrics and the session row never disagree; a failed
// landing write is its own loud counter and log line.
func (s *Server) land(id string, completion *jobpb.JobCompletion) {
	raw, err := protojson.Marshal(completion)
	if err != nil {
		s.log.Error("marshal completion", "session", id, "error", err)
		raw = fmt.Appendf(nil, `{"jobId":%q,"outcome":"FAILED","reason":"completion failed to marshal"}`, completion.GetJobId())
	}
	if _, err := s.store.Transition(id, store.StateLanded, func(sess *store.Session) {
		sess.Completion = raw
	}); err != nil {
		s.log.Error("record landing", "session", id, "error", err)
		s.metrics.landingWriteFailures.Add(1)
		return
	}
	s.metrics.landed(completion.GetOutcome())
	s.log.Info("job landed", "session", id, "job_id", completion.GetJobId(),
		"outcome", completion.GetOutcome().String(), "wall_ms", completion.GetWallMs())
}

// RecoverOrphans lands every queued or running row as FAILED, loudly. It
// runs once at daemon startup: a row in flight when no flight exists is a
// crash's leftover, and presenting it as alive would be smuggled
// degradation.
func RecoverOrphans(st *store.Store, log *slog.Logger) (int, error) {
	rows, err := st.List()
	if err != nil {
		return 0, fmt.Errorf("recovery scan: %w", err)
	}
	recovered := 0
	for _, row := range rows {
		if row.State != store.StateQueued && row.State != store.StateRunning {
			continue
		}
		completion := &jobpb.JobCompletion{
			JobId:   row.JobID,
			Outcome: jobpb.JobCompletion_FAILED,
			Reason:  "hahod restarted before this job landed; the flight is lost",
		}
		raw, err := protojson.Marshal(completion)
		if err != nil {
			return recovered, fmt.Errorf("marshal recovery completion for %s: %w", row.ID, err)
		}
		if _, err := st.Transition(row.ID, store.StateLanded, func(sess *store.Session) {
			sess.Completion = raw
		}); err != nil {
			return recovered, fmt.Errorf("land orphan %s: %w", row.ID, err)
		}
		log.Warn("orphaned session landed FAILED at startup",
			"session", row.ID, "job_id", row.JobID, "was", string(row.State))
		recovered++
	}
	return recovered, nil
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
		// 409, not 404: the session exists and a poller must be able to
		// tell "still in flight" from "no such session" without parsing
		// error prose.
		s.fail(w, http.StatusConflict, "session %s is %s; no completion has landed", id, sess.State)
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
