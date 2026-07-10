# haho 🪂 — the harness design

- Status: DRAFT (awaiting operator ratification)
- Date: 2026-07-10
- Deciders: Max (operator), Claude (agent — drafted by Fable 5)
- Companions: holden `docs/ADR-0003-holden-is-a-service.md` (the first
  consumer's side of this seam); the README (operational reference for the
  current PoC — it describes what runs today, this document describes what
  haho is becoming).

## What haho is

haho is the mesh's LLM harness: the one way work gets done by a model here.
Anything on the mesh that needs a model — an assessment, an errand, a corpus
sweep — describes the job, and haho runs it: filters the input, checks the
cache, talks to the model, filters the output, reports the result. We built
our own because the harness needs what our substrate already provides —
surrealdb for caching and RAG, mapesis, the contracts — natively, not bolted
on, and because we will be spawning harnesses often enough that "make shit up
each time" is a cost we decline to keep paying.

One haho instance runs one workload, and then it is gone.

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
   filter configuration, cache policy — and nothing else. Configuration is
   fixed at birth; there is no runtime reconfiguration of a one-job process
   (the PoC's runtime `POST /v1/filters` dies with this design).
2. **Descent.** Work can take a long time, so a living instance is queryable.
   It exposes a status endpoint for its lifetime — still here, still working,
   progress so far, the model it is driving — and emits progress events. The
   spawner records the instance's address at birth. The endpoint's identity
   dies with the instance; nothing answers on it between jobs, by design.
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
- **Model providers.** One interface, several backends: local models over
  Ollama (mistral-24b is the workhorse), Claude over the Anthropic API,
  DeepSeek over its OpenAI-compatible API. The job spec picks; the provider
  enum in `haho.model.v1` names backends, not vendors' marketing.
- **mapesis.** Available in-instance for jobs that need it.
- **The surrealdb client.** The instance's line to the only state that
  outlives it (next section).

The image is the "all the things" guarantee: holden (or any spawner) does not
assemble an environment, it names an image. A dirty harness is impossible
because there is no harness left to be dirty.

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
  exists. Vector search is a later decision, taken when a workload demands
  it, not before.

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
resource (one GPU does not run ten mistral-24b instances) that spawners
budget for in their own config. haho's job is to make instance ten as cheap
and boring as instance one.

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
- `haho.model.v1` — survives; provider naming trued to backends.
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

## Security posture

- Secrets are operator-injected env, never files, never argv, never logged:
  surrealdb credentials via `SURREAL_CREDENTIALS` (named by config), model
  API keys via the config-named env var, startup fails loudly if the named
  var is empty.
- The descent endpoint is read-only by design once runtime filter mutation
  dies; the control token guards anything that remains mutating.
- The instance carries only the credentials its one job needs — a job spec
  that names no API model spawns an instance holding no API key.

## Observability

Structured JSON logging (fleet rule; bare prints are lint errors), `/metrics`
counters on the descent endpoint, progress events on stdout, and the
completion record as the terminal artifact. A spawner needs no log-diving to
know what happened: the completion record says, and disagreement with the
exit code is FAILED, loudly.

## Distribution

haho ships as two artifacts, versioned together:

1. **The baked image** — the instance root: binary, filters, provider
   runtimes' clients, mapesis, config defaults. Commit-stamped like
   delightd's bake.
2. **The protos** — the contract consumers generate against.

There is deliberately no "install haho on a host" story: hosts run spawners;
spawners run images.

## Graduation: dev to prod

haho lives in `~/mesh/dev` because it is a skeleton. It moves to prod when,
and only when:

1. the contracts are recast around the three moments and gen-freshness holds;
2. the image bakes reproducibly and an instance runs a real job from spec to
   completion record;
3. the e2e proof below passes;
4. holden can consume it through the invocation contract with no holden-side
   special cases.

holden and haho are separate services and separate projects, but they share a
fate in this effort: holden does not serve in production against a dev repo's
contract, so this graduation is on holden's critical path and is treated with
that seriousness.

## The e2e proof

Spawn an instance with a real job spec; query the descent endpoint mid-flight
and see busy-with-progress; receive the completion record; watch the instance
exit clean. Then spawn a second instance with the identical job and see the
cache answer instead of the model. Spawner talks to instance, instance talks
to model and surrealdb, the product comes out — twice, the second time for
free. If that cannot be demonstrated, this design is not done, whatever the
coverage number says.

## Open questions (named, not smuggled)

- **Provider naming.** `MODEL_PROVIDER_OPUS` names a vendor's model tier, not
  a backend; the recast should name the seam (`ANTHROPIC`? `CLAUDE_CLI` vs
  `ANTHROPIC_API`?) — decided when the contract is redrawn.
- **In-cluster instances.** The contract does not care where an instance
  runs, but metal-bound models pin those jobs to the host. Whether any haho
  ever runs as a fleet pod (for API-only jobs) is a placement decision for
  the spawners, deferred.
- **Vector RAG.** BM25 until a workload proves it insufficient.
