# haho 🪂 — RFC

- Status: DRAFT. Ratification by the operator and fable marks this file
  read-only, and the read-only file is the reference for the integrity of the
  project from then on, as is the usual convention. Ratification is currently
  BLOCKED on one dependency: the speedy RFC (section 17).
- Authors: Max (operator); Claude (Fable 5, drafting). Review happens on the
  PR.
- Companion: holden `docs/ADR-0003-holden-is-a-service.md` (holden is haho's
  first consumer). Project names used throughout are defined in section 18.

Contents:

1. What haho is
2. The discernment equation
3. The principle: borg-shaped, fruit-flavored
4. Execution lanes
5. Placement: virtualization, containerization, and isolation
6. Residency
7. The lifecycle of a chute
8. The programming interface
9. Framing and the coherence window
10. Dispatch and lanes
11. Sidecars and haho
12. The first consumers
13. Observability
14. Security posture
15. Graduation and the end-to-end proof
16. Non-goals
17. Open questions
18. Definitions and prior work
19. References
- Appendix A: the 70x, reproducibly

## 1. What haho is

haho runs jobs. A job is one unit of model work: transcribe this audio,
smooth this transcript, review this diff, distill this document. A requestor
describes the job in a typed spec. haho decides nothing about what the job
means; it executes the job the way the spec says, against the hardware the
spec names, and reports the result.

The reason haho exists is cost. The same unit of work can be done many ways —
on the GPU, on the Neural Engine, on a local model, or by a paid API on the
other side of the network — and the difference between the right way and the
wrong way is not a percentage. It is the difference between feasible and
infeasible (section 4, and Appendix A: 70x, measured on this hardware), or
between free and $2.58 per diff review, every diff, forever. haho is the
piece of software that lets every pipeline on this mesh do its model work the
right way without hand-wiring hardware knowledge into each one.

haho is Apple end to end, and that is its primary defining characteristic.
This machine is one SoC whose GPU, Neural Engine, and CPU share unified
memory, with an OS that keeps a foundation model warm and offers
transcription and translation as system services. Everything in this document
follows from taking that hardware seriously. On the day the hardest pipeline
on the roadmap is built — live interpreting of a room, described in section
12 — its mesh side reduces to two haho job templates and a dispatch loop.
That reduction is the point of the design.

holden is the first consumer, and it doubles as the integration test for
everything after it: one job, one API-backed model, no deadline. The
consumers after holden (section 12) use progressively more of the interface,
up to deadline-bound work across every lane at once.

## 2. The discernment equation

Every job haho runs is an instance of one question:

> What is the fastest way to accomplish this unit of work with a reasonable
> degree of accuracy, such that the value of the work is proportionate to
> the cost of the processing needed to accomplish it?

Stated as the equation the rest of this document keeps referring to. For a
unit of work `w` and a set of candidate placements `P` (the lanes of section
4, plus the paid APIs):

```text
feasible(w) = { p in P : capable(p, w)  and  eta(w, p) <= deadline(w) }

place(w)    = argmax over p in feasible(w) of

                 value(w) * accuracy(w, p)
                 -------------------------
                       cost(w, p)

where cost(w, p) = dollars(p) + energy(p) + occupancy(w, p)
```

`occupancy` is the opportunity cost of holding a contended lane (section
10): a job that pins the GPU for ten seconds costs every other GPU job ten
seconds, and the equation must see that. `accuracy` is fitness for purpose,
not a benchmark score: a transcription that misses proper nouns may still be
fit for cutting audio into segments and unfit for a permanent record.

Choosing a placement by this equation is **discernment**. Discernment is the
combination of dispatch, orchestration, and scheduling: given a job, decide
where it runs. In this version of haho, discernment is **external**: the
requestor works the equation and writes the answer into the job spec (which
backend, which framing, which deadline). haho enforces the answer — admission
control, lane budgets, deadlines — but does not second-guess it. Internal
discernment, where haho itself works the equation from the job's shape, is a
future capability and is out of scope here.

Two worked examples of the equation, because it is the whole design in
miniature:

- "Review this 4,000-line diff adversarially." No local model is capable;
  `feasible` contains only the paid APIs. Discernment says: send it to
  claude, pay, and do not pretend otherwise.
- "Transcribe this 45-second audio segment." The GPU does it in about a
  second for free; a paid API does it slower for money; torch on CPU takes
  so long the answer arrives after it stopped mattering. Discernment says:
  metal lane, and the deadline drops the result if it is ever late.

A unit of work moving through haho is called a **chute** — the word lines up
with "pipeline," and with the parachute the project is named for. One chute,
one jump, one landing.

## 3. The principle: borg-shaped, fruit-flavored

haho tries to be two things at once, and the tension between them is the
design:

- It wants to be kubernetes-natured: typed contracts, scheduled placement,
  observable execution, auditable results. Backend software that behaves at
  scale.
- It wants to be a well-behaved Apple-ecosystem program: native processes,
  system frameworks, the hardware's own fast paths. Software of the kind
  Apple designed this machine to run cheaply — such programs almost never
  leave the device and have no concept of a bus.

No existing runtime is both. Kubernetes-shaped tooling on this platform puts
work inside Linux virtual machines where none of the Apple hardware or
frameworks exist (section 5). Apple-native software has no job contracts, no
scheduler, no fleet. We need a name for the combination, so: **borg-shaped,
fruit-flavored**. Borg-shaped is the grammar — the following are assumptions
of this document, not aspirations:

- Work is described by typed specs, never by convention.
- Execution emits events; completions are durable records.
- Placement is a scheduling decision against declared resources.
- Health is an endpoint.
- Everything can be observed while running and audited after landing.

Fruit-flavored is the physics: the resources under the scheduler are one
SoC's execution units and one OS's frameworks, bound tightly enough that a
transcription job and an interpretation job can plausibly run at the same
time without slowing each other down — on different execution units of the
same chip (section 4 states this precisely, including what is hypothesis and
what is fact). The phrase "tight fruit binding" appears throughout this
document's ancestry and means exactly this: the design leans on Apple's
hardware-software integration on purpose, because that integration is where
the cost term of the discernment equation collapses.

This bet is directional, and it should be stated as one. The on-device
foundation model is small today. Apple's trajectory — the hardware, the
Foundation Models framework, the curated open models AOT-compiled for the
platform[^5] — points at bigger models on the same free, warm, local terms.
Building haho against that trajectory is a mortgage, not a sunk cost: the
payments are the constraint-work in this document, and what the payments buy
is that every capability Apple ships lands in an already-built harness with
its cost term already near zero. If we did not believe the trajectory, the
honest alternative would be a cloud provider and none of this.

What this is not, precisely:

- **As described and implemented, this is not a distributed system.** The
  pieces that would allow one exist — the bus, framing, external caches —
  but at n>1 the design philosophy changes so substantially that haho as
  specified would be moot: a rack of machines does not need carefully
  discerned chutes, it needs a real cluster scheduler. haho is an n=1
  design, and the equation's cost term is computed for one machine's
  resources.
- **Compute stays home; participation does not.** Mobile, wearable, and
  ambient devices participate in the mesh — a phone on the same wifi asks
  this machine to do work the phone cannot, through an ephemeral cloudflared
  tunnel (intruder, section 18) — but the work executes here. The phone is a
  requestor, not a worker. The equation covers this case too: same-owner
  same-network hardware is a placement whose dollars are zero and whose
  latency is wifi.
- **The cloud is not the enemy.** Discernment sends work to claude or
  deepseek when no local placement is capable or timely. What the design
  refuses is *defaulting* to paid placement for work the machine does
  cheaply.

The failure this principle exists to prevent is a design failure, not a
runtime one, and it is the failure that necessitated haho: on this platform
there is no Dockerfile, no container runtime, no virtualization product that
grants access to the hardware Apple carefully designed to run these
workloads cheaply. Kubernetes is good. Containers are good. Apple silicon is
good. Nothing ships that lets one piece of software have all three, so haho
is the piece of software that arranges them: kubernetes-shaped where the
fleet lives, native where the work lives, contracts across the seam.

## 4. Execution lanes

Work placement on this machine, by hardware path:

| Lane | Entered through | Runs | Character |
|---|---|---|---|
| metal (GPU) | MLX, via a resident model server | whisper large-v3, mlx-lm models | highest local throughput; contended — budgets in section 10; the 70x side of Appendix A[^8] |
| ane (Neural Engine) | AFM (Foundation Models)[^4], Speech (`SpeechTranscriber`), Translation, CoreML[^5] | the on-device foundation model[^10]; streaming transcription; on-device machine translation | designed to run concurrently with the GPU; low power; small coherence windows (section 9) |
| api (network) | HTTP to a model endpoint[^7] | claude, deepseek, anything with a served model | capable of what no local model is; costs dollars and round trips |
| cpu | plain code | chunking, filters, synthesis assembly, voice-activity detection (VAD — deciding where speech starts and stops) | reliable glue |

torch does not appear in this table on purpose: haho is fruit-native by
definition, and torch on this hardware loses the cost equation by roughly
two orders of magnitude (Appendix A). A job that only torch could run is a
job for the api lane.

Three statements about the lanes, with their epistemic status:

1. **Fact.** The non-api lanes open only from native macOS processes. MLX,
   AFM, Speech, Translation, and CoreML are macOS frameworks; they do not
   exist inside a kubernetes-resident container (which on this platform is a
   Linux virtual machine, section 5), and a virtualized macOS cannot reach
   the Neural Engine at all.
2. **Hypothesis, to be proven.** The GPU and the Neural Engine execute
   concurrently without meaningful contention — whisper on metal while AFM
   interprets on the ane, neither queueing behind the other. The hardware
   has separate execution units and this is the design intent of the SoC,
   but Apple's frameworks arbitrate access and nothing guarantees the
   absence of internal serialization. Graduation proof 3 (section 15) exists
   to convert this hypothesis into a measurement before anything
   deadline-bound depends on it. Note the promise being made is **speedy —
   near-real-time — never realtime** (speedy is a defined term, section 18).
3. **Fact.** The Neural Engine has no direct programming interface — there
   is no instruction stream or bytecode a process can hand it. CoreML, AFM,
   and Speech compile and place work on it at the framework's discretion; a
   process asks for the task, and Apple's runtime decides what silicon
   serves it.[^9] haho therefore schedules *lanes*, and accepts Apple's
   arbitration underneath them.

## 5. Placement: virtualization, containerization, and isolation

Three shapes were evaluated for where a chute's process runs.[^1][^2]

| Shape | Metal | ANE / AFM / Speech | Spawn cost | Verdict |
|---|---|---|---|---|
| kubernetes-resident container — on this platform, a Linux micro-VM (`apple/container`[^1], Docker) | no | no; macOS frameworks do not exist in the guest | sub-second | rejected for chutes; this is where fleet services live |
| macOS virtual machine (Virtualization.framework) | paravirtualized, graphics-oriented; compute throughput unproven[^6] | no Neural Engine; Apple Intelligence and AFM do not function in VMs[^4] | seconds | rejected for chutes |
| **native macOS process, isolated by sandbox profile** | full | full | milliseconds | **the shape** |

The container row is the decisive one. There is no GPU passthrough in
Apple's container stack — the maintainers state it plainly, and Apple's GPUs
are not behind an IOMMU, so classic passthrough may never come.[^2]
Community projects broker MLX inference from guests to a host daemon over
vsock at about 95% of native throughput,[^3] which is good engineering and
also a concession of the point: the model work happens host-side anyway, so
haho puts the whole chute host-side and skips the brokering.

Virtualization.framework still informs the design. The paravirtualized GPU
it gives macOS guests *brokers* Metal to shared hardware rather than handing
over the device — brokered access, never passthrough, is how this platform
shares silicon, and haho's resident-model-server arrangement (section 6)
follows the same grain. The macOS license also caps virtualized macOS at two
instances per host,[^6] which matters to nobody at n=1 but confirms VMs were
never the fan-out story.

**Isolation, then, is the Solaris-zones kind, not the hypervisor kind.** A
chute's process is told: here is your filesystem slice, here is your network
reach, act like that is the whole world — and the real kernel and the real
silicon sit directly underneath. No guest OS, no virtual devices, no second
kernel. On macOS the mechanism is a sandbox profile, generated per job from
the spec, plus a per-job scratch directory and an environment constructed
from the spec alone (nothing inherited). Illustrative profile:

```scheme
;; chute sandbox (illustrative; generated per-job from the JobSpec).
;; The spawning side passes HAHO_ROOT / SCRATCH / API_HOST as sandbox
;; parameters at process launch; they are not ambient.
(version 1)
(deny default)
(allow file-read*  (subpath (param "HAHO_ROOT"))      ; the versioned bundle
                   (subpath (param "SCRATCH")))
(allow file-write* (subpath (param "SCRATCH")))
(allow network-outbound
  (remote tcp "127.0.0.1:*")       ; resident model servers, furnished caches
  (remote tcp (param "API_HOST"))) ; present only when the spec names an api
                                   ; backend; set to the spec's base_url host,
                                   ; nothing wider
;; plus the framework services the declared lanes require, and no others
```

Read the last allowance carefully, because it is the security model in one
line: a job's network reach is derived from its spec. A spec that names no
api backend produces a profile with no rule for outbound traffic beyond
loopback — the chute can reach the resident model servers and the furnished
caches on this machine, and nothing else on any network. The scratch
directory is created at process launch and deleted at exit.

### What haho is not

- Not a container runtime and not a VM manager. It launches sandboxed
  processes.
- Not a model server. Models are resident and owned elsewhere (section 6).
- Not a pipeline framework. Pipelines (birbs, section 18) remain services;
  haho executes their jobs.
- Not a sidecar, and not a replacement for sidecars (section 11).
- Not a cluster scheduler, per section 3.

## 6. Residency

**Models are resident. Work is ephemeral.**

whisper large-v3 takes seconds to load and pins gigabytes of unified memory.
It cannot be loaded per chunk, and it must not die between segments 17 and
18 of a stream. AFM is resident because the OS keeps the foundation model
warm — a direct consequence of the platform choice. Speech and Translation
likewise. So the resident layer is small and specific:

- **The MLX model server** — a host process holding whisper and any mlx-lm
  models warm on the metal lane, serving loopback HTTP. This is owned by the
  mesh's existing Apple-silicon Swift work in big-little-mesh
  (`big-little-mesh/apple-silicon`[^11]); haho is a client of it, not an
  owner. It holds no job state.
- **AFM, Speech, Translation** — resident inside macOS; a chute calls the
  framework.
- **Furnished state** — surrealdb, redis, and whatever else `meubilair.yaml`
  declares (section 18). This is a direct payoff of being borg-shaped: the
  fleet furnishes a resource as a container with an exposed port, quiescent
  containers cost almost nothing, and haho exposes the furnished resource to
  any chute whose spec asks for it. haho is, in this one respect, a mirror
  of meubilair: meubilair furnishes resources to the fleet; haho hands them
  to jobs. The cache list in the contract (section 8) is therefore open, not
  exhaustive — chromadb or anything else furnishable joins by yml fragment,
  not by design change.

A chute's process is then genuinely cheap: milliseconds to launch, no
weights to load, gone at the completion record. The process itself holds
nothing across jobs. Continuity between jobs is a designed feature of the
*mesh*, not of haho: a pipeline's product can flow to the next pipeline over
the bus, carried by an itinerary with a bounded number of hops (magpie
finishes, neal picks up magpie's product). haho's guarantee is narrower and
exact: whatever a chute accumulated in-process — context, model state,
temperature — ends when the chute lands. What persists is what was
deliberately written to a furnished store or published to the bus.

## 7. The lifecycle of a chute

A HAHO jump opens high and rides a long descent to a precise landing. A
chute lives exactly one jump.

1. **Creation.** The harness accepts a `JobSpec` and admits it against lane
   budgets (section 10). Jobs are immutable once created and accepted to
   run: there is no reconfiguration of a job in flight. The creation record
   carries the job id and the chute's status address (loopback, ephemeral
   port).
2. **Descent.** The chute is queryable but not directable: the status
   endpoint answers with state, time in flight, progress, lane and model in
   use — read-only, for the whole working life. Progress events flow to the
   harness (section 13). Work can take a long time; a living chute can
   always say so.
3. **Landing.** One terminal `JobCompletion` — outcome, result, model used,
   chunk and cache statistics, wall time. The process exit status is the
   coarse cross-check: a completion/exit disagreement is treated as FAILED
   by the harness regardless of what either says alone. The scratch
   directory is deleted; the process is gone.

Chutes are plural: one chute per unit of work, arbitrarily many in flight,
bounded by lane budgets rather than by the contract.

## 8. The programming interface

### The harness and its clients

haho runs as one resident harness per machine — `hahod` — and this is a
deliberate design decision rather than a drift from ephemerality: **the
harness is resident; every chute is ephemeral.** Two things force it. Lane
budgets are per-machine facts, so admission control must live at one point
that sees every job (section 10); and sessions need somewhere to exist
between creation and tasking. The harness holds session and queue state
(bbolt-shaped, on disk, survivable); it does no model work; each admitted
job still runs as its own sandboxed process with its own lifetime.

Clients speak to the harness over loopback HTTP. The `haho` CLI is a thin
wrapper over the same API:

```console
$ haho new                          # create a quiescent session
{"id": "haho-6722-a60e2", "created": true}

$ haho task haho-6722-a60e2 job.yml # the session jumps when tasked
{"id": "haho-6722-a60e2", "accepted": true, "status": "127.0.0.1:52341"}

$ haho status haho-6722-a60e2
{"id": "haho-6722-a60e2", "in_flight": false, "disposition": "PRODUCED"}
```

Programmatically, a requestor uses the client library — no process
execution, no shell:

```go
h, err := haho.Dial(ctx)                    // loopback; hahod's socket
if err != nil { ... }

session, err := h.New(ctx)
if err != nil { ... }

events, err := session.Task(ctx, spec)      // *jobpb.JobSpec; admission errors
if err != nil { ... }                       // surface here, loudly

for ev := range events {                    // creation, heartbeats, progress,
    relay(ev)                               // chunk misses, cache hits
}

done, err := session.Completion(ctx)        // the terminal record
```

Completions are also published to the bus (kafka) following the bento
convention, so a requestor that dies mid-descent — or a pipeline three hops
away — reads the landing from the mesh's durable record rather than from a
pipe that no longer exists. Requestor-side delivery and bus publication
carry the same record; the bus copy is the one of record.

### The contract

Contracts are protobuf (buf v2); consumers vendor-generate; gen-freshness
holds per the mesh's standing invariant. `haho.common.v1.ChatMessage` and
`haho.filter.v1` carry over from the existing proto tree. On the wire and at
rest, protojson. Anything llm-shaped in a spec — synthesis instructions,
prompt fragments — is a jinja2 template, stated here once as an explicit
expectation for the whole mesh.

```proto
syntax = "proto3";

package haho.job.v1;

option go_package = "github.com/janearc/haho/gen/jobpb;jobpb";

import "google/protobuf/timestamp.proto";
import "haho/common/v1/common.proto";
import "haho/filter/v1/filter.proto";

// Everything one unit of work needs, and nothing else. Immutable once the
// job is created and accepted to run.
message JobSpec {
  string job_id = 1;          // requestor-assigned; rides every event and the lease
  string correlation_id = 2;  // cross-pipeline correlation over the bus

  Payload payload = 3;
  Backend backend = 4;        // which model seam does the work (a descriptor)
  Framing framing = 5;        // how work is fitted to the model (section 9)
  Synthesis synthesis = 6;    // what happens to per-chunk results
  Cache cache = 7;
  haho.filter.v1.FilterPipelineConfig input_filters = 8;
  haho.filter.v1.FilterPipelineConfig output_filters = 9;
  Deadline deadline = 10;
  map<string, string> secret_env = 11; // logical name -> ENV VAR NAME.
                                       // References, never values (section 14).
                                       // The harness validates each named var
                                       // exists and is non-empty at admission.
  map<string, string> labels = 12;     // requestor-owned; mirrored to the lease
  bool allow_partial = 13;             // a degraded or low-confidence result is
                                       // acceptable. holden sets false: a ruling
                                       // is whole or it is not a ruling.
}

message Payload {
  oneof body {
    Messages messages = 1;  // chat-shaped work
    string   text     = 2;  // a transcript, a diff, a document
    bytes    raw      = 3;  // audio and other binary stage input
  }
}
message Messages { repeated haho.common.v1.ChatMessage messages = 1; }

// Backend is a descriptor: it names a model seam and how to reach it. The
// job carries the descriptor; the chute constructs the actual client from
// it at launch. Lane is not a field — it is derived from kind (metal for
// MLX, ane for AFM/SPEECH/TRANSLATION, api for the *_COMPATIBLE kinds) and
// a kind/lane mismatch is therefore impossible to express.
message Backend {
  Kind kind = 1;
  enum Kind {
    KIND_UNSPECIFIED     = 0;
    ANTHROPIC_COMPATIBLE = 1; // api lane: claude, deepseek, ...
    OPENAI_COMPATIBLE    = 2; // api lane: the /api/chat shape; local ollama too
    AFM                  = 3; // ane lane: Apple Foundation Models, on-device
    MLX                  = 4; // metal lane: the resident MLX server
    SPEECH               = 5; // ane lane: SpeechTranscriber
    TRANSLATION          = 6; // ane lane: on-device machine translation.
                              // Reserved room below: translation will need
                              // language-pair and glossary fields.
  }
  string base_url = 2;      // api kinds and the MLX server
  string model = 3;         // where the kind takes one
  string token_env = 4;     // ENV VAR NAME of the credential; empty = none
  // params carries backend-specific knobs that are not contract-worthy
  // (whisper's task=translate, an api's temperature). Keys are defined by
  // the backend implementation and validated there; the contract
  // deliberately does not enumerate them.
  map<string, string> params = 5;
  reserved 6 to 9;          // TRANSLATION language pairs, future kinds' needs
}

// Lane is the hardware path (section 4). Derived from Backend.kind;
// declared here because dispatch (section 10) budgets by lane.
enum Lane {
  LANE_UNSPECIFIED = 0;
  LANE_METAL = 1;
  LANE_ANE   = 2;
  LANE_API   = 3;
  LANE_CPU   = 4;
}

// Framing: how work is fitted to a model whose coherence window (section 9)
// may be smaller than the work.
message Framing {
  Strategy strategy = 1;
  enum Strategy {
    STRATEGY_UNSPECIFIED = 0;
    WHOLE_OR_ERROR = 1;  // one attempt; a window miss is the requestor's problem
    RESIZE_ON_FAIL = 2;  // try whole; on a window miss, chunk and continue
    CHUNK_ALWAYS   = 3;  // never try whole; this is the map of mapesis
  }
  Chunker chunker = 2;
  enum Chunker {
    CHUNKER_UNSPECIFIED = 0;
    TOKENS            = 1; // FUTURE: requires a per-backend tokenizer story
                           // (section 17); do not implement first
    SENTENCES         = 2;
    MARKDOWN_SECTIONS = 3; // input is treated as text; recognized markdown is
                           // cut on section boundaries; anything else is
                           // wrapped exhaustively in xml and cut on elements
                           // (wonderlib's rule, carried forward)
    LINES             = 4;
  }
  uint32 chunk_size = 3;             // unit per chunker
  uint32 chunk_overlap = 4;
  uint32 max_attempts_per_chunk = 5; // re-read on fail (section 9)
  Backoff backoff = 6;
  message Backoff {
    uint32 initial_ms = 1;
    double multiplier = 2;
    uint32 max_ms = 3;
  }
}

// The trailing synthesis step over per-chunk results. For MAPESIS the
// per-chunk outputs are assembled, in chunk order, into a single Messages
// payload; the instruction template (jinja2) renders with the outputs bound
// to `chunks` and the result is the synthesis model's prompt. CONCAT is an
// ordered join with no model. NONE returns the per-chunk results as-is.
message Synthesis {
  Mode mode = 1;
  enum Mode {
    MODE_UNSPECIFIED = 0;
    NONE    = 1;
    CONCAT  = 2;
    MAPESIS = 3;
  }
  Backend backend = 2;      // MAPESIS only; often a wider window than the map
  string instruction = 3;   // jinja2 template; this is a prompt, not prose
}

// Cache: memoization of model calls. Before a chunk is sent to a model, the
// chute computes key = hash(backend kind, model, model_fingerprint, params,
// filtered chunk) and asks the furnished store; a hit skips the model call
// entirely. model_fingerprint is reported by the backend (for AFM it
// incorporates the OS build, since the OS updates the model) so a model
// upgrade cannot serve stale answers — see section 17.
message Cache {
  Kind kind = 1;
  enum Kind {
    KIND_UNSPECIFIED = 0;
    NONE      = 1;
    SURREALDB = 2;  // also the RAG store
    REDIS     = 3;  // plain hot KV
    // open set: anything meubilair furnishes can back a cache; new kinds are
    // additive, never breaking
  }
  uint32 ttl_seconds = 2; // 0 = no expiry
}

message Deadline {
  uint32 deadline_ms = 1; // 0 = none
  OnLate on_late = 2;
  enum OnLate {
    ON_LATE_UNSPECIFIED = 0;
    DROP   = 1; // freshness over completeness: a late answer is no answer
    FINISH = 2; // batch work: late is fine, record the overrun
  }
}
```

Events and the landing. Heartbeats reuse the mesh's existing heartbeat
contract rather than minting a new one — `observability.v1
.ServiceHealthHeartbeat` from big-little-mesh, excerpted here for the
reader:

```proto
// big-little-mesh: proto/observability/v1/observability.proto (excerpt)
message ServiceHealthHeartbeat {
  string service_name = 1;
  HealthState current_state = 2;
  uint32 uptime_seconds = 3;
  uint32 internal_load_metric = 4;
  google.protobuf.Timestamp timestamp = 5;
  string idempotency_key = 6; // duplicate delivery is safe
}
```

```proto
message JobEvent {
  string job_id = 1;
  google.protobuf.Timestamp at = 2;
  oneof event {
    Created   created   = 3; // the status address, once, at admission
    observability.v1.ServiceHealthHeartbeat heartbeat = 4;
    Progress  progress  = 5;
    ChunkMiss chunk_miss = 6; // parallel language to cache_hit: the window
                              // rejected this attempt (section 9)
    CacheHit  cache_hit = 7;
    Degraded  degraded  = 8;
  }
  message Created   { string status_addr = 1; }
  message Progress  { uint32 chunks_done = 1; uint32 chunks_total = 2; string stage = 3; }
  message ChunkMiss { string reason = 1; bool will_resize = 2; }
  message CacheHit  { uint32 chunk_index = 1; }
  message Degraded  { string what = 1; string why = 2; }
}

message JobCompletion {
  string job_id = 1;
  Outcome outcome = 2;
  enum Outcome {
    OUTCOME_UNSPECIFIED = 0;
    PRODUCED     = 1;
    PARTIAL      = 2; // only ever when the spec set allow_partial
    DROPPED_LATE = 3;
    FAILED       = 4; // with a specific reason; never fabricated output
  }
  string reason = 3;
  Payload result = 4;
  string model_used = 5;   // completions surface model, lane, and statistics —
  Lane lane_used = 6;      // never the backend descriptor or its references
  uint32 chunks = 7;
  uint32 cache_hits = 8;
  int32 tokens_in = 9;
  int32 tokens_out = 10;
  uint64 wall_ms = 11;
}
```

Mid-descent, anything on the machine can ask the chute directly:

```console
$ curl -s 127.0.0.1:52341/status
{"jobId":"interp-8841-s17","state":"busy","inFlightMs":1290,
 "stage":"map","chunksDone":2,"chunksTotal":4,"lane":"ane","model":"afm"}
```

### Three worked specs

holden's assessment. One job, api lane, whole or nothing:

```json
{
  "jobId": "ruling-delightd-121",
  "payload": {"messages": {"messages": [
    {"role": "user", "content": "<the assembled ruling prompt>"}]}},
  "backend": {"kind": "ANTHROPIC_COMPATIBLE", "model": "claude-fable-5",
              "baseUrl": "https://api.anthropic.com",
              "tokenEnv": "HOLDEN_ANTHROPIC_KEY"},
  "framing": {"strategy": "WHOLE_OR_ERROR"},
  "synthesis": {"mode": "NONE"},
  "cache": {"kind": "NONE"},
  "allowPartial": false
}
```

A wonderlib distillation. The requestor has decided — this is discernment,
worked externally — that no large local model comes up for this: the
document is chunked through the on-device foundation model, and those
per-chunk results are the map half of mapesis. The synthesis half runs on
the local ollama mistral — the sturdy workhorse[^5] — because the entire
point of this machinery is to avoid paying an api for work the machine does
free; the api lane is the escalation, not the default:

```json
{
  "jobId": "sig-corpus-c041",
  "payload": {"text": "<one corpus document>"},
  "backend": {"kind": "AFM"},
  "framing": {"strategy": "CHUNK_ALWAYS", "chunker": "MARKDOWN_SECTIONS",
              "chunkSize": 1, "maxAttemptsPerChunk": 3,
              "backoff": {"initialMs": 100, "multiplier": 2.0, "maxMs": 2000}},
  "synthesis": {"mode": "MAPESIS",
                "backend": {"kind": "OPENAI_COMPATIBLE",
                            "baseUrl": "http://127.0.0.1:11434",
                            "model": "dolphin-mistral-24b"},
                "instruction": "{# jinja2 #} Merge these per-section signatures into taxonometry rows and one synthesis section: {{ chunks }}"},
  "cache": {"kind": "SURREALDB", "ttlSeconds": 0}
}
```

mackerel's interpreter stage, deadline-bound:

```json
{
  "jobId": "interp-8841-s17",
  "correlationId": "sess-8841/seg-17",
  "payload": {"text": "<50-100 words of whisper output for this segment>"},
  "backend": {"kind": "AFM"},
  "framing": {"strategy": "RESIZE_ON_FAIL", "chunker": "SENTENCES",
              "chunkSize": 3, "maxAttemptsPerChunk": 2,
              "backoff": {"initialMs": 50, "multiplier": 2.0, "maxMs": 400}},
  "synthesis": {"mode": "MAPESIS",
                "backend": {"kind": "AFM"},
                "instruction": "{# jinja2 #} Merge these smoothed sentences; carry meaning, drop disfluency, spoken register: {{ chunks }}"},
  "cache": {"kind": "SURREALDB", "ttlSeconds": 900},
  "deadline": {"deadlineMs": 3000, "onLate": "DROP"}
}
```

The transcription job that feeds it is one more spec, not a special case:
`backend {kind: MLX, model: "whisper-large-v3", params {task: "translate"}}`,
payload `raw` audio, framing `WHOLE_OR_ERROR` (the client cut the segment to
size before sending it), deadline `DROP`.

Pluggability is exactly the enum surfaces: backends, chunkers, synthesis
modes, cache kinds, filters. Each is a Go interface with a registry behind
it; adding one is one implementation and one enum value, and no requestor
changes. Filters deserve one definition here since they sit in the critical
path: a filter transforms chunk text before the model sees it (context
reduction, redaction, normalization), it runs per chunk, and it runs
*before* the cache key is computed — the cache memoizes what the model was
actually asked.

## 9. Framing and the coherence window

Every model has a **coherence window**: the largest input it handles while
its output remains trustworthy. Past the window the failure is not always
loud — output degrades, drops requirements, or wanders — which is why the
window is defined by *trustworthiness*, not by the advertised context size.
The on-device foundation model has a small window (it fails visibly on a
100-line markdown file). claude's is enormous. Framing is how haho fits work
to whatever window the chosen backend has, and it is the direct descendant
of what wonderlib built (section 18): profile the corpus, cut the hard parts
smaller, feed a small model many digestible pieces, synthesize once at the
end. The economics carry over unchanged: holden chunks diffs through local
models today not because claude cannot read a diff whole, but because $2.58
per review is not a price you pay at a merge gate.

The ladder, as a chute executes it:

```go
func (f *framing) run(ctx context.Context, job *Job) (*Result, error) {
    if f.strategy != ChunkAlways {
        res, err := f.attempt(ctx, job.whole())
        if err == nil {
            return res, nil
        }
        if f.strategy == WholeOrError || !isWindowMiss(err) {
            return nil, err // real failures propagate; window misses resize
        }
        emit(ChunkMiss{Reason: err.Error(), WillResize: true})
    }

    chunks := f.chunker.cut(job)
    outs := make([]*Out, len(chunks))
    for i, c := range chunks {
        if ctx.Err() != nil { // the deadline owns this loop
            return nil, ctx.Err()
        }
        if hit, ok := f.cache.get(c.key()); ok {
            outs[i] = hit
            emit(CacheHit{ChunkIndex: uint32(i)})
            continue
        }
        out, err := f.attemptWithRetry(ctx, c) // re-read on fail; backoff on distress
        if err != nil {
            return nil, fmt.Errorf("chunk %d/%d: %w", i+1, len(chunks), err)
        }
        outs[i] = out
        f.cache.put(c.key(), out)
        emit(Progress{ChunksDone: uint32(i + 1), ChunksTotal: uint32(len(chunks)), Stage: "map"})
    }
    return f.synthesize(ctx, outs) // NONE | CONCAT | MAPESIS
}
```

The mechanisms, made concrete enough to implement:

- **Window-miss detection is per-backend, and each backend implementation
  MUST declare its miss signals.** For api kinds: the provider's
  context-length error class, and truncated output. For AFM: the framework's
  size/refusal errors, and empty output on non-empty input. For MLX: the
  server's declared limits. A window miss resizes (when the strategy allows);
  every other error — auth, network, a resident server down — propagates
  immediately and honestly.
- **Re-read on fail.** Before an output is accepted, a validator asks two
  questions: is the output well-formed for the job's declared output shape
  (jinja2-templated jobs declare one; plain-text jobs get the degenerate
  check of non-empty, non-error-phrase output), and does it address the
  chunk it was given. A failed re-read counts against
  `max_attempts_per_chunk` and triggers backoff.
- **Model distress is measured, not guessed**, and this RFC defines the
  measurement: the re-read failure rate over the last k attempts on
  well-formed chunks. Rising distress engages the backoff multiplier; the
  requestor sees it as `Degraded` events, never as silently worse output.
- **The window itself gets a benchmark.** `haho bench` measures, per
  backend and per hardware, the coherence boundary: declare N as the
  maximum input (in the chunker's units) before the re-read failure rate
  crosses a threshold, and fit the decoherence coefficient D — the rate at
  which trustworthiness falls as input grows past N. Default `chunk_size`
  per chunker ships from those measurements. The same benchmark re-runs on
  new hardware and new model releases (an M5 Max, a bigger foundation
  model) so the defaults track the platform instead of fossilizing.
- **The cache key** is `hash(backend kind, model, model_fingerprint, params,
  filtered chunk)` — content-addressed, so identical work costs one model
  call until TTL, across all chutes. The fingerprint keeps a model upgrade
  from serving yesterday's answers (section 17). In a live room this is not
  an optimization: announcements repeat, names repeat, and a repeat is a
  deadline met for free.

mapesis (lowercase in prose; `MAPESIS` only in the contract) is the map half
plus the synthesis half: chunk outputs are the map, the synthesis pass —
usually on a wider-window, cheaper-per-token lane than the map — produces
the merged artifact. Map on the small free lane, synthesize once on the
sturdy one, escalate to the api lane only when the equation says so.

## 10. Dispatch and lanes

Dispatch in haho is borg in miniature, and it lives in exactly one place:
the harness. An earlier draft put admission in a per-requestor library;
review caught the contradiction — two requestors each enforcing "one metal
tenant" locally admit two metal tenants globally. Budgets are per-machine
facts, so the machine gets one admission point (`hahod`, section 8), and
requestors are clients of it.

```json
{
  "lanes": {
    "metal": { "budget": 1,  "queue": 4  },
    "ane":   { "budget": 2,  "queue": 8  },
    "api":   { "budget": 16, "queue": 64 },
    "cpu":   { "budget": 8,  "queue": 32 }
  }
}
```

The metal budget of 1 is tied directly to the lane table's "contended"
entry (section 4), and "heavy" has a definition: a job is heavy on a lane
when its expected utilization of that lane approaches 1.0 — it saturates
the resource for its duration, so lane throughput is serial in heavy jobs.
One whisper stream is heavy on metal: `budget = floor(1 / utilization) =
1`. A future weight-class refinement can admit several *light* metal jobs
(small mlx-lm models at fractional utilization) under the same rule;
the budget arithmetic already accommodates it, and nothing changes at the
contract.

The ane budget is small because AFM's own concurrency is. The api budget is
wide because someone else owns those GPUs. A deadline-bearing job that
cannot be admitted before its deadline is DROPPED at admission — failing
before spending is the freshness rule applied to the queue itself, and it
requires exactly the single queue view that centralizing admission
provides.

That is the entire scheduler: a few dozen lines against a four-row resource
table, still recognizably the borg grammar — declared resources, admission
control, observable placement — at the scale the physics actually are.

## 11. Sidecars and haho

The mesh's pattern for resident host-native services is the sidecar: paling
runs its inference daemon host-side, owning its own models locally, and
`paling-sidecar` runs in the fleet carrying everything the mesh requires of
a frood — registration with delightd, health reporting, bus adjacency
(frood-compliance; the terms are defined in section 18). A sidecar exists
because a host-native resident cannot live inside the fleet, and the fleet
still needs it to behave like a frood.

haho covers the case the sidecar cannot: ephemeral work that needs the
hardware the fleet cannot reach. The two do not overlap, and haho makes the
boundary sharper than it was:

| | sidecar | chute |
|---|---|---|
| exists for | a resident service | one job |
| lifetime | the service | the job |
| carries | frood-compliance for its resident | a JobSpec and nothing else |
| runs as | fleet pod | sandboxed native process |
| created by | deployment, with its service | the harness, at admission |

A chute does not get a sidecar. Its fleet-side representation is the lease
the harness maintains for it (section 13), which costs nothing and dies with
the job. If anything, haho strengthens the sidecar's definition: a sidecar
is *only* the compliance carrier for a resident — any model work a resident
wants is a JobSpec like everyone else's.

## 12. The first consumers

Each consumer requires more of the interface than the one before, and each
is a real workload:

1. **holden** — one job, api lane, `WHOLE_OR_ERROR`, no deadline, no cache,
   `allow_partial: false` — a ruling is whole or it is not a ruling, so a
   degraded or low-confidence answer is never acceptable here. holden is the
   first service on haho and the integration test for the whole seam:
   haho, the delightd mesh, and the bus, proven end to end by the service
   that gates every merge.
2. **wonderlib** — batch distillation over a corpus: `CHUNK_ALWAYS`, AFM
   map, mapesis synthesis on the local mistral, cache with no expiry. The
   CLI's `--signature --all` and `--mapesis` modes become spec templates,
   and the library stops owning chunking and starts owning *strategies*.
3. **lloyd / neal** — smoothing as a seam. The flow lloyd wants is exactly
   the mapesis shape: a large body of text is chunked and synthesized, and
   the foundation model smooths the chunk-assembled synthesis —
   `RESIZE_ON_FAIL`, sentence chunking, ane lane. The alternative — asking a
   large paid model to smooth every voice memo — is the cost this machinery
   exists to refuse. neal runs the same shape over the operator's voice
   memos.
4. **mackerel / intruder / speedy** — the interpreting pipeline: three lanes
   at once, deadlines with DROP, the cache as deadline insurance,
   correlation ids threading the phone's capture through the mesh and back.
   Its mesh side reduces to two spec templates — transcribe on metal,
   interpret on ane — and a dispatch loop, which is the reduction section 1
   promised.

## 13. Observability

- **The event stream.** Creation, heartbeats (the mesh's standard
  `ServiceHealthHeartbeat`), progress, chunk misses, cache hits,
  degradations — delivered to the requestor over its harness connection.
- **The status endpoint.** Pull-based truth mid-descent, per chute,
  read-only, loopback (section 7).
- **The completion record.** Published to kafka following the bento
  convention at landing; the bus copy is the copy of record, and the
  requestor's synchronous copy is a convenience. The record carries model,
  lane, chunks, cache hits, tokens, and wall time — everything, because
  logs could not reconstruct the flight even in principle: the work happened
  on the metal, inside frameworks that do not narrate.
- **The lease.** Every chute has an explicit lease with hm, and the lease is
  a contract like anything else on this mesh. Mechanically it is a
  Kubernetes Lease object — a small fleet-side record with a holder and an
  expiry, which the holder must renew to keep; the fleet's standard
  liveness primitive, adopted for delightd's needs and reused here. The
  harness takes the lease at admission (labeled from the spec), renews it
  through the descent, and releases it at landing. The fleet therefore sees
  every host-side flight as a first-class object: dashboards list them, and
  a lease that lapses without a landing is, by construction, the alarm for
  a hung or orphaned flight — including the case where the harness itself
  died and cannot say so. Expiry is the garbage collection.
- **Structured JSON logging** throughout, per fleet rule; a bare print is a
  lint error.

## 14. Security posture

- Secret **values** MUST NOT appear in a JobSpec, an event, a completion, a
  log line, or this repo. Specs carry env var *names*; the operator
  provisions values (launchd environment, keychain-backed injection); the
  harness validates that every named variable exists and is non-empty at
  admission, and the chute reads them at launch. A named-but-empty
  reference is a loud admission failure, never a quiet degradation.
- The sandbox denies by default (section 5): read the versioned bundle and
  the scratch directory; write scratch only; loopback plus the spec's api
  host only; the environment is constructed from the spec's references and
  nothing else is inherited.
- The status endpoint is read-only and binds loopback. There is no mutating
  surface on a chute in flight; jobs are immutable once accepted.
- A chute holds only what its one job needs. A job with no api backend
  holds no credential and reaches nothing beyond loopback. The blast radius
  is one job, and then the process is gone.

## 15. Graduation and the end-to-end proof

haho is a dev project. It graduates to prod when this document is ratified
by the operator and fable and marked read-only at `docs/haho-rfc.md`, as is
the usual convention, and the ratified document is thereafter the reference
for the integrity of the project, as is the usual convention.

What ratification buys is the proof obligation. haho MUST be end-to-end
provable from the day it is promoted, even as the barest shell it will then
be: it must **loft** (admit a spec and answer on the chute's status
endpoint), **work** (drive a real backend through framing, with progress
events and cache statistics), and **land** (publish a completion record
that agrees with the process exit) — each step verifiable from outside, in
addition to whatever unit and coverage tests exist. Coverage with no flight
is not a tested harness.

The staged proof:

0. **hello world.** Before any named consumer: one trivial spec (`text` in,
   `text` out, cpu lane), submitted with `haho new` / `haho task`, observed
   on `/status`, landed on the bus. The smallest complete flight.
1. **The flight.** Loft, work, land against a real model backend.
2. **The cache.** Run a real job, then the identical job; the second
   completion shows `cacheHits > 0` and near-zero model time.
3. **The lanes.** A metal transcription job and an ane interpretation job
   in flight simultaneously, both meeting deadlines that either alone would
   meet. This is the measurement that converts section 4's concurrency
   hypothesis into fact — or fails to, in which case the speedy pipeline's
   design assumptions get revisited *before* anything is built on them.
4. **The ladder.** A document that misses the window whole is resized,
   mapped with at least one cache hit, synthesized via mapesis, and lands
   PRODUCED.
5. **The bench.** `haho bench` produces the coherence-window numbers (N and
   D, section 9) for this hardware and the resident backends, and the
   shipped defaults cite them.

## 16. Non-goals

- **n>1.** Section 3. The design philosophy would change so substantially
  at n>1 that haho as specified would be moot.
- **Containers or VMs as chute shapes.** Section 5. Fleet services stay in
  containers; chutes are native processes.
- **Resident chutes.** The harness is resident (it holds queues and
  sessions, and does no model work); a chute never is.
- **Owning models or weights.** The MLX server belongs to
  big-little-mesh's apple-silicon work; AFM belongs to the OS; ollama can
  be furnished if a job needs it. haho exposes furnished resources to jobs
  the way meubilair exposes them to the fleet; it owns none of them.
- **Internal discernment.** Version one enforces the requestor's placement;
  it does not compute placements itself (section 2).

## 17. Open questions

- **The speedy RFC — ratification blocker.** This document leans on speedy
  (deadline semantics, near-real-time promises, the deposit pattern) while
  speedy has no RFC of its own. That is not acceptable for ratification:
  haho cannot be ratified against an undefined dependency. Venue is decided
  (operator, 2026-07-11): speedy is an idea, not software — its build
  surface is proto changes, and haho is its primary driver — so its RFC
  lives in this repo, at `docs/speedy-rfc.md`. Writing it precedes
  ratification here.
- **Cache fingerprints per backend.** The key includes a model fingerprint;
  each backend must define one. api models report identifiers; MLX can
  fingerprint weights; AFM offers no version API, so its fingerprint
  incorporates the OS build — coarse but safe. Confirm per backend during
  implementation.
- **TOKENS chunking.** Requires a tokenizer per backend (tiktoken-style for
  api kinds, mlx-lm's for metal, nothing public for AFM). Marked FUTURE in
  the contract; SENTENCES and MARKDOWN_SECTIONS carry the near-term load.
- **Cancellation.** Jobs are immutable and chutes are not directable, so
  the only in-flight signal today is process termination by the harness
  (deadline enforcement). Whether requestors get a graceful cancel — and
  what "graceful" means mid-map — is deferred until a consumer needs it.
- **Coherence-window benchmarking method.** Section 9 defines N and D and
  `haho bench`; the fitting method and thresholds get designed against
  wonderlib's existing corpus runs, which are the natural instrument.
- **Vector retrieval for the RAG store.** The store today is BM25 in
  surrealdb: lexical search, which ranks documents by the words they
  actually share with the query. That matches how this mesh queries — code
  identifiers, error strings, names — where the exact token *is* the
  signal. What BM25 cannot do is find a document that says the same thing
  in different words: paraphrase retrieval requires embedding text into
  vectors and searching by semantic distance, which costs an embedding
  model in the loop, an index lifecycle, and a tuning surface. The trigger
  for paying that cost is a real workload that demonstrably misses with
  BM25 — corpus synthesis across restated sources is the expected first
  case. Until one exists, lexical stays.

## 18. Definitions and prior work

Definitions first, because the project names are deliberately whimsical and
the reader deserves a map. Whimsy is a tool here: it keeps long work light.

- **frood** — the mesh's pipeline library
  ([janearc/big-little-mesh](https://github.com/janearc/big-little-mesh)):
  contracts, registration, heartbeats, bus adjacency. A service built on it
  and meeting its behavioral requirements is **frood-compliant**.
- **birb** — a defined pipeline built on frood, with orchestration and
  scheduling, communicating with other pipelines only via the bus. Birbs
  never call birbs.
- **delightd** — the pipeline orchestrator
  ([janearc/delightd](https://github.com/janearc/delightd)): a host-level
  operator that drives the fleet (k3s) and furnishes it.
- **meubilair** — delightd's furniture manifest (`meubilair.yaml`): the
  declared set of resident resources (kafka, surrealdb, redis, grafana, and
  so on) the fleet runs. Adding a resource is a yml fragment.
- **bento** — the mesh's unit-of-work record convention: a durable,
  self-describing manifest of what a pipeline did. haho completions follow
  it on the bus.
- **hm** — hall-monitor, the fleet's watcher; holds the leases of section
  13.
- **holden** — the assessment service (the judgment gate formerly living in
  the sprints repo). haho's first consumer.
- **magpie** — transcription pipeline
  ([janearc/magpie](https://github.com/janearc/magpie)): voice memos to
  text, whisper large-v3 on MLX. The 70x measurement is magpie's (Appendix
  A):

  ```python
  # magpie/pipeline.py (excerpt)
  WHISPER_MODEL = "mlx-community/whisper-large-v3-mlx"

  import mlx_whisper
  result = mlx_whisper.transcribe(str(audio_path), **kwargs)
  ```

- **paling** — inference pipeline
  ([janearc/paling](https://github.com/janearc/paling)): a host-side daemon
  owning its own local models, with the fleet-side `paling-sidecar` — the
  sidecar pattern of section 11.
- **lloyd** — the smoothing pipeline (named from *Dumb and Dumber*): takes
  not-great text (raw transcription, raw machine translation) and shapes it.
- **liarbird** — text-to-audio; magpie in reverse.
- **neal** — interprets the operator's own voice memos back to him;
  batch-shaped.
- **cardinal** — the first effort to make the bird-pipelines use Apple
  hardware natively (cardinals are red; it was first). It proved the AFM
  seam this document builds on — the on-device foundation model behind a
  swappable backend, honestly gated:

  ```python
  # cardinal/backends/fm.py (excerpt)
  import apple_fm_sdk as fm

  ok, reason = fm.SystemLanguageModel().is_available()  # gated, honestly
  session = fm.LanguageModelSession()
  text = await session.respond(prompt)
  ```

- **turtledove** — voice-memo pipeline (so many birds already existed that
  the christmas carol supplied the name). Its contribution is the
  transcription trade study: `mlx-whisper` (metal, stronger accuracy)
  versus Apple Speech (`SpeechTranscriber`: ane, faster than realtime,
  weaker on proper nouns, fails outright on code-switching). Those failure
  modes become code paths, not dead ends — for example a second pass over
  the transcript with a local model and the audio's context as prompt.
  Which door a job uses is discernment, per stage.
- **wonder / wonderlib** — wonder (archived) attacked the 2024 problem:
  local fine-tuning was not viable and torch was unusable on this hardware,
  so the only path was profiling a corpus (taxonometry: find what is
  hardest to read), cutting the hard parts smaller, and feeding a very
  small model pieces it could digest. wonderlib is the successor library;
  its chunking, re-read, backoff, and caching are what section 9 promotes
  into the harness.
- **mackerel** — the live-interpreting pipeline. Named because babelfish is
  correct but not correct enough: babelfish is realtime, and **speedy**
  promises only near-real-time. mackerel is an ambient listener that emits
  translated speech and a scrolling transcript when enough information has
  passed through to render them.
- **grackle** — the image-generation flavor of mackerel (the words rhyme):
  ephemeral text-to-image in the same ambient mode, unbuilt.
- **intruder** — the participation seam: a phone or wearable joins the mesh
  through an ephemeral cloudflared tunnel for the life of a connection,
  deposits capture, and receives product. Client-side capture and cutting;
  the mesh does the work.
- **speedy** — the near-real-time processing mode and its semantics
  (deadlines, freshness over completeness, deposit of workloads between
  pipelines). RFC pending; see section 17.

Prior-work note rather than history lesson: the judge — holden's previous
life as a CLI in the sprints repo — ran fifteen sprints as a proof of
concept and earned its keep; as context sizes bloomed it became a single
point of failure on large diffs, and the choice was to fix that rather than
ship unreviewed diffs. Everything the judge could not do — framing, caching,
backend choice, events in flight — is this document.

## 19. References

[^1]: Apple, "Discover container machines," WWDC26 session 389.
    <https://developer.apple.com/videos/play/wwdc2026/389/> (retrieved
    2026-07-10). Micro-VM-per-container architecture; no GPU or Metal access
    for guests. Project: <https://github.com/apple/container> — worth
    tracking; GPU support landing there would revise section 5.

[^2]: apple/container, discussion 62, "GPU passthrough availability?"
    <https://github.com/apple/container/discussions/62> (retrieved
    2026-07-10). Maintainer: "we do not currently support this." Apple GPUs
    are not behind an IOMMU.

[^3]: RobotFlow-Labs, container-toolkit-mlx.
    <https://github.com/RobotFlow-Labs/container-toolkit-mlx> (retrieved
    2026-07-10). Host-side Swift daemon with native Metal access serving MLX
    over vsock; ~95% of native throughput; early-stage. The same
    socket-that-takes-work shape could serve as a sidecar transport if the
    fleet ever needs to reach the metal directly. Docker Model Runner does
    the same brokering at the API level:
    <https://www.docker.com/blog/docker-model-runner-vllm-metal-macos/>.

[^4]: Apple, "What's new in the Foundation Models framework," WWDC26
    session 241. <https://developer.apple.com/videos/play/wwdc2026/241/>
    (retrieved 2026-07-10). The framework accepts any model conforming to
    its Language Model protocol — Apple's own on-device model, vendor
    packages (Anthropic and Google publish Swift packages), or local open
    models. The VM limitation is specific: Apple Intelligence does not
    activate inside a virtualized macOS, and the Foundation Models
    framework reports itself unavailable there — so the ane lane simply
    does not exist in a guest, independent of GPU questions.

[^5]: Apple, "Integrate on-device AI models into your app using Core AI,"
    WWDC26 session 326.
    <https://developer.apple.com/videos/play/wwdc2026/326/> (retrieved
    2026-07-10). Curated open models AOT-compiled for Apple silicon,
    Mistral included. Distinct from the operator's own workhorse: a
    Dolphin-Mistral-24B (Venice Edition) at Q6_K_L quantization, ~19.7 GB
    GGUF, served by ollama — slow, sturdy, does the job most of the time.
    Apple's curated Mistral and a personally-chosen quantized fine-tune are
    different placements with different accuracy terms in the equation.

[^6]: Apple, Virtualization framework.
    <https://developer.apple.com/documentation/virtualization> (retrieved
    2026-07-10). Paravirtualized, graphics-oriented GPU device for macOS
    guests. The macOS license permits two virtualized macOS instances per
    host — irrelevant at n=1, recorded for completeness.

[^7]: The api lane means "someone's served model at the other end of an
    RPC," with two wire dialects in practice: the Anthropic Messages shape
    and the OpenAI chat shape. DeepSeek serves the former directly
    (`https://api.deepseek.com/anthropic`), which is the existence proof
    that these are seams, not vendors.

[^8]: magpie's audio path, measured 2026-06 on the machine whose signature
    appears in Appendix A, alongside a re-runnable fragment.

[^9]: On this platform the Neural Engine is reached only through Apple's
    frameworks, and for speech and transcription that is deliberate: the
    OS treats transcription as nearly free — close to a no-op
    computationally — which is precisely the integration this design leans
    on. An audit of the dev tree (2026-07-10) found no direct ANE
    programming anywhere (no CoreML artifacts); ane-lane work here rides
    AFM and Speech, as designed.

[^10]: The on-device foundation model's parameter count is widely reported
    as ~3B; Apple does not publish it. Treat the number as reported, not
    known — and as a moving target, per the directional bet in section 3.

[^11]: big-little-mesh, apple-silicon:
    <https://github.com/janearc/big-little-mesh/tree/main/apple-silicon>.
    The Swift work that owns host-side model serving for the mesh.

## Appendix A: the 70x, reproducibly

The measurement behind the lane table and the discernment equation's cost
term: whisper large-v3 transcribing the same voice memo, MLX versus torch,
on this machine, 2026-06, via magpie's audio path.

Hardware signature. Human-readable facts first; the fingerprint is
reproducible and verifiable by anyone at this keyboard:

```text
Model Identifier: Mac15,8 (MacBook Pro)
Chip:             Apple M3 Max — 16 CPU cores (12P/4E), 40 GPU cores
Memory:           48 GB unified
macOS:            27.0 (build 26A5368g)
Firmware:         20457.0.13.0.11
```

```console
$ { system_profiler SPHardwareDataType | grep -v -i 'serial\|uuid\|provisioning\|activation'; sw_vers; } \
    | shasum -a 256 | cut -c1-16
663bbc4d197add27
```

The fragment. Both sides transcribe the identical file; model load is
excluded from both timings, because residency is not work (section 6):

```python
# bench_whisper.py — the 70x, head to head
# uv run --with mlx-whisper --with openai-whisper python bench_whisper.py memo.m4a
import sys, time

audio = sys.argv[1]

import mlx_whisper  # Metal
mlx_whisper.transcribe(audio,  # first call eats the model load; time the second
                       path_or_hf_repo="mlx-community/whisper-large-v3-mlx")
t0 = time.monotonic()
mlx_whisper.transcribe(audio, path_or_hf_repo="mlx-community/whisper-large-v3-mlx")
t_mlx = time.monotonic() - t0

import whisper  # openai-whisper, torch backend — CPU on this hardware
model = whisper.load_model("large-v3")  # load excluded from timing
t0 = time.monotonic()
model.transcribe(audio)
t_torch = time.monotonic() - t0

print(f"mlx    {t_mlx:8.1f}s")
print(f"torch  {t_torch:8.1f}s   ({t_torch / t_mlx:.0f}x)")
```

Notes for a re-run. openai-whisper's torch backend runs on CPU here — no
CUDA exists, and torch's MPS path is not what was measured. The torch side
pulls roughly 3 GB of weights on first run. The original measurement was on
walking-around memos minutes in length; the exact multiple is clip- and
hardware-dependent, and the order of magnitude is the finding.

The next benchmark this appendix wants, once `haho bench` exists (section
15): one fixed 60-second file, transcribed three ways on the same hardware —
torch on cpu (the hours-of-the-day baseline), whisper on metal (the 70x),
and Apple Speech on the ane, where transcription approaches a computational
no-op[^9] — with the ane number carrying its framing caveat (section 9),
since `SpeechTranscriber`'s speed is bought at a coherence cost on proper
nouns and code-switching.
