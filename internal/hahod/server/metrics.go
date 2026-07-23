package server

import (
	"fmt"
	"net/http"
	"sync/atomic"

	"github.com/janearc/haho/gen/jobpb"
)

// metrics is the /metrics surface, prometheus text exposition, hand-held
// atomics — a handful of counters is not worth a client library. Every
// daemon on this mesh exposes /health and /metrics; this is hahod's.
type metrics struct {
	sessionsCreated      atomic.Int64
	jobsAdmitted         atomic.Int64
	jobsRefused          atomic.Int64
	jobsInFlight         atomic.Int64
	landedProduced       atomic.Int64
	landedPartial        atomic.Int64
	landedFailed         atomic.Int64
	landingWriteFailures atomic.Int64
}

func (m *metrics) landed(outcome jobpb.JobCompletion_Outcome) {
	switch outcome {
	case jobpb.JobCompletion_PRODUCED:
		m.landedProduced.Add(1)
	case jobpb.JobCompletion_PARTIAL:
		m.landedPartial.Add(1)
	default:
		m.landedFailed.Add(1)
	}
}

func (m *metrics) handler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	fmt.Fprintf(w, "# TYPE hahod_sessions_created_total counter\n")
	fmt.Fprintf(w, "hahod_sessions_created_total %d\n", m.sessionsCreated.Load())
	fmt.Fprintf(w, "# TYPE hahod_jobs_admitted_total counter\n")
	fmt.Fprintf(w, "hahod_jobs_admitted_total %d\n", m.jobsAdmitted.Load())
	fmt.Fprintf(w, "# TYPE hahod_jobs_refused_total counter\n")
	fmt.Fprintf(w, "hahod_jobs_refused_total %d\n", m.jobsRefused.Load())
	fmt.Fprintf(w, "# TYPE hahod_jobs_in_flight gauge\n")
	fmt.Fprintf(w, "hahod_jobs_in_flight %d\n", m.jobsInFlight.Load())
	fmt.Fprintf(w, "# TYPE hahod_jobs_landed_total counter\n")
	fmt.Fprintf(w, "hahod_jobs_landed_total{outcome=\"PRODUCED\"} %d\n", m.landedProduced.Load())
	fmt.Fprintf(w, "hahod_jobs_landed_total{outcome=\"PARTIAL\"} %d\n", m.landedPartial.Load())
	fmt.Fprintf(w, "hahod_jobs_landed_total{outcome=\"FAILED\"} %d\n", m.landedFailed.Load())
	fmt.Fprintf(w, "# TYPE hahod_landing_write_failures_total counter\n")
	fmt.Fprintf(w, "hahod_landing_write_failures_total %d\n", m.landingWriteFailures.Load())
}
