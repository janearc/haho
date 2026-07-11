# haho 🪂 — RFC

- Status: DRAFT. Ratification by the operator and fable marks this file
  read-only, and the read-only file is the reference for the integrity of the
  project from then on, as is the usual convention.
- Date: 2026-07-11. Supersedes `docs/DESIGN.md` (2026-07-10, folded in here).
- Authors: Max (operator); Claude (Fable 5, drafting). Reviewed on the PR.
- Companions: holden `docs/ADR-0003-holden-is-a-service.md` (first customer);
  wonderlib (the gullet's origin); cardinal (the AFM seam, proven); mackerel
  `DESIGN.md` (the summit workload, RESEARCH); the speedy RFC (deposit
  semantics, unbuilt).

Contents:

1. What this is for
2. The principle: borg-shaped, fruit-flavored
3. How we got here (paling and magpie led, accidentally)
4. The silicon map
5. Placement: virtualization, containerization, and what haho is not
6. Residency: models are resident, work is ephemeral
7. The instance lifecycle (one jump per job)
8. The programming interface (JobSpec, JobEvent, JobCompletion)
9. The gullet
10. Dispatch and lanes (borg in miniature)
11. Sidecars and haho: the contours
12. The customer ladder
13. Security posture
14. Observability
15. Graduation and the end-to-end proof
16. Non-goals
17. Open questions
18. References
Appendix A: the 70x, reproducibly

## 1. What this is for

Nearly everything we build here reduces to the same sentence: take messy
human material — speech, corpora, diffs — and produce *meaning* from it, under
constraints of time, silicon, and cost. magpie turns walking-around audio into
text. lloyd smooths bad text into good text. neal interprets the operator's own
recordings back to him. wonderlib turns corpora into signatures and taxonomy.
holden turns a diff plus its design docs into a ruling. mackerel — the summit —
turns a room full of a language you do not speak into your language, in your
ears, a few seconds behind reality.

Each of these decomposes into the same small set of stages: capture, cut,
transcribe, smooth/interpret, synthesize, speak, cache. And on this hardware,
each stage has a *correct* processor: transcription belongs on the GPU or the
ANE, meaning-work belongs on the ANE's on-device model, synthesis-of-syntheses
can afford an API model, glue belongs on the CPU. We have been building
these stages one birb at a time, each birb hand-wiring its own model access,
its own chunking, its own cache, its own retry behavior. That worked for
proving the stages. It does not scale to composing them, and composition is
where the ambition lives.

haho is the harness: the one way a unit of model-work gets executed on this
mesh. A spawner describes the job — what to do, which model seam, how to chunk
when the model's gullet is small, how to synthesize the pieces, what to cache
and where, how long the answer is worth waiting for — and haho runs it: an
ephemeral, sandboxed, native macOS process that is born with the job, works it
against the right silicon, reports the whole way down, lands a completion
record, and exits.

The near-term customer is holden, which needs assessments run outside itself
(writer is never judge). Supporting holden is deliberately the easy case — one
job, one API-backed model, no deadline. The real target is the mackerel line:
live interpreting on hardware we own, which is the problem the wearables
industry is currently attacking with data centers. We are attacking it with a
laptop, because — section 2 — a laptop is the only place this design works.

## 2. The principle: borg-shaped, fruit-flavored

The design rule for all of this software, stated once and referenced
everywhere: keep
the *grammar* of large-scale systems and run it on the *physics* of one Apple
machine.

Borg-shaped: work is described by contracts, not by shell conventions. Jobs
are typed specs. Execution emits events. Completions are records. Placement is
a scheduling decision against declared resources. Health is a surface, not a
vibe. Everything is observable while it runs and auditable after it lands.
This is the robot grammar that makes backend systems legible, and we keep all
of it.

Fruit-flavored: the resources being scheduled are one SoC's organs — a GPU
reached through Metal, a Neural Engine reached only through Apple's
frameworks, an on-device foundation model the OS itself keeps warm, unified
memory shared by all of them. Stages of a pipeline co-reside with zero
serialization boundaries except the ones we choose. Transcription and
interpretation run *simultaneously without contending*, because they ride
different organs of the same fruit.

Two consequences, stated as requirements:

- This system MUST NOT be mistaken for a distributed system. It does not work
  on a cluster, and not because we have not gotten to it: the enabling
  condition is that ears, brain, and mouth share one machine's silicon and
  memory. At n>1 the design does not degrade; it ceases to exist. This is an
  n=1 mesh, Apple end to end, and the design's job is to extract the maximum
  from exactly that.
- Reach is distributed; compute is not. intruder puts the operator's phone on
  this mesh through an ephemeral cloudflared tunnel for the life of a
  connection. That is distribution of *access*. The work stays home.

The failure mode this principle guards against is real and we have stepped
near it repeatedly: "ephemeral unit of work" pattern-matches to "write a
Dockerfile," and a Dockerfile on this platform means a Linux micro-VM that
cannot reach Metal, cannot reach the ANE, and cannot call a single Apple
framework — a worker with every door to the silicon closed (section 5).
Robot-shaped alone is easy and unremarkable. The machine worth building is
robot-shaped *and* silicon-native.

## 3. How we got here (paling and magpie led, accidentally)

None of the following was designed toward haho. haho is what they add up to.

- **magpie** proved the silicon law. Its transcription stage (whisper
  large-v3 on MLX) ran roughly **70x faster than the same workload across
  torch** on the same machine.[^8] Appendix A gives the hardware signature
  and a re-runnable fragment. That number is the whole argument for stack
  correctness: a model on the wrong runtime is not slow, it is infeasible —
  the realtime ambitions of section 1 do not exist on torch. The law: model
  execution happens on the Apple-optimized stack, or it does not happen.
- **paling** proved model sovereignty and the sidecar pattern: a host-side
  inference daemon owning its own models, with a fleet-side sidecar carrying
  its mesh citizenship (health, discovery, bus adjacency). It established
  that host-native model work and fleet-native citizenship are different
  concerns carried by different processes (section 11).
- **cardinal** proved the AFM seam: Apple's on-device ~3B foundation model
  (the one behind Apple Intelligence) slotted behind a swappable backend
  interface, honestly gated — `available()` asks the framework, and a job
  that cannot produce reports `produced=false` with the real reason rather
  than fabricating output.
- **turtledove** recorded the transcription trade study: `mlx-whisper`
  (Metal, stronger accuracy) versus `apple-speech` (`SpeechTranscriber`, ANE,
  faster-than-realtime, weaker on proper nouns). Both doors stay open; which
  one a job uses is a placement decision, not a preference (section 4).
- **wonderlib** built the gullet: chunking strategies, re-read on fail,
  backoff informed by model distress, caching because most corpus work is
  repeatable. It built these as a standalone tool; section 9 promotes them
  to harness capability.
- **The judge** (now holden) demonstrated the need by lacking all of it: it
  shells out to a CLI with one whole prompt and hopes. No chunk control, no
  cache, no backend choice, no events while a long assessment runs. What we
  want from haho is, concretely, everything the judge cannot do.

## 4. The silicon map

The fruit has independent execution lanes. This table is the ground truth the
rest of the RFC schedules against:

| Lane | Reached through | What runs there | Properties |
|---|---|---|---|
| METAL (GPU) | MLX, via a resident server | whisper large-v3, mlx-lm models | highest throughput; **contended** — effectively one heavy tenant at a time; measured 70x over torch |
| ANE | AFM (Foundation Models)[^4], Speech (`SpeechTranscriber`), Translation, CoreML[^5] | the on-device ~3B model; streaming transcription; on-device MT | **concurrent with METAL**; low power; small models, small gullets; only Apple's frameworks can enter |
| API | Anthropic-compatible and OpenAI-compatible HTTP | Claude, DeepSeek[^7], anything speaking either wire | wide fan-out; costs money and round trips; needs the hostile network to cooperate |
| CPU | plain code | chunking, filters, synthesis glue, VAD | boring, reliable |

Three facts shape everything downstream:

1. **Every non-API lane opens only from a native macOS process.** MLX, AFM,
   Speech, Translation, and CoreML are macOS frameworks. No Linux guest can
   call them — they do not exist in that world — and no macOS guest can
   reach the ANE at all (section 5).
2. **METAL and ANE run concurrently.** This is what makes realtime plausible:
   whisper holds the GPU while AFM interprets on the ANE, and neither queues
   behind the other. A design that puts both stages on one lane reintroduces
   the contention that kills the deadline.
3. **The ANE is entered only through Apple's front doors.** There is no
   public direct API. AFM routes to it at the framework's discretion; Speech
   and CoreML likewise. We schedule *lanes* and accept that Apple schedules
   the silicon under them. (Audited 2026-07-10: nothing here has ever
   programmed the ANE directly — no CoreML artifacts exist anywhere.[^9]
   The one plausible ANE traversal to date is cardinal's AFM calls. A
   remembered "ANE in the shell" was Metal-via-MLX; the conflation is common
   enough that the contract now names lanes explicitly.)

## 5. Placement: virtualization, containerization, and what haho is not

Three shapes were evaluated for the instance. The findings are load-bearing,
so they live here rather than in a chat log (researched 2026-07-10;
references in section 18).

| Instance shape | Metal | ANE / AFM / Speech | Spawn cost | Fan-out | Verdict |
|---|---|---|---|---|---|
| Linux container (`apple/container`, Docker) — every container is a Linux micro-VM | no | no, and never — macOS frameworks do not exist in a Linux guest | sub-second | fine | **rejected** for work; fleet residents only |
| macOS guest (Virtualization.framework) | paravirtualized, graphics-tuned; compute throughput unproven for these loads | **no ANE**; Apple Intelligence / AFM unavailable in VMs[^4] | seconds; APFS clones help | **EULA: two macOS VMs per host**[^6] | isolation tier only |
| **Native macOS process + sandbox** | full, native | full — every door | milliseconds | GPU/ANE-bound only | **the instance shape** |

On the container row: Apple's `container` 1.0 (June 2026) gives each
container its own micro-VM.[^1] There is no GPU passthrough — the maintainers
state it plainly, and Apple's GPUs are not behind an IOMMU, so classic
passthrough may never come.[^2] Community projects proxy MLX inference from
guests to a host daemon over vsock at ~95% of native throughput,[^3] which is
good engineering and also a concession of the point: the silicon work happens
host-side anyway.

On the macOS-guest row: Virtualization.framework informs this design even
though we are not building on it, three ways. First, it shows the platform's
grain — the paravirtualized GPU device for macOS guests *brokers* Metal to
shared hardware rather than surrendering the device;[^6] brokered access,
never passthrough, is how Apple shares silicon, and our resident-server
pattern (section 6) follows the same grain. Second, it is the named **isolation
tier**: a job we genuinely do not trust can run in a macOS guest (drivable
from Go via published bindings), paying the tier's price — no ANE, no AFM,
at most two concurrent — which is exactly why isolation is the exception and
the sandbox is the rule. Third, the two-guest license cap makes "a fleet of
macOS VMs" a contradiction on this platform; reaching for one imports
cluster physics that do not exist here.

So, precisely, **haho is**: a native-process job harness — a spawner-facing
contract, a sandboxed one-job process, a gullet, a cache client, lane-aware
dispatch, and an audit trail.

And **haho is not**:

- not a container runtime or a VM manager — it spawns processes; the
  isolation tier delegates to Virtualization.framework when invoked;
- not a model server — models are resident and owned elsewhere (section 6);
- not a service framework — birbs remain services; haho executes *jobs*;
- not a sidecar (section 11);
- not a scheduler for more than one machine, ever (sections 2, 16).

Job isolation is a sandbox profile plus a per-job scratch directory plus an
explicitly constructed environment — no ambient inheritance. Illustrative
profile; the real one is generated per job from the JobSpec:

```scheme
;; haho instance sandbox (illustrative; generated per-job from the spec)
(version 1)
(deny default)
(allow process-fork)
(allow file-read*  (subpath (param "HAHO_ROOT"))      ; the versioned bundle
                   (subpath (param "SCRATCH")))
(allow file-write* (subpath (param "SCRATCH")))
(allow network-outbound
  (remote tcp "127.0.0.1:*")       ; resident model servers, surrealdb, redis
  (remote tcp (param "API_HOST"))) ; present only when the spec names an API lane
;; plus the mach-lookups the declared lanes require (AFM, Speech), and no others
```

A job whose spec names no API backend gets no egress beyond loopback. Every
job's scratch dies with it.

## 6. Residency: models are resident, work is ephemeral

An early draft of this design conflated two lifetimes. The correction is the
central architectural sentence of this RFC:

**Models are resident. Work is ephemeral.**

whisper large-v3 takes seconds to load and pins gigabytes of unified memory;
you do not load it per utterance-chunk, and you do not let it die between
segments 17 and 18 of a live stream. AFM is resident by construction — macOS
itself keeps the foundation model warm, which is one of the quiet gifts of
this platform. Speech and Translation likewise. The resident layer is small,
boring, and mostly not ours:

- **the MLX server** — a host process holding whisper (and any mlx-lm
  models) warm on the METAL lane, serving loopback HTTP. The one resident
  piece we own. Its ownership is an open question (section 17); its contract
  is not: it is a model server, it holds no job state, and haho instances
  are its only local clients.
- **AFM, Speech, Translation** — resident inside macOS; an instance calls
  the framework and the OS does the rest.
- **surrealdb / redis** — resident state, in the fleet, reached over exposed
  loopback ports; the only things one instance shares with another.

A haho instance is then genuinely cheap: process spawn in milliseconds, no
weights to load, born with its job on stdin, dead at the completion record.
Freshness is enforced by lifetime — an instance that no longer exists cannot
carry one job's context into the next — and the expensive things it touched
were never its to carry.

## 7. The instance lifecycle (one jump per job)

The name is the design: a HAHO jump opens high and rides a long descent to a
precise landing. An instance lives exactly one jump.

1. **Birth.** The spawner starts the process with a `JobSpec` on stdin. The
   first stdout line is the birth record: the job id and the status address
   the instance bound (loopback, ephemeral port). Configuration is fixed at
   birth; there is no runtime reconfiguration of a one-job process.
2. **Descent.** The instance is queryable for its whole working life — the
   status endpoint reports state, how long it has been in flight, progress,
   the lane and model in use — and it emits typed progress events on stdout.
   Work can take a long time; a living instance can always say so.
3. **Landing.** One terminal `JobCompletion` on stdout — outcome, result,
   model used, chunk and cache statistics, wall time — then exit. The exit
   code is the coarse cross-check: a completion/exit disagreement is FAILED
   in every spawner's eyes, loudly. Then there is nothing left to be dirty.

Instances are plural: one instance per workload, arbitrarily many in flight,
bounded by lane budgets (section 10) rather than by the contract. Nothing
distinguishes the first instance from the tenth.

## 8. The programming interface

Contracts are protobuf (buf v2); consumers vendor-generate; gen-freshness
holds per the mesh's standing invariant. The PoC's resident-service protos
(`HarnessService` with `Submit`/`StreamSubmit`) are superseded by the job
contract below; `haho.common.v1.ChatMessage` and `haho.filter.v1` survive
unchanged. On the wire and at rest, protojson.

The spec is the whole interface. A spawner never configures haho; it
describes one job.

```proto
syntax = "proto3";

package haho.job.v1;

option go_package = "github.com/janearc/haho/gen/jobpb;jobpb";

import "google/protobuf/timestamp.proto";
import "haho/common/v1/common.proto";
import "haho/filter/v1/filter.proto";

// Everything one unit of work needs, and nothing else. Arrives on stdin at
// birth; immutable for the life of the instance.
message JobSpec {
  string job_id = 1;          // spawner-assigned; rides every event and the lease
  string correlation_id = 2;  // cross-pipeline correlation (mackerel's corr)

  Payload payload = 3;
  Backend backend = 4;        // which model seam does the work
  Gullet gullet = 5;          // how the work is fed to it (section 9)
  Synthesis synthesis = 6;    // what happens to per-chunk results
  Cache cache = 7;
  haho.filter.v1.FilterPipelineConfig input_filters = 8;
  haho.filter.v1.FilterPipelineConfig output_filters = 9;
  Deadline deadline = 10;     // speedy semantics live here
  map<string, string> secret_env = 11; // logical name -> ENV VAR NAME.
                                       // References, never values (section 13).
  map<string, string> labels = 12;     // spawner-owned; mirrored to the hm lease
}

message Payload {
  oneof body {
    Messages messages = 1;  // chat-shaped work
    string   text     = 2;  // a transcript, a diff, a corpus file
    bytes    raw      = 3;  // audio and other binary stage input
  }
}
message Messages { repeated haho.common.v1.ChatMessage messages = 1; }

// A backend names a seam, never a vendor.
message Backend {
  Kind kind = 1;
  enum Kind {
    KIND_UNSPECIFIED     = 0;
    ANTHROPIC_COMPATIBLE = 1; // base_url + model + token env: Claude, DeepSeek, ...
    OPENAI_COMPATIBLE    = 2; // the /api/chat shape: Ollama et al.
    AFM                  = 3; // Apple Foundation Models, on-device
    MLX                  = 4; // the resident MLX server (whisper, mlx-lm)
    SPEECH               = 5; // SpeechTranscriber streaming transcription
    TRANSLATION          = 6; // Apple's on-device machine translation
  }
  Lane lane = 2;            // declared placement; validated against kind
  string base_url = 3;      // API kinds and the MLX server
  string model = 4;         // where the kind takes one
  string token_env = 5;     // ENV VAR NAME of the credential; empty = none
  map<string, string> params = 6;
}

// Lanes are the silicon truth (section 4). The dispatcher budgets per lane.
enum Lane {
  LANE_UNSPECIFIED = 0;
  LANE_METAL = 1;  // contended; heavy tenants
  LANE_ANE   = 2;  // concurrent with METAL; small gullets
  LANE_API   = 3;  // wide; network-dependent
  LANE_CPU   = 4;  // glue
}

// The gullet: how work is fed to a model that may be smaller than the work.
message Gullet {
  Strategy strategy = 1;
  enum Strategy {
    STRATEGY_UNSPECIFIED = 0;
    WHOLE_OR_ERROR = 1;  // one attempt; a barf is the caller's problem
    CHUNK_ON_BARF  = 2;  // try whole; on barf, cut and continue
    CHUNK_ALWAYS   = 3;  // never try whole; this is the map of mapesis
  }
  Chunker chunker = 2;
  enum Chunker {
    CHUNKER_UNSPECIFIED = 0;
    TOKENS            = 1;
    SENTENCES         = 2;
    MARKDOWN_SECTIONS = 3;
    LINES             = 4;
  }
  uint32 chunk_size = 3;             // unit per chunker
  uint32 chunk_overlap = 4;
  uint32 max_attempts_per_chunk = 5; // re-read on fail
  Backoff backoff = 6;
  message Backoff {
    uint32 initial_ms = 1;
    double multiplier = 2;
    uint32 max_ms = 3;
  }
}

// The trailing synthesis step over per-chunk results.
message Synthesis {
  Mode mode = 1;
  enum Mode {
    MODE_UNSPECIFIED = 0;
    NONE    = 1;  // per-chunk results ARE the result
    CONCAT  = 2;  // ordered join, no model
    MAPESIS = 3;  // a model pass over the map results
  }
  Backend backend = 2;    // MAPESIS only; often a wider-gulleted lane than the map
  string instruction = 3; // what the synthesis pass is for
}

message Cache {
  Kind kind = 1;
  enum Kind {
    KIND_UNSPECIFIED = 0;
    NONE      = 1;
    SURREALDB = 2;  // also the RAG store
    REDIS     = 3;  // plain hot KV when that is the right shape
  }
  uint32 ttl_seconds = 2; // 0 = no expiry
  // Keys are content-addressed: hash(kind, model, params, filtered chunk).
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

The descent and the landing:

```proto
message JobEvent {
  string job_id = 1;
  google.protobuf.Timestamp at = 2;
  oneof event {
    Born      born      = 3; // first line: the status address
    Heartbeat heartbeat = 4;
    Progress  progress  = 5;
    Barfed    barfed    = 6;
    CacheHit  cache_hit = 7;
    Degraded  degraded  = 8;
  }
  message Born      { string status_addr = 1; }
  message Heartbeat { uint64 in_flight_ms = 1; }
  message Progress  { uint32 chunks_done = 1; uint32 chunks_total = 2; string stage = 3; }
  message Barfed    { string reason = 1; bool will_chunk = 2; }
  message CacheHit  { uint32 chunk_index = 1; }
  message Degraded  { string what = 1; string why = 2; }
}

message JobCompletion {
  string job_id = 1;
  Outcome outcome = 2;
  enum Outcome {
    OUTCOME_UNSPECIFIED = 0;
    PRODUCED     = 1;
    DROPPED_LATE = 2; // the deadline fired with on_late=DROP
    FAILED       = 3; // with a specific reason; never fabricated output
  }
  string reason = 3;
  Payload result = 4;
  string model_used = 5;
  Lane lane_used = 6;
  uint32 chunks = 7;
  uint32 cache_hits = 8;
  int32 tokens_in = 9;
  int32 tokens_out = 10;
  uint64 wall_ms = 11;
}
```

Invocation is a pipe, because instances are ephemeral and pipes are how
ephemeral processes talk. The status endpoint exists alongside stdout because
a descent can be long and a pipe cannot be queried:

```console
$ haho run < job.json
{"jobId":"interp-8841-s17","event":{"born":{"statusAddr":"127.0.0.1:52341"}}}
{"jobId":"interp-8841-s17","event":{"progress":{"chunksDone":1,"chunksTotal":4,"stage":"map"}}}
{"jobId":"interp-8841-s17","event":{"cacheHit":{"chunkIndex":2}}}
{"jobId":"interp-8841-s17","event":{"progress":{"chunksDone":4,"chunksTotal":4,"stage":"synthesis"}}}
{"completion":{"jobId":"interp-8841-s17","outcome":"PRODUCED","modelUsed":"afm","laneUsed":"LANE_ANE","chunks":4,"cacheHits":1,"wallMs":2140,"result":{"text":"..."}}}
$ echo $?
0
```

Exit codes: `0` PRODUCED, `3` DROPPED_LATE, `1` FAILED — and any mismatch
between the exit code and the completion record is FAILED in the spawner's
eyes regardless of what either says alone.

Mid-flight, anyone — the spawner, the operator, hm — can ask the instance
itself:

```console
$ curl -s 127.0.0.1:52341/status
{"jobId":"interp-8841-s17","state":"busy","inFlightMs":1290,
 "stage":"map","chunksDone":2,"chunksTotal":4,"lane":"LANE_ANE","model":"afm"}
```

A spawner, in Go. This is holden's whole integration, which is why supporting
holden is the easy case:

```go
cmd := exec.Command("haho", "run")
cmd.Stdin = bytes.NewReader(specJSON) // protojson JobSpec
out, _ := cmd.StdoutPipe()
if err := cmd.Start(); err != nil { ... }

sc := bufio.NewScanner(out)
for sc.Scan() {
    var l hahoLine // {Event *JobEvent; Completion *JobCompletion}
    if err := json.Unmarshal(sc.Bytes(), &l); err != nil { ... }
    switch {
    case l.Event != nil && l.Event.GetBorn() != nil:
        watchdog.Arm(l.Event.GetBorn().StatusAddr) // kill-if-quiet is spawner-side
    case l.Event != nil:
        relay(l.Event) // onto WatchRulings, the bus, wherever this spawner reports
    case l.Completion != nil:
        done = l.Completion
    }
}
err := cmd.Wait()
if disagree(done, err) { // PRODUCED but exit != 0, exit 0 but no completion, ...
    done = failed("completion/exit disagreement")
}
```

Three specs, spanning the ladder. First, holden's assessment — the trivial
customer:

```json
{
  "jobId": "ruling-delightd-121",
  "payload": {"messages": {"messages": [
    {"role": "user", "content": "<the assembled ruling prompt>"}]}},
  "backend": {"kind": "ANTHROPIC_COMPATIBLE", "lane": "LANE_API",
              "baseUrl": "https://api.anthropic.com", "model": "claude-fable-5",
              "tokenEnv": "HOLDEN_ANTHROPIC_KEY"},
  "gullet": {"strategy": "WHOLE_OR_ERROR"},
  "synthesis": {"mode": "NONE"},
  "cache": {"kind": "NONE"}
}
```

Second, the sentence this interface exists to make sayable — a wonderlib
corpus sweep that declines the big local model on purpose: *we are not
bringing up mistral for this; we are going to furiously chunk it through the
fm, and that is the map part of mapesis.* As a spec:

```json
{
  "jobId": "sig-corpus-c041",
  "payload": {"text": "<one corpus document>"},
  "backend": {"kind": "AFM", "lane": "LANE_ANE"},
  "gullet": {"strategy": "CHUNK_ALWAYS", "chunker": "MARKDOWN_SECTIONS",
             "chunkSize": 1, "maxAttemptsPerChunk": 3,
             "backoff": {"initialMs": 100, "multiplier": 2.0, "maxMs": 2000}},
  "synthesis": {"mode": "MAPESIS",
                "backend": {"kind": "ANTHROPIC_COMPATIBLE", "lane": "LANE_API",
                            "baseUrl": "https://api.anthropic.com",
                            "model": "claude-fable-5",
                            "tokenEnv": "WONDER_ANTHROPIC_KEY"},
                "instruction": "merge the per-section signatures; emit taxonometry rows and a synthesis section"},
  "cache": {"kind": "SURREALDB", "ttlSeconds": 0}
}
```

Third, mackerel's interpreter stage — the summit customer, deadline-bound:

```json
{
  "jobId": "interp-8841-s17",
  "correlationId": "sess-8841/seg-17",
  "payload": {"text": "<50-100 words of whisper output for this segment>"},
  "backend": {"kind": "AFM", "lane": "LANE_ANE"},
  "gullet": {"strategy": "CHUNK_ON_BARF", "chunker": "SENTENCES", "chunkSize": 3,
             "maxAttemptsPerChunk": 2,
             "backoff": {"initialMs": 50, "multiplier": 2.0, "maxMs": 400}},
  "synthesis": {"mode": "MAPESIS",
                "backend": {"kind": "AFM", "lane": "LANE_ANE"},
                "instruction": "merge the smoothed sentences; carry meaning, drop disfluency; spoken register"},
  "cache": {"kind": "SURREALDB", "ttlSeconds": 900},
  "deadline": {"deadlineMs": 3000, "onLate": "DROP"}
}
```

The transcription job that feeds it is one more spec, not a special case:
`backend {kind: MLX, lane: LANE_METAL, model: "whisper-large-v3", params
{task: "translate"}}`, payload `raw` audio, gullet `WHOLE_OR_ERROR` — the
client already cut the chunk to size; that was the point of client-A's
disposable transcript — and `deadline {onLate: DROP}`.

Pluggability is exactly the enum surfaces: backends, chunkers, synthesis
modes, cache kinds, filters. Each is a Go interface with a registry behind
it. Adding a backend is one interface implementation and one enum value, and
no spawner changes.

## 9. The gullet

wonderlib discovered, empirically, that most of the difficulty of using
small models is feeding them, and most of the cost of using any model is
feeding it the same thing twice. haho promotes both findings from a tool to
the harness, so no birb hand-writes them again.

The ladder, as the instance executes it:

```go
func (g *gullet) run(ctx context.Context, job *Job) (*Result, error) {
    if g.strategy != ChunkAlways {
        res, err := g.attempt(ctx, job.whole())
        if err == nil {
            return res, nil
        }
        if g.strategy == WholeOrError || !isBarf(err) {
            return nil, err // real failures propagate; barfs escalate
        }
        emit(Barfed{Reason: err.Error(), WillChunk: true})
    }

    chunks := g.chunker.cut(job) // TOKENS | SENTENCES | MARKDOWN_SECTIONS | LINES
    outs := make([]*Out, len(chunks))
    for i, c := range chunks {
        if ctx.Err() != nil { // the deadline owns this loop
            return nil, ctx.Err()
        }
        if hit, ok := g.cache.get(c.key()); ok {
            outs[i] = hit
            emit(CacheHit{ChunkIndex: uint32(i)})
            continue
        }
        out, err := g.attemptWithRetry(ctx, c) // re-read on fail; backoff on distress
        if err != nil {
            return nil, fmt.Errorf("chunk %d/%d: %w", i+1, len(chunks), err)
        }
        outs[i] = out
        g.cache.put(c.key(), out)
        emit(Progress{ChunksDone: uint32(i + 1), ChunksTotal: uint32(len(chunks)), Stage: "map"})
    }
    return g.synthesize(ctx, outs) // NONE | CONCAT | MAPESIS
}
```

Three mechanisms carry the weight:

- **Barf detection.** A barf is the model failing *because of the input's
  size or shape*, distinct from a real failure: context-window errors,
  truncated or empty output, refusals that name length, output that fails
  the re-read. Barfs escalate down the ladder; real failures — auth,
  network, a model service being down — propagate immediately and honestly.
- **Re-read on fail.** Before an output is accepted, a cheap check asks: does
  it parse, and does it address the chunk it was given. A failed re-read
  counts against `max_attempts_per_chunk` and triggers backoff. This is the
  tuning that knows when the model is not doing well with the input —
  measured, not guessed. Repeated re-read failures on well-formed chunks are
  the signature of a distressed model, and the multiplier is how the
  instance eases off.
- **The cache key** is `hash(backend kind, model, params, filtered chunk)`:
  content-addressed, so identical work costs one model call, ever, across
  all instances, until TTL. In a live room this is not an optimization —
  announcements repeat, names repeat, and every repeat is a deadline met for
  free. surrealdb is the default and doubles as the RAG store; redis is
  available when plain hot KV is the right shape.

MAPESIS synthesis closes the loop wonderlib opened: the per-chunk outputs
are the map; the synthesis pass — often on a wider-gulleted lane than the
map, as in the corpus example — produces the merged artifact: a smoothed
utterance, a `synthesis.md`, taxonometry rows. Map on the small free lane,
synthesize on the big one exactly once. That cost shape is what makes
corpus-scale work affordable and realtime work possible.

## 10. Dispatch and lanes (borg in miniature)

The dispatcher is spawner-side library code — holden, the wonderlib CLI, and
mackerel all link it — and it is deliberately small. It admits jobs against
per-lane budgets and queues or refuses what does not fit. Instances never
negotiate for silicon.

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

METAL's budget of one is not a placeholder. One whisper stream saturates the
GPU; admitting a second heavy tenant does not make two jobs fast, it makes
both late — the 70x law has a corollary: the right stack, oversubscribed,
degrades back out of feasibility. The ANE budget is small because AFM's own
concurrency is. The API budget is wide because someone else owns those GPUs.
A deadline-bearing job that cannot be admitted before its deadline is
DROPPED at admission: failing before spending is the freshness rule applied
to the queue itself.

This is the entire scheduler: a few dozen lines against a four-row resource
table. It is still the borg grammar — declared resources, admission control,
observable placement — at the scale the physics actually are. Anything
fancier is cluster cosplay.

## 11. Sidecars and haho: the contours

The mesh uses sidecars wherever it has resident host-native services:
paling's inference daemon runs host-side (model sovereignty), and
`paling-sidecar` runs in the fleet carrying its citizenship — health
reporting, discovery/registration with delightd, bus adjacency. The pattern
generalizes: **a sidecar is how a resident thing remains a good citizen of
the fleet it cannot live inside.**

haho is the other half of that sentence: **how ephemeral work executes
against silicon the fleet cannot reach.** The contours, so nobody blurs
them:

| | sidecar | haho instance |
|---|---|---|
| lifetime | the service's (long) | the job's (short) |
| holds | citizenship state (registration, health) | nothing beyond the job |
| does model work | never | only |
| lives | fleet pod (Linux) | native macOS process |
| spawned by | deploy/launchd, with its service | a spawner, per job |
| answers to | delightd, hm, the bus | its spawner; hm's lease mirror |

Two rules fall out. A haho instance MUST NOT get a sidecar: its citizenship
is carried by its spawner and by hm's lease mirror (section 14); a per-job
fleet pod would be pure overhead. And a sidecar MUST NOT do
model work: the moment it wants a model, that want is a JobSpec, deposited
like everyone else's. Resident services stay resident, work stays ephemeral,
and the two shake hands only through contracts.

## 12. The customer ladder

Each rung uses strictly more of the interface, and each is a real, named
workload:

1. **holden** — one job, API lane, `WHOLE_OR_ERROR`, no deadline, no cache.
   The judge's entire outsourcing need, and deliberately the least
   interesting thing haho can do. If the harness is right, holden is a side
   effect.
2. **wonderlib CLI** — batch fan-out over a corpus: `CHUNK_ALWAYS`, AFM map,
   API-lane MAPESIS synthesis, surrealdb cache with no expiry. The CLI's
   `--signature --all` and `--mapesis` modes become spec templates; the
   library stops owning chunking and starts owning *strategies*.
3. **lloyd / neal** — the interpreter stage as a seam: `CHUNK_ON_BARF`,
   sentence chunking, ANE lane. lloyd's backend swaps from mistral-via-
   ollama to AFM via the fm seam cardinal proved, and the smoothing of the
   operator's own English memos becomes the same spec mackerel will run on
   a stranger's Spanish.
4. **mackerel / intruder / speedy** — the summit: three lanes at once,
   deadlines with DROP, the cache as deadline insurance, correlation ids
   threading client-A → mesh → client-B. The mesh side of the interpreting
   loop reduces, in haho terms, to two spec templates — transcribe on METAL,
   interpret on ANE — and a spawner that deposits them per segment. That our
   hardest ambition reduces to two JSON documents and a dispatch loop is the
   design working, not the ambition shrinking.

## 13. Security posture


- Secret **values** MUST NOT appear in a JobSpec, an event, a completion, a
  log line, or this repo. Specs carry env var *names*; the operator provisions
  values (launchd environment, keychain-backed injection at spawn); the
  instance reads them at birth. A named-but-empty reference is a loud birth
  failure, never a quiet degradation.
- The sandbox denies by default: read the versioned bundle and the scratch
  dir; write scratch only; loopback plus the declared API host only; no
  ambient environment — the spawner constructs the child environment from
  the spec's references and nothing else.
- The status endpoint is read-only and binds loopback. There is no mutating
  surface on a living instance, because configuration died at birth.
- An instance holds only what its one job needs. A job with no API backend
  holds no credential and reaches nothing beyond loopback. The blast radius
  is one job, and then the process is gone.

## 14. Observability

- **stdout events** — the descent as typed JSON lines: born, heartbeats,
  progress, barfs, cache hits, degradations, completion. The spawner relays
  where it likes: holden onto `WatchRulings`, pipeline spawners onto the
  bus.
- **the status endpoint** — pull-based truth mid-flight, `in_flight_ms`
  included, for anything that missed the stream.
- **the completion record** — the durable artifact. Spawners persist it
  (holden's ledger, wonderlib's manifests); it carries enough — model,
  lane, chunks, cache hits, tokens, wall time — that no log-diving ever
  reconstructs a flight.
- **the hm lease mirror** — the fleet's window. The spawner reports births
  and landings to hm, which holds a Kubernetes Lease per flight, labeled
  from the spec. Kube dashboards see host-side work as first-class objects
  with liveness semantics; a lease that lapses without a landing is the
  monitoring signal for a hung flight. Kube observes; it never schedules.

```yaml
apiVersion: coordination.k8s.io/v1
kind: Lease
metadata:
  name: haho-interp-8841-s17
  namespace: fleet
  labels:
    mesh/kind: haho-flight
    mesh/spawner: mackerel
    mesh/lane: ane
spec:
  holderIdentity: "mackerel@host"
  leaseDurationSeconds: 15
```

- **structured JSON logging** throughout, per fleet rule; a bare print is a
  lint error.

## 15. Graduation and the end-to-end proof

haho is a dev project. It graduates to prod when this document is ratified
by the operator and fable and marked read-only at `docs/haho-rfc.md`, as is
the usual convention, and the ratified document is thereafter the reference
for the integrity of the project, as is the usual convention.

What ratification buys is the proof obligation. haho MUST be end-to-end
provable from the day it is promoted, even as the barest shell it will then
be: it must **loft** (spawn from a spec and answer on its status endpoint),
**work** (drive a real backend through the gullet, with progress events and
cache statistics), and **land** (emit a completion record that agrees with
its exit code) — each step verifiable from outside, in addition to whatever
unit and coverage tests exist. Coverage with no flight is not a tested
harness.

The full proof, staged as the rungs come alive:

1. **The flight** (barest shell): loft, work, land — verifiable at each
   step.
2. **The cache**: run a real job, then the identical job; the second
   completion shows `cacheHits > 0` and near-zero model time. The product
   comes out twice, the second time for free.
3. **The lanes** (the fruit-flavored proof): a METAL transcription job and
   an ANE interpretation job in flight *simultaneously*, both meeting
   deadlines that either alone would meet — the concurrency the entire
   realtime ambition rests on, demonstrated rather than asserted.
4. **The ladder**: a document that barfs whole is chunked, mapped with at
   least one cache hit, synthesized via MAPESIS, and lands PRODUCED.

## 16. Non-goals

- **n>1.** Not now, not later. Portability of this design to a cluster is
  not deferred work; it is a contradiction of the design (section 2).
- **Linux containers as instances.** Fleet residents are containers; work
  is not (section 5).
- **A resident haho.** Anything resident here is a model server or a fleet
  service; the harness stays one-job-one-process.
- **Being a model server.** haho calls them; owning weights is someone
  else's residency (sections 6, 17).
- **Kube-scheduled work.** hm mirrors flights into kube for observation;
  nothing in kube ever places silicon work.
- **Replacing sidecars.** Residents keep their citizenship pattern
  untouched (section 11).

## 17. Open questions (named, not smuggled)

- **Who owns the resident MLX server.** paling is the precedent — model
  sovereignty, host daemon, sidecar citizenship — and may simply grow the
  role; a purpose-built tiny server is the alternative. Decided when the
  METAL lane is first wired, not before.
- **AFM gullet calibration.** Where the fm actually barfs — the real
  capacity behind "it fails on a 100-line markdown file" — needs
  measurement, and the default `chunk_size` per chunker ships from those
  measurements, not from guesses. wonderlib's existing corpus runs are the
  natural instrument.
- **Vector RAG.** BM25 (lexical, in surrealdb today) matches how we query:
  code, error strings, names. Vector search enters when a
  workload demonstrably retrieves by paraphrase rather than by token —
  corpus synthesis across restated sources is the likely first — and it
  costs an embedding model in the loop plus an index story, so it waits for
  that demonstration.
- **The isolation-tier trigger.** Which jobs, if any, are untrusted enough
  to pay the macOS-guest price: no ANE, no AFM, two concurrent. Today the
  answer is none; the tier exists so that the day the answer changes, the
  design does not.
- **speedy as a library surface.** Deadline/DROP semantics live in the
  JobSpec now; whether the big-little-mesh speedy package adds streaming
  deposit ergonomics on top — per-segment spawners as a reusable pattern —
  is speedy's RFC to settle, not this one.

## 18. References

Fetched 2026-07-10 over the VPN. The hostile network makes live links
unreliable, which is why every claim's substance is also restated inline
where it is used.

[^1]: Apple, "Discover container machines," WWDC26 session 389 —
    <https://developer.apple.com/videos/play/wwdc2026/389/>. Micro-VM-per-
    container architecture; the session does not mention GPU or Metal
    access for guests.

[^2]: apple/container, discussion 62, "GPU passthrough availability?" —
    <https://github.com/apple/container/discussions/62>. Maintainer, June
    2025: "we do not currently support this." Apple GPUs are not behind an
    IOMMU; the hypervisor has no passthrough support.

[^3]: RobotFlow-Labs, container-toolkit-mlx —
    <https://github.com/RobotFlow-Labs/container-toolkit-mlx>. Host-side
    Swift daemon with native Metal access serving MLX inference over vsock;
    ~95% of native throughput; early-stage. Docker Model Runner does the
    same brokering at the API level —
    <https://www.docker.com/blog/docker-model-runner-vllm-metal-macos/>.

[^4]: Apple, "What's new in the Foundation Models framework," WWDC26
    session 241 — <https://developer.apple.com/videos/play/wwdc2026/241/>.
    Third-party and open models behind the Language Model protocol. Apple
    Intelligence, and therefore AFM, is unavailable inside virtual
    machines.

[^5]: Apple, "Integrate on-device AI models into your app using Core AI,"
    WWDC26 session 326 —
    <https://developer.apple.com/videos/play/wwdc2026/326/>. Curated open
    models (Mistral included) AOT-compiled for Apple silicon; PyTorch
    conversion tooling.

[^6]: Apple, Virtualization framework —
    <https://developer.apple.com/documentation/virtualization>. macOS
    guests receive a paravirtualized, graphics-oriented GPU device; the
    macOS software license agreement permits up to two virtualized macOS
    instances per host machine.

[^7]: DeepSeek's Anthropic-compatible endpoint,
    `https://api.deepseek.com/anthropic` — the existence proof for
    `ANTHROPIC_COMPATIBLE` as a seam rather than a vendor.

[^8]: magpie's audio path, measured 2026-06 on the machine whose signature
    appears in Appendix A, alongside a re-runnable fragment.

[^9]: ANE-usage audit of `~/mesh/dev`, 2026-07-10: no CoreML artifacts
    anywhere (no `.mlmodel`/`.mlpackage`, no `coremltools`); the one
    plausible ANE traversal is cardinal's `apple-fm-sdk` calls
    (`cardinal/backends/fm.py`).

## Appendix A: the 70x, reproducibly

The measurement behind the silicon law (sections 3 and 4): whisper large-v3
transcribing the same voice memo, MLX versus torch, on this machine,
2026-06, via magpie's audio path.

Hardware signature — `663bbc4d197add27` (sha256, first 16, over the
sanitized `system_profiler SPHardwareDataType` + `sw_vers` output; serial
and machine UUID excluded before hashing):

```text
Model Identifier: Mac15,8 (MacBook Pro)
Chip:             Apple M3 Max — 16 CPU cores (12P/4E), 40 GPU cores
Memory:           48 GB unified
macOS:            27.0 (build 26A5368g)
Firmware:         20457.0.13.0.11
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

Notes for a re-run. openai-whisper's torch backend runs on CPU here — there
is no CUDA, and torch's MPS path was not what magpie measured. The torch
side pulls roughly 3 GB of weights on first run; mind the download rules
before letting it rip. The original measurement was on walking-around memos
minutes in length; the exact multiple is clip- and hardware-dependent, and
the order of magnitude is the law.
