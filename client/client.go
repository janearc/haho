// Package client is the committed Go client for hahod's loopback API
// (RFC section 8): dial, new, task, status, completion — no process
// execution, no shell. The haho CLI is a thin wrapper over exactly this
// package, and any Go requestor on the mesh links it rather than
// hand-rolling the wire. Bodies are strict protojson for contract
// messages; the session envelope is the hand-shaped JSON the README's
// divergence list records.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"google.golang.org/protobuf/encoding/protojson"

	"github.com/janearc/haho/gen/jobpb"
)

// Refused is admission's loud no, surfaced to the requestor exactly as
// the daemon said it: stable code, specific reason. It is an error so a
// requestor cannot mistake it for transport trouble.
type Refused struct {
	Code   string
	Reason string
}

func (r *Refused) Error() string { return fmt.Sprintf("REFUSED (%s): %s", r.Code, r.Reason) }

type Client struct {
	base string
	http *http.Client
}

// Dial points the client at hahod. addr is the base URL
// (http://127.0.0.1:8790); the generous timeout covers task submission,
// not job execution — jobs land in the store and are polled, never held
// on a socket.
func Dial(addr string) *Client {
	return &Client{base: addr, http: &http.Client{Timeout: 30 * time.Second}}
}

// New creates a quiescent session and returns its id.
func (c *Client) New(ctx context.Context) (string, error) {
	body, err := c.post(ctx, "/sessions", nil)
	if err != nil {
		return "", err
	}
	var resp struct {
		ID      string `json:"id"`
		Created bool   `json:"created"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("client: new: %w", err)
	}
	if !resp.Created || resp.ID == "" {
		return "", fmt.Errorf("client: new: daemon answered without a session: %s", body)
	}
	return resp.ID, nil
}

// Task submits a JobSpec to a session. Admission errors surface here,
// loudly, as *Refused; acceptance returns the status address.
func (c *Client) Task(ctx context.Context, session string, spec *jobpb.JobSpec) (string, error) {
	raw, err := protojson.Marshal(spec)
	if err != nil {
		return "", fmt.Errorf("client: marshal spec: %w", err)
	}
	return c.TaskRaw(ctx, session, raw)
}

// TaskRaw submits an already-serialized protojson JobSpec. The bytes are
// validated client-side first — an off-contract spec never even leaves
// the requestor.
func (c *Client) TaskRaw(ctx context.Context, session string, specJSON []byte) (string, error) {
	if err := protojson.Unmarshal(specJSON, &jobpb.JobSpec{}); err != nil {
		return "", fmt.Errorf("client: spec is not a haho.job.v1.JobSpec: %w", err)
	}
	body, err := c.post(ctx, "/sessions/"+session+"/task", specJSON)
	if err != nil {
		return "", err
	}
	var resp struct {
		Accepted bool   `json:"accepted"`
		Status   string `json:"status"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("client: task: %w", err)
	}
	if !resp.Accepted {
		return "", fmt.Errorf("client: task: daemon answered without acceptance: %s", body)
	}
	return resp.Status, nil
}

// Status reports a session: whether work is in flight, and the
// disposition once landed ("" until then).
func (c *Client) Status(ctx context.Context, session string) (inFlight bool, disposition string, err error) {
	body, err := c.get(ctx, "/sessions/"+session)
	if err != nil {
		return false, "", err
	}
	var resp struct {
		InFlight    bool   `json:"in_flight"`
		Disposition string `json:"disposition"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return false, "", fmt.Errorf("client: status: %w", err)
	}
	return resp.InFlight, resp.Disposition, nil
}

// Completion fetches the landed JobCompletion. Before landing, the
// daemon answers 409 and this surfaces as an error naming the state.
func (c *Client) Completion(ctx context.Context, session string) (*jobpb.JobCompletion, error) {
	body, err := c.get(ctx, "/sessions/"+session+"/completion")
	if err != nil {
		return nil, err
	}
	completion := &jobpb.JobCompletion{}
	if err := protojson.Unmarshal(body, completion); err != nil {
		return nil, fmt.Errorf("client: completion is not a haho.job.v1.JobCompletion: %w", err)
	}
	return completion, nil
}

// Wait polls Status until the session lands or ctx ends, then returns
// the completion. The interval is the poll cadence; the daemon holds no
// socket open for this (packets, not streaming).
func (c *Client) Wait(ctx context.Context, session string, interval time.Duration) (*jobpb.JobCompletion, error) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		_, disposition, err := c.Status(ctx, session)
		if err != nil {
			return nil, err
		}
		if disposition != "" {
			return c.Completion(ctx, session)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

func (c *Client) post(ctx context.Context, path string, body []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+path, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("client: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(req)
}

func (c *Client) get(ctx context.Context, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return nil, fmt.Errorf("client: %w", err)
	}
	return c.do(req)
}

func (c *Client) do(req *http.Request) ([]byte, error) {
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("client: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, fmt.Errorf("client: read response: %w", err)
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return body, nil
	}
	// A refusal is a structured answer, not transport trouble; surface it
	// as itself. Anything else is the daemon's loud error body.
	var refusal struct {
		Refused bool   `json:"refused"`
		Code    string `json:"code"`
		Reason  string `json:"reason"`
	}
	if json.Unmarshal(body, &refusal) == nil && refusal.Refused {
		return nil, &Refused{Code: refusal.Code, Reason: refusal.Reason}
	}
	return nil, fmt.Errorf("client: %s: %s", resp.Status, bytes.TrimSpace(body))
}
