# holden — RFC

- Status: DRAFT. Ratification by the operator and fable marks this file
  read-only, as is the usual convention. holden ratifies as one tranche
  with the haho RFC (`docs/haho-rfc.md`) and the speedy RFC
  (`docs/speedy-rfc.md`): the three documents share one vocabulary and
  one worldview, and none of them ratifies alone.
- Date: 2026-07-12.
- Authors: Max (operator); Claude (Fable 5, drafting). Review happens on
  the PR.
- Provenance: this document **continues ADR-0003** (janearc/holden,
  `docs/ADR-0003-holden-is-a-service.md`), which revised ADR-0001's
  mechanics. The ADR remains in holden's repo as the decision record,
  with a status pointer here; this RFC is the design reference. The
  design lives in this repo because holden, haho, and speedy were
  converged as one piece and are ratified as one piece — a design that
  was cut together should not be maintained apart.
- Naming: holden catches diffs before they go over the cliff. That is the
  whole job description, and the repo is named for it.
- Companions: `docs/haho-rfc.md` (the job contract, hahod, chutes, lanes —
  this document consumes those definitions rather than restating them);
  `docs/speedy-rfc.md` (rulings are *not* speedy work; section 4 says
  why); janearc/holden ADR-0001 (the coding-process gates this service
  enforces).

## 1. What holden is

The judge grew up in the sprints repo as a hand-run CLI: an operator types
a command, a subprocess running a very powerful code-assessing model
renders a verdict, a JSON ruling lands in the sprint ledger, a commit
status gates the merge. ADR-0001's design has held — writer is never
judge, done is external, rulings are schema'd artifacts — and that spine
carries forward unchanged. What we outgrew is the mechanics around it:
invocation only by a human at this keyboard, a model reached only as a
hardwired subprocess, no way for anything else on the mesh to request a
ruling or observe one happening.

holden becomes a persistent service — alive, heartbeating, containerized —
with two protobuf contract surfaces: an inbound ruling surface that holden
owns, and the haho job contract (`haho.job.v1`, defined in haho's RFC),
which haho owns and holden merely consumes. Each assessment is one haho
job: an ephemeral chute executes it with whatever model the spec names,
reports completion, and is gone; holden never touches a model directly.

## 2. The spine kept from ADR-0001, verbatim

- **Fresh judgment per ruling.** Every ruling is a fresh model instance
  with no memory of the diff's authoring. Unchanged (section 3 records
  the resident-judge reconciliation).
- **Done is external** (ADR-0001 Decision 3). Mechanical gates green, a
  ratify ruling on the head SHA, operator sign-off. Nothing here relaxes
  that.
- **Rulings are fixed-schema artifacts** in a durable ledger, enforced on
  GitHub as the `ruling/ratify` commit status. The schema is unchanged;
  it gains a protobuf representation so it can travel a wire as well as
  sit in a file.
- **The test goalpost is the design doc** (ADR-0001 Decision 8). This
  document names its own end-to-end proof in section 8.

## 3. What "service" means here (the resident-judge reconciliation)

ADR-0001 explicitly struck resident judge processes: long-lived sessions
rot, and judgment stored in a transcript is judgment lost. That ruling
stands.

holden-the-service holds **no judgment context**. The resident process is
dispatch and bookkeeping: it accepts ruling requests, assembles inputs (as
`assemble.rs` does today), submits one job to haho per request, validates
what comes back against the ruling schema, writes the ledger, posts the
status, and emits lifecycle events. The chute that executes the job lives
exactly as long as the job — that is haho's guarantee, enforced by its
resident daemon (hahod), not by holden's discipline: a chute that no
longer exists cannot carry context into the next ruling. What stays
resident is plumbing; what stays ephemeral is judgment. A holden that has
been up for a month rules exactly as a holden started this morning.

holden runs as a host-level operator container, on the delightd
precedent. Its silicon story is deliberately empty: holden reaches models
only through hahod's loopback API, so nothing in holden needs Metal, the
ANE, or an Apple framework, and it containerizes cleanly. It stays
host-level rather than running as a fleet pod for delightd's own reason —
a gate that rules every merge on the fleet's repos must not be supervised
by the thing it gates — and because hahod's API binds loopback on the
host.

## 4. holden and speedy: a ruling is not speedy work

An earlier draft called rulings "speedy's null case." That framing was
wrong, and the correction is worth stating precisely because it defends
both designs' edges:

- **No recipe.** Nothing composes into a ruling and nothing composes out
  of one. A ruling is not a step in any itinerary; no frood's output ever
  causes one (the frood law holds at this gate like everywhere else —
  ruling requests enter the mesh from outside it: a human, a CI event, an
  operator-agent).
- **No dial.** holden is requested, never released-into. It is not an
  addressable step in anyone's flow.
- **One job, minted by the requestor.** Each assessment is a single haho
  job — `WHOLE_OR_ERROR`, `allow_partial: false`, api lane — submitted,
  watched, landed. No itinerary, no hop arithmetic, no heap.

speedy's machinery — recipes, dials, the heap, convergence — simply never
touches a ruling, and nothing in this document depends on that machinery
existing. The three designs meet at exactly one seam: holden is a haho
requestor, and haho separately implements speedy for chutes. That is the
entire relationship.

## 5. Two contract surfaces, two owners

**Inbound — `holden.ruling.v1`, owned by holden.** The requestor-facing
surface:

- `SubmitRuling(RulingRequest) returns (RulingHandle)` — repo, PR number,
  includes; returns a ruling id immediately.
- `GetRuling(RulingHandle) returns (Ruling)` — the ruling document, or
  its in-flight state.
- `WatchRulings(WatchRequest) returns (stream RulingEvent)` — holden
  emits; requestors and health-watchers observe instead of polling.

`Ruling` is the protobuf twin of the ADR-0001 ruling schema — `diff_ref`,
`judge_instance`, `fired_at`, verdict enum, divergences, `shape_verdict`,
consumer impact with file:line evidence, `doc_content_agreement` — the
proto authoritative once this lands, serialized as JSON at rest and on any
human-facing surface. `RulingEvent` covers the lifecycle: RECEIVED,
INPUTS_ASSEMBLED, JUDGE_SPAWNED, VERDICT, REFUSED_RETRY, PUBLISHED,
FAILED. Consumers vendor-generate, per mesh practice. Bus emission
(kafka) is explicitly a later seam — kafka-svc owns bus contracts, and
wiring holden into the bus is a separate decision this document names
rather than smuggles.

**Outbound — the haho job contract (`haho.job.v1`), owned by haho.**
holden is a haho requestor, and deliberately the simplest one haho's RFC
names. Per assessment:

- holden opens a session and submits **one job** through the haho client
  (`haho new` / `haho task` / `haho status` are the operator-facing
  verbs; holden links the Go client over hahod's loopback API). The
  JobSpec is `WHOLE_OR_ERROR` with `allow_partial` pinned false — a
  ruling prompt is never chunked, and a partial ruling is not a ruling —
  on the api lane. haho's worked-spec section shows the exact spec.
- While the job runs, holden relays haho's JobEvents onto `WatchRulings`.
  Liveness of in-flight work is haho's to prove — hahod's heartbeats and
  the hm lease chain mean a hung assessment surfaces as a lapsed lease —
  so holden carries no watchdog of its own; it consumes the events and
  waits.
- The JobCompletion's payload is validated against the ruling schema on
  holden's side of the contract. A completion that does not parse as a
  ruling is a refusal to be handled, never output to be trusted.

Concurrent rulings are simply concurrent jobs: admission control and lane
budgets are hahod's, and holden holds no dispatch logic at all. Nor is
haho holden-exclusive — anything on the mesh with model work submits
under the same contract; holden assumes nothing about being alone.

Which model answers, and how the chute executes, is haho's business and
invisible to holden; haho defines itself in its own RFC, not here. The
current `spawn.rs` claude-CLI subprocess survives only as the pre-haho
shim, until hahod serves.

holden assesses; haho furnishes. holden never defines a model contract,
and haho never learns what a ruling is: holden validates the completion
record's payload against the ruling schema on its own side of the pipe.
That is the whole division of labor, and it is enforced by the wire.

## 6. Resources and health

Per fleet rules, holden gets `/health` and `/metrics`, structured JSON
logging, heartbeats emitted on `WatchRulings` (so a watcher can tell
idle from dead), and a readiness check that means something:

| Check | Green means |
|-------|-------------|
| `hahod_reachable` | hahod answers its health endpoint and admits work; in-flight chute liveness is carried by hahod's heartbeats and the hm lease chain, so holden never polls a chute — a lapsed lease is the hung-assessment alarm |
| `delightd_reachable` | the roster endpoint answers; holden can resolve consumers |
| `ledger_writable` | the rulings directory accepts a write |
| `publisher_ready` | GitHub API reachable for status posting; if not, holden serves but reports DEGRADED, loudly — rulings queue rather than vanish |

Config resolves once at startup, flag over env over default (the existing
`Config` boundary). The stale `~/work/sprints` default WILL be fixed
before holden is considered stable. holden carries no credentials of its
own; the chute owns model auth via the spec's env references (haho's
security posture), and the `gh` layer owns GitHub auth, exactly as today.

## 7. Migration, ordered

1. Contracts first: `holden.ruling.v1` proto lands with this tranche
   ratified; generated code follows the gen-freshness invariant (ADR-0001
   Decision 5).
2. Core refactor: the existing crate's assemble/rule/publish path is
   wrapped by the service; the CLI remains as a thin client of the same
   core (rehearsal and hostile-network modes keep working).
3. `spawn.rs` is replaced by the haho client: open a session, submit the
   JobSpec, relay events, take the completion. The claude-CLI subprocess
   survives only as the shim behind that seam until hahod serves.
4. haho's ratification gates holden serving in production. The repo
   relocation already happened (haho lives in `~/mesh/prod/haho` as of
   2026-07-12); what remains of "graduation" is this tranche's
   ratification and haho's staged end-to-end proofs — a production gate
   does not take a hard dependency on an unratified contract. Until
   then, holden-as-a-service runs against the shim.

## 8. The end-to-end proof (ADR-0001 Decision 8 compliance)

Submit a ruling for a real PR through `SubmitRuling`, watch the lifecycle
on `WatchRulings`, and observe the `ruling/ratify` status appear on the
PR's head SHA and the ruling row appear in the ledger. Requestor talks to
holden, holden talks to hahod, the gate comes out. If that flow cannot be
demonstrated, this design is not done, whatever the coverage number says.

## 9. Carried, not solved here

- sprints issue 52: judge non-determinism (a required field dropped twice
  on a 1686-line diff, clean on respawn). Service-ization makes retries
  and validation observable, which should make this bug diagnosable; it
  does not fix it by itself.
- Bus emission of ruling events (kafka-svc seam, later pilot).
- Where the ledger lives long-term. Today it stays in the sprints repo;
  the destination is decided — it must be surrealdb — and the move gets
  its own ADR once the RAG layer exists.

## 10. Consequences

**Gained.** Anything on the mesh can request and observe a ruling; the
model seam is swappable without touching holden; the judge — which
currently guards all the production repos — finally has a health surface
of its own; the laptop-path era ends.

**Paid.** Two contract surfaces to keep gen-fresh; a daemon to run and
watch where a CLI used to suffice; haho's ratification and proofs now sit
on holden's critical path; the CLI-only escape hatch must keep working
through the transition, which is a real maintenance tax. The price of
growth is sprawl and overhead. But without growth there is no glory.
