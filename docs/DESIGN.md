# haho 🪂 — the harness design

- Status: DRAFT (awaiting operator + fable ratification; on ratification this
  document is marked read-only at `docs/haho-rfc.md` per convention)
- Date: 2026-07-10 (research pass against WWDC26-era sources, cited inline)
- Deciders: Max (operator), Claude (agent — drafted by Fable 5)
- Companions: holden `docs/ADR-0003-holden-is-a-service.md` (the first
  consumer's side of this seam); the README (operational reference for the
  current PoC — it describes what runs today, this document describes what
  haho is becoming, and the README gets trued when the contract recast lands).

## What haho is

haho is the mesh's LLM harness: the one way work gets done by a model here.
Anything on the mesh that needs a model — an assessment, an errand, a corpus
sweep — describes the job, and haho runs it: filters the input, checks the
cache, talks to the model, filters the output, reports the result. We built
our own because the harness needs what our substrate already provides —
surrealdb for caching and RAG, mapesis, the contracts — natively, not bolted
on, and because we will be spawning harnesses often enough that "make shit up
each time" is a cost we decline to keep paying.

haho runs one workload, one instance, and then it is gone.

## The name is the design

A HAHO jump opens high and rides a long descent to a precise landing. A haho
instance is the same shape: born for one job, alive and working — possibly
for a long time — and able to tell you so the whole way down, then it lands
and it is over. It is NOT a resident service. An instance never sees a second
job; whatever context it accumulated dies with it. Spawners get freshness by
construction, not by discipline: a harness that no longer exists cannot carry
one job's context into the next.

## The three moments

1. **Birth.** The spawner starts an instance with a proto-defined job spec on
   stdin: everything this one workload needs — messages, model selection,
   filter configuration, cache policy, secret *references* (never values; see
   Secrets) — and nothing else. Configuration is fixed at birth; there is no
   runtime reconfiguration of a one-job process (the PoC's runtime
   `POST /v1/filters` dies with this design).
2. **Descent.** Work can take a long time, so a living instance is queryable.
   It exposes a status endpoint for its lifetime — still here, still working,
   how long it has been in flight, progress so far, the model it is driving —
   and emits progress events. The spawner records the instance's address at
   birth. The endpoint's identity dies with the instance; nothing answers on
   it between jobs, by design.
3. **Landing.** One terminal completion record — job id, outcome, result
   payload, model used, token spend, cache statistics — then exit. The exit
   code is the coarse cross-check on the completion record; a spawner that
   sees them disagree treats the job as failed. The completion record is the
   instance's sign-off that the work was done: the last thing it says before
   it is gone.

## The instance, inside

Every instance is a container from haho's baked image, so every jump starts
from the same known root. In that root:

- **The filter pipeline.** Input filters (RTK for context reduction) and
  output filters (passthrough, logger), composable, configured at birth via
  the job spec. Owned by `haho.filter.v1`.
- **Model backend clients.** One interface, several backends — the next
  section, because this is where the design has to be forward-looking and
  where the research went.
- **mapesis.** Available in-instance for jobs that need it.
- **The surrealdb client.** The instance's line to the only state that
  outlives it.

The image is the "all the things" guarantee: holden (or any spawner) does not
assemble an environment, it names an image. A dirty harness is impossible
because there is no harness left to be dirty.

## Model backends

The job spec picks a backend; the instance drives it. Two shapes cover
everything we can currently name, and the enum in `haho.model.v1` will be
recast to name these seams rather than vendors' marketing:

**1. Anthropic-API-shaped endpoints (the wire we standardize on).** The
Anthropic Messages API has become a de-facto compatibility surface: DeepSeek
serves it directly (`https://api.deepseek.com/anthropic`), and the pattern of
retargeting an Anthropic-speaking client by environment is proven:

```json
{
  "env": {
    "ANTHROPIC_BASE_URL": "https://api.deepseek.com/anthropic",
    "ANTHROPIC_AUTH_TOKEN": "<injected, never committed>",
    "ANTHROPIC_MODEL": "deepseek-v4-pro[1m]"
  }
}
```

One client, many backends: Claude itself, DeepSeek, and anything else that
speaks the protocol, selected by base URL + model name + token in the job
spec's env block. This collapses what would have been a provider-per-vendor
zoo into a single backend type: `ANTHROPIC_COMPATIBLE`. The OpenAI-shaped
`/api/chat` path (Ollama et al.) remains a second type for endpoints that
never learned Anthropic's wire.

**2. Local models on the Apple stack (AFM / Core AI / MLX) — and the
constraint stated at full strength.** The constraint is not which model; it
is which *stack*. On this hardware, inference is only real inference when it
runs on the Apple-optimized stack — Metal and the ANE, reached through MLX
and, as of WWDC26, AFM and the Core AI framework (curated open models
including Mistral, ahead-of-time compiled for Apple silicon, with tooling to
convert PyTorch models). The measured basis is ours, not vendor marketing:
magpie's audio path ran **~70x faster across MLX than across torch** on the
same machine. A model that falls back to CPU torch is not a slower backend;
it is a different, useless category of computation. So the rule: **model
execution happens in a macOS process on the Apple stack, or it does not
happen.** There is no in-container fallback; a 70x-slower fallback is an
outage wearing a costume.

**Why the instance can never do this itself (researched 2026-07-10, state
named honestly).** Two locked doors, either one sufficient:

- *No Metal in the guest.* Apple's `container` 1.0 (June 2026) runs each
  container in a Linux micro-VM, and there is no native GPU passthrough: the
  maintainers state it plainly, and the hardware reality (Apple GPUs are not
  behind an IOMMU) means classic passthrough may never come. The WWDC26
  container-machines session does not mention GPU access at all.
- *No AFM in the guest, ever.* AFM, Core AI, and MLX are macOS frameworks —
  Swift APIs backed by Metal and the ANE that exist only inside macOS
  processes. A Linux guest cannot call them regardless of what GPU access it
  has; they are not present in that world to be called.

What does exist is the crossing: a host-side daemon with native Metal/ANE
access serving inference over vsock into the guest — community toolkits (the
"NVIDIA container toolkit for Apple silicon" shape) measure ~95% of native
MLX throughput through that seam, and Docker's Model Runner does the same
trick at the API level. The serialization tax is on prompts and tokens,
which is why it is small.

**The design consequence: the macOS boundary is the model-server boundary.**
The architecture's real seam is not "where the GPU is" but "where macOS is."
Everything Apple-optimized — AFM, Core AI, MLX, the ANE — lives host-side,
behind one model server with one queue; a haho instance in its Linux
container reaches it as an endpoint, exactly as it reaches the Anthropic
API. Every backend is an endpoint; the local ones happen to sit across the
vsock at native speed. If Apple later grants containers real GPU access and
a guest-callable inference surface — the community pressure is loud and
Apple has moved a long way in two WWDCs — the model server moves inside the
image and *nothing in the contract changes*. We are not blocked on Apple,
and we are positioned to benefit the day they ship.

## Externalized state: surrealdb, RAG, and the cache

Instances share nothing with each other except state that lives outside all
of them, in surrealdb:

- **The response cache.** A huge majority of harness work is repeatable;
  identical work should cost one model call, ever, across all instances.
  Per-entry TTL. This is why the cache is in surrealdb and not in the
  instance: a cache that died with its instance would be a private memo, not
  a cache.
- **The RAG store.** BM25 full-text search today, created/verified at
  instance startup (`EnsureSchema`), degraded-with-a-warning until the index
  exists. Vector search is a later decision — see Open questions for what
  would force it.

surrealdb runs in the fleet; instances reach it by exposed port (static
discovery) from the host, or k3s discovery when an instance runs in-cluster.
All user-supplied values are bound as query variables, never interpolated
into SurrealQL.

## Instances are plural

One instance per workload, arbitrarily many in flight. Nothing distinguishes
the first from the tenth: each has its own job, its own address, its own
landing. Ten instances chewing overlapping corpora do not do the same work
ten times — that is the shared cache doing its job.

Fan-out is bounded by the spawner's dispatch policy, not by haho: an
API-backed model tolerates wide fan-out; a metal-bound model is a contended
resource (one GPU does not run ten mistral-24b servers) that spawners budget
for in their own config. The host-side model server makes this budget
concrete: it is one queue in one place, not ten instances fighting for a
GPU none of them can see. haho's job is to make instance ten as cheap and
boring as instance one.

## Customers

- **holden** — one assessment, one instance; the first customer and the
  proving seam.
- **magpie-style fan-out** — many agent-like workers, each in its own
  harness, each with work it has to do.
- **wonderlib-style corpus work** — a sweep is a fan-out of chunked, cached
  workloads; the cache carries the overlap.

haho never learns what a ruling, an errand, or a corpus is. It runs the job
spec it was born with. Same contract, different spawner, every time.

## Contracts

Owned in `proto/` (buf v2), consumers vendor-generate, gen-freshness per the
mesh's standing invariant. What survives from the PoC and what changes:

- `haho.common.v1.ChatMessage` — survives; the one canonical message shape.
- `haho.filter.v1` — survives; filter config becomes birth-time-only.
- `haho.model.v1` — recast to the two backend shapes above
  (`ANTHROPIC_COMPATIBLE`, `OPENAI_COMPATIBLE`) plus endpoint + model name +
  env references; vendor names leave the enum.
- `haho.harness.v1.HarnessService` — **recast**. Submit/StreamSubmit/GetHealth
  describes a resident harness, which haho is not. The contract is re-drawn
  around the three moments: a **JobSpec** (birth, on stdin), a **JobEvent**
  stream and the status endpoint (descent), a **JobCompletion** (landing).
  The messages largely survive; the service definition does not. This happens
  before generated code spreads to any consumer, so it is a cheap correction
  now and a schema break later — hence it gates everything else in the
  migration section.

Health states (`idle`, `busy`, `waiting_subprocess`, `waiting_input`) survive
on the descent endpoint, where they answer the only question that matters
mid-job: is this instance still earning its keep. `GetHealth` is meaningful
ON a living instance, never BETWEEN instances — standing "is haho healthy" is
a spawner-side statistic about recent landings, not a question any instance
can answer.

## Secrets

The rules, in order of who holds what:

- **Values never transit the spawn chain.** An API key does not ride from an
  operator session through holden into a job spec — that path has too many
  hops, each one a log line waiting to happen. The job spec carries secret
  *references* (the name of an env var or mounted secret), never values.
- **The container runtime injects.** haho instances are containers, and
  containers have a sanctioned secrets mechanism: operator-provisioned
  secrets (Kubernetes Secrets in-cluster, the runtime's secret mounts on the
  host) materialize inside the instance as env or files at birth. The
  operator provisions once; every instance that names that reference gets the
  value from the runtime, not from the spawner.
- **An instance holds only what its job needs.** A job spec that names no API
  backend spawns an instance holding no API key. Startup fails loudly if a
  named reference resolves empty.
- **Nothing is ever hard-coded.** Not in the image, not in configs, not in
  the job spec, not in this repo. The operator injects values; everything
  else passes names.

## Observability

Structured JSON logging (fleet rule; bare prints are lint errors), `/metrics`
counters on the descent endpoint, progress events on stdout, and the
completion record as the terminal artifact. A spawner needs no log-diving to
know what happened: the completion record says, and disagreement with the
exit code is FAILED, loudly.

## Distribution

haho's deliverable is not a program you install; it is a capability a host
gains. Concretely, haho ships as two artifacts, versioned together:

1. **The baked image** — the instance root: binary, filters, backend
   clients, mapesis, config defaults. Commit-stamped like delightd's bake.
2. **The protos** — the contract consumers generate against.

A host that can spawn haho instances is a host that has a spawner (holden,
magpie, an operator at a shell) and can pull the image. There is no
`make install`, no host PATH entry, no per-host drift to manage: if two
hosts have the same image, they have the same haho, and "which haho is
running" is answered by an image tag instead of an archaeology dig.

## Graduation

haho is a dev project. It will be graduated to prod when this document is
ratified by the operator and fable and is marked read-only at
`docs/haho-rfc.md`, as is the usual convention, and the ratified document
will be used as the reference for the integrity of the project going
forward, as is the usual convention. Ratification is the gate; the e2e proof
below is what ratification is buying.

holden and haho are separate services and separate projects, but they share
a fate in this effort: holden does not serve in production against a dev
repo's contract, so this graduation is on holden's critical path and is
treated with that seriousness.

## The e2e proof

haho must be e2e provable from the day it is promoted, even as the barest
shell it will be when initially promoted: it must **loft, work, and land,
and be verifiable at each step of the way** — spawn from a job spec and
answer on its descent endpoint (loft, verified by querying it), drive its
model and move through its filter/cache path (work, verified by progress
events and cache statistics), and emit the completion record and exit clean
(land, verified by the record/exit-code agreement). That proof is in
addition to whatever unit and coverage tests it has, not a substitute for
them — and the reverse holds too: green coverage with no flight is not a
tested harness.

The full proof, once the cache rides: run a real job start to finish, then
run the identical job and watch the cache answer instead of the model —
spawner talks to instance, instance talks to model and surrealdb, the
product comes out, twice, the second time for free. If that cannot be
demonstrated, this design is not done, whatever the coverage number says.

## Open questions (named, not smuggled)

- **In-cluster instances.** The contract does not care where an instance
  runs, and the host-side model server pattern (Model backends, above) means
  even metal-model jobs no longer pin the *instance* to the host — only the
  model server. Whether any haho runs as a fleet pod is now purely a
  placement decision for spawners, and a weaker question than it was before
  the research pass. Deferred, comfortably.
- **Vector RAG — what it is and what would force it.** BM25 (the current
  index) is lexical: it ranks documents by term overlap with the query,
  which is exactly right when jobs quote code, error strings, and names —
  the mesh's usual currency. It cannot find a document that says the same
  thing in different words; that requires embedding text into vectors and
  searching by semantic distance, which costs an embedding model in the
  loop, an index rebuild story, and a tuning surface. The forcing function
  would be a real workload that retrieves by *meaning* rather than by
  *token* — corpus synthesis across paraphrased sources is the likely first
  one. Until a workload demonstrably misses with BM25, we do not pay for
  vectors.

## References (fetched 2026-07-10)

- Apple `container` 1.0 and Containerization framework; container machines:
  WWDC26 session 389 "Discover container machines" (no GPU/Metal mention).
- GPU passthrough status: apple/container discussion 62 — maintainers: "we
  do not currently support this"; Apple GPUs are not behind an IOMMU.
- Host-daemon Metal proxy pattern: RobotFlow-Labs/container-toolkit-mlx
  (Swift gRPC daemon, vsock, ~95% of native MLX throughput, early-stage);
  Docker Model Runner (host-side OpenAI-compatible API).
- AFM/Core AI: WWDC26 sessions 241 "What's new in the Foundation Models
  framework" (third-party models via the Language Model protocol) and 326
  "Integrate on-device AI models into your app using Core AI" (curated open
  models incl. Mistral, AOT compilation, PyTorch conversion).
- DeepSeek Anthropic-compatible endpoint: api.deepseek.com/anthropic.
