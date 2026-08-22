# haho 🪂 — RFC

- Status: DRAFT. Ratification by the operator and fable marks this file
  read-only, and the read-only file is the reference for the integrity of the
  project from then on, as is the usual convention. haho ratifies as one
  tranche with the speedy RFC (`docs/speedy-rfc.md`) and the holden RFC
  (`docs/holden-rfc.md`): the three documents share one vocabulary and one
  worldview, and none of them ratifies alone.
- Authors: Max (operator); Claude (Fable 5, drafting). Review happens on the
  PR. This document is shared work — the operator's (eight hours of review on
  one revision alone) and an AI's — and that is stated plainly rather than
  hidden. Its primary reader is a machine: the document is a contract, and
  this mesh verifies future work against documents like it (holden rules
  code against exactly such contracts). It is long, and it is not tuned for
  human approachability, because that is not its first job. Going forward
  this is the expected shape of technical documentation here, not an
  eccentricity of the authors. The first acknowledgment of this way of
  working is [building wonder](https://github.com/janearc/wonder/blob/main/docs/building-wonder.md)
  — the first time the operator recorded that he was authoring together
  with something, and that the primary usefulness of the artifact was its
  machine readability.
- Companions: `docs/holden-rfc.md` (holden is haho's first consumer; the RFC
  continues holden's ADR-0003) and `docs/speedy-rfc.md` (the recipe semantics
  haho implements for chutes — speedy does not require haho, and haho does
  not define speedy) — haho, holden, and speedy are one imbricated document
  set and ratify as one worldview. Project names used throughout are defined in
  section 18.
- Code convention: illustration code in this document is scheme — it reads
  well, and it cannot be mistaken for a commitment. Where the document shows
  Go, python, Swift, or protobuf, we mean it: that code is functional or is
  the contract itself, and we commit to it. The sandbox profiles are SBPL,
  which is already scheme; the platform got there first.

> "Have you ever tried to develop through Docker? It's sometimes a huge
> pain in the ass … it works on any machine you use it — like, **it works
> on any machine but mine.**"
> — ThePrimeagen[^12]

That quote is the gap this project lives in. A big-company engineer writes
software that deploys to a thousand machines at a button press — and cannot
run it on the Mac laptop in front of them, the most capable machine they
own. The tooling that makes software run anywhere makes it run anywhere
except here; the fruit is forbidden. big-little-mesh came to be in that gap,
haho is the piece that bridges it, and this document is for two readers: the
engineer who wants to run real backend software on their own laptop, and the
engineer who merely recognizes the position and wants to see it solved.

The section headings are the index — the rendered view generates an outline
from them, and `grep '^## '` does the same for a shell. Appendices: A — the
70x, reproducibly. B — the ANE serializes, measured. C — the two ANE doors,
measured.

## 1. What haho is

haho exposes borg-shaped things to fruit-flavored jobs.

That sentence is the project, and the direction of it matters. The job —
transcribe this audio, smooth this transcript, review this diff, distill
this document — stays fruit-flavored: a native macOS process with the GPU,
the Neural Engine, and the OS's warm foundation model an arm's reach away.
The borg-shaped world is delivered *to* it, as services: a typed spec,
admission control, a lease vouching for its liveness, kafka carrying its
completion, surrealdb and redis as memory that outlives it. No
Apple-flavored process ordinarily has any of those things; no
kubernetes-flavored process ordinarily has that hardware. haho is the
environment that furnishes both at once — and environment is the right
word, more than harness: a harness constrains a thing, an environment
furnishes one. haho decides nothing about what a job means; it executes the
job the way the spec says, against the hardware the spec names, and reports
the result.

The reason haho exists is cost. The same unit of work can be done many ways
— on the GPU, on the Neural Engine, on a local model, or by a paid API on
the other side of the network — and the difference between the right way
and the wrong way is not a percentage. It is the difference between
feasible and infeasible (section 4, and Appendix A: 70x, measured on this
hardware), or between free and $2.58 per diff review. Feasible is not a
shrug here — not "too expensive" or "can't be done" — it is defined
mathematically in section 2, as a computed set; and the set usually has
more than one member, because there is more than one way to do it.[^13] Models get cheaper
and more capable over time, so the dollar figure is a snapshot — but a
dozen reviews a day must not bill an api a dozen times for work this
machine does free. haho is the piece of software that lets every pipeline
on this mesh do its model work the right way without hand-wiring hardware
knowledge into each one.

The hardware under haho is Apple end to end: one SoC whose GPU, Neural
Engine, and CPU share unified memory, an OS that keeps a foundation model
warm and offers transcription and translation as system services. Taking
that hardware seriously is haho's primary defining characteristic, and
everything in this document follows from it. On the day the hardest
pipeline on the roadmap is built — live interpreting of a room, described
in section 12 — its mesh side reduces to two haho job templates and a
dispatch loop. That reduction is the point of the design.

The first flight is `haho-hello-world` — the smallest complete jump, named
here because it is graduation stage 0 (section 15). holden is the first
*consumer*, and it doubles as the integration test for everything after it:
one job, one API-backed model, whole or nothing. The consumers after holden
(section 12) use progressively more of the interface, up to work across
every lane at once.

## 2. The discernment equation

Every job haho runs is an instance of one question:

> What is the fastest way to accomplish this unit of work with a reasonable
> degree of accuracy, such that the value of the work is proportionate to
> the cost of the processing needed to accomplish it?

Stated as the equation the rest of this document keeps referring to, in the
illustration register. For a unit of work `w` and the candidate placements
(the lanes of section 4, plus the paid APIs):

```scheme
;; the discernment equation
(define (feasible w)
  (filter (lambda (p) (capable? p w)) placements))

(define (cost w p)
  (+ (dollars p) (energy p) (occupancy w p)))

(define (place w)
  (argmax (lambda (p)
            (/ (* (value w) (accuracy w p))
               (cost w p)))
          (feasible w)))
```

`occupancy` is the opportunity cost of holding a contended lane (section
10): a job that pins the GPU for ten seconds costs every other GPU job ten
seconds, and the equation must see that. `accuracy` is fitness for purpose,
not a benchmark score: a transcription that misses proper nouns may still be
fit for cutting audio into segments and unfit for a permanent record.
`value` is the requestor's, and it may decay with arrival time — a
placement too slow to be useful prices itself out through the value term,
never through enforcement of haho's: the mesh makes no velocity promises
(speedy RFC, section 8), and haho is mesh.

Choosing a placement by this equation is **discernment**. Discernment is the
combination of dispatch, orchestration, and scheduling: given a job, decide
where it runs. In this version of haho, discernment is **external**: the
requestor works the equation and writes the answer into the job spec (which
backend, which framing). haho enforces the answer — admission
control, lane budgets — but does not second-guess it. Internal
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
  metal lane — the placement whose answer arrives while it still matters is
  the one the value term priced in.

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
  leave the device and have no concept of a bus. Apple-ness has a second
  dividend: it hitches the work to a (probably) benevolent producer whose
  platform accrues capabilities over time, and software built against the
  chain inherits each new link free. The same bet is more precarious on
  Linux, where nobody owns the whole chain.

The same integration cuts the other way, and honesty requires saying so:
the ecosystem restricts what can be invented against it. Building a
realtime interpreting pipeline on macOS is hard; on iOS it is harder still.
The nice interfaces and the tight hardware-software integration are also
the shackles — Apple's frameworks decide what a process may do with the
silicon, and inventing something the platform did not anticipate means
prying at seams the platform does not advertise. haho is that pry-bar:
it opens the seams that exist without leaving the platform, which is the
only place the fast paths are.

No existing runtime is both. Kubernetes-shaped tooling on this platform puts
work inside Linux virtual machines where none of the Apple hardware or
frameworks exist (section 5). Apple-native software has no job contracts, no
scheduler, no fleet. We need a name for the combination, so: **borg-shaped,
fruit-flavored**. Borg-shaped is the grammar:

- Work is described by typed specs, never by convention.
- Execution emits events; completions are durable records.
- Placement is a scheduling decision against declared resources.
- Health is an endpoint.
- Everything can be observed while running and audited after landing.

Fruit-flavored is the hardware reality: the resources under the scheduler
are one SoC's execution units and one OS's frameworks, bound tightly enough
that a transcription job and an interpretation job can plausibly run at the
same time without slowing each other down — on different execution units of
the same chip (section 4 states this precisely, including what is
hypothesis and what is fact). The phrase "tight fruit binding" appears
throughout this document's ancestry and means exactly this: the design
leans on Apple's hardware-software integration on purpose, because that
integration is where the cost term of the discernment equation collapses.

**The deliberate investment in a walled garden — curated and provided by a
closed enterprise — is a choice, and it is the only rational one here.**
The laptop arrives with its software; the choice that remains is between
forever renting intelligence for work the local machine is capable of, and
building the software the machine lacks — fastening the tool to the
unstated but implied aspirations of the company that makes the hardware.
When a laptop is mandatory for the work but income from the work is not
guaranteed, consolidating on the hardware, its producer, and the software
bundled with it is not ideology; it is arithmetic. The bet is directional
and should be stated as one: the current on-device foundation model is
small, and Apple's trajectory — the hardware, the Foundation Models
framework, the curated open models AOT-compiled for the platform[^5] —
points at bigger models on the same free, warm, local terms. Building haho
against that trajectory is a mortgage, not a sunk cost: the payments are
the constraint-work in this document, and what the payments buy is that
every capability Apple ships lands in an already-built harness with its
cost term already near zero. If we did not believe the trajectory, the
honest alternative would be a cloud provider and none of this.

What this is not, precisely:

- **As described and implemented, this is not a distributed system.** The
  pieces that would allow one exist — the bus, framing, external caches —
  but at n>1 the design philosophy changes so substantially that haho as
  specified would be moot: a rack of machines does not need carefully
  discerned chutes, it needs a real cluster scheduler. haho is an n=1
  design, and the equation's cost term is computed for one machine's
  resources.
- **Compute stays home; participation does not.** Concretely: every model
  call and every chute executes on this machine. Mobile, wearable, and
  ambient devices participate in the mesh — a phone on the same wifi asks
  this machine to do work the phone cannot, through an ephemeral cloudflared
  tunnel (*intruder*, section 18) — but a participating device only submits
  work and receives product; nothing executes there. The phone is a
  requestor, not a worker. The equation covers this case too: same-owner
  same-network hardware is a placement whose dollars are zero and whose
  latency is wifi.
- **The cloud is not the enemy.** When discernment shows a job is
  *provably* infeasible on the localhost, it goes to claude or deepseek
  with no complaint. What the design refuses is defaulting to paid
  placement without proving local infeasibility first — for often-repeated
  work especially, that default is an abdication of responsibility toward
  cost.

The failure this principle exists to prevent is a design failure, not a
runtime one. It is the failure that necessitated haho: on this platform, no
Dockerfile, no container runtime, and no virtualization product grants
access to the hardware Apple carefully designed to run these workloads
cheaply. Kubernetes is good. Containers are good. Apple silicon is good.
Nothing ships that lets one piece of software have all three, so haho is
the piece of software that arranges them: kubernetes-shaped where the fleet
lives, native where the work lives, contracts across the seam. *We wish to
have our 🍰 and eat it too.*

## 4. Execution lanes

Work placement on this machine, by hardware path:

| Lane | Entered through | Runs | Character |
|---|---|---|---|
| metal (GPU) | MLX, via a resident model server | whisper large-v3, mlx-lm models | highest local throughput; contended — budgets in section 10; the 70x side of Appendix A[^8] |
| ane (Neural Engine) | AFM (Foundation Models)[^4], Speech (`SpeechTranscriber`), Translation, CoreML[^5] | the on-device foundation model[^10]; on-device transcription; on-device machine translation | designed to run concurrently with the GPU; low power; small coherence windows (section 9); concurrency is per-door, measured (Appendices B, C) |
| api (network) | HTTP to a model endpoint[^7], or exec of an agent CLI that speaks to one on its own behalf (`CLAUDE_CLI`) | claude, deepseek, anything with a served model | capable of what no local model is; costs dollars and round trips |
| cpu | plain code | chunking, filters, synthesis assembly, voice-activity detection (VAD — deciding where speech starts and stops) | simple code patterns |

torch does not appear in this table on purpose: haho is *fruit-native* by
definition, and torch on this hardware loses the cost equation by roughly
two orders of magnitude (Appendix A). A job that only torch could run does
not meet discernment's threshold for local placement at all — its occupancy
is assessed so high that, by the same equation, it is cheaper to just ask
claude. That job belongs to the api lane.

Three statements about the lanes, and how much trust each deserves:

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
   to convert this hypothesis into a measurement before the cross-lane
   pipelines depend on it. Note the promise being made is **speedy —
   near-real-time — never realtime** (speedy is a defined term, section 18).
3. **Fact.** The Neural Engine has no direct programming interface — there
   is no instruction stream or bytecode a process can hand it. CoreML, AFM,
   and Speech compile and place work on it at the framework's discretion; a
   process asks for the task, and Apple's runtime decides what silicon
   serves it.[^9] haho therefore schedules *lanes*, and accepts Apple's
   arbitration underneath them.

### The Apple words, one line each

Apple's naming is genuinely unstable — similar words, shifting meanings,
terms that change between WWDCs — so these are this document's fixed
meanings. Convention: lanes are lowercase (`metal`, `ane`, `api`, `cpu`);
devices and frameworks keep their proper names.

- **Apple silicon** — the SoC family (M-series here): CPU, GPU, and Neural
  Engine on one die with unified memory.
- **ANE** — the Apple Neural Engine, the dedicated inference device on the
  SoC. What the ane lane schedules.
- **Metal** — Apple's GPU API. The metal lane's hardware door.
- **MLX** — Apple's array/ML framework. Runs on the GPU via Metal. Never
  touches the ANE — a conflation this mesh has made more than once, hence
  this list.
- **AFM / Foundation Models** — the on-device foundation model and the
  framework that serves it. Routes to the ANE at the framework's
  discretion.
- **Speech / `SpeechTranscriber`** — the transcription framework; ANE.
- **Translation** — the on-device machine-translation framework; ANE.
- **Core AI** — the WWDC26 framework for running curated open models on
  Apple silicon.[^5]
- **CoreML** — the classic compile-a-model door to the ANE (and GPU and
  CPU); nothing here uses it directly today.

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
also a concession of the point: the model work happens host-side anyway.
haho draws the obvious conclusion — the whole chute runs on the host, and
there is nothing left to broker.

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
(allow network-bind                  ; the chute's read-only status endpoint
  (local tcp "127.0.0.1:*"))         ; (section 7): loopback, ephemeral port
(allow network-outbound
  (remote tcp (param "HAHOD_ADDR"))  ; the daemon, always
  (remote tcp (param "MLX_ADDR"))    ; present only when the spec names the
                                     ; MLX backend; that server's one port
  (remote tcp (param "CACHE_ADDR"))  ; present only when the spec enables a
                                     ; cache; the named store's one port
  (remote tcp (param "API_HOST")))   ; present only when the spec names an api
                                     ; backend; the spec's base_url host,
                                     ; nothing wider
;; plus the framework services the declared lanes require, and no others
```

Read the allowances carefully, because they are the security model in
miniature: a job's network reach is derived from its spec, and loopback is
not a blanket grant. The profile enumerates the specific local ports the
spec implies — hahod always, the MLX server only if the spec names that
backend, the named cache only if the spec enables caching — so a chute
cannot browse the machine's other local services. A spec that names no api
backend produces a profile whose outbound reach is exactly those enumerated
loopback ports and nothing else on any network. The scratch directory is
created at process launch and deleted at exit; a crash leaves it behind,
and the sweeper reaps any scratch whose chute is gone — disk exhaustion is
the failure mode that rule exists to prevent.

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

whisper large-v3 takes seconds to load and pins gigabytes of unified
memory; loading it per chunk would spend the whole flight on residency. AFM
is resident because the OS keeps the foundation model warm — a direct
consequence of the platform choice. Speech and Translation likewise. So the
resident layer is small and specific:

- **The MLX model server** — a host process holding whisper and any mlx-lm
  models warm on the metal lane, serving loopback HTTP. This is owned by the
  mesh's existing Apple-silicon Swift work in big-little-mesh
  (`big-little-mesh/apple-silicon`[^11]); haho is a client of it, not an
  owner. It holds no job state. The two version independently on one host;
  compatibility is a brew-upgrade-grade concern — the bench suite catches
  drift — not a fleet-grade protocol negotiation.
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

1. **Creation.** `hahod` accepts a `JobSpec`, admits it against lane
   budgets (section 10), and creates a **harness** — the scope and sandbox
   the chute executes in. Jobs are immutable once created and accepted to
   run: there is no reconfiguration of a job in flight. The creation record
   carries the job id and the chute's status address (loopback, ephemeral
   port).
2. **Descent.** The chute is queryable but not directable: the status
   endpoint answers with state, time in flight, progress, lane and model in
   use — read-only, for the whole working life. Progress events flow to
   `hahod` (section 13). Work can take a long time; a living chute can
   always say so.
3. **Landing.** One terminal `JobCompletion` — outcome, result, model used,
   chunk and cache statistics, wall time. The process exit status is the
   coarse cross-check: a completion/exit disagreement is treated as FAILED
   by `hahod` regardless of what either says alone. The scratch directory
   is deleted; the process is gone.

Chutes are plural: one chute per unit of work, arbitrarily many in flight,
bounded by lane budgets rather than by the contract. The budgets are
measured facts about the hardware, not tuning opinions — and the
measurements disagree in an instructive way: the ANE's two front doors
behave differently under concurrency (AFM queues strictly, Speech runs
concurrent — Appendices B and C), which is why budgets attach to *doors*,
not to the lane, and why plural chutes never means plural tenants on a
serial device.

## 8. The programming interface

### The daemon, the harness, the chute

Three words, held apart on purpose. `hahod` is the resident daemon. A
**harness** is what hahod creates per accepted job: the scope and sandbox
one chute executes in — a subordinate but not fixed process, in that it
daemonizes (no stdin, no controlling terminal) and runs until it believes
it is done. Having no terminal, a harness's stdout and stderr are captured
by hahod and ride its structured log. SIGTERM to a harness means
drain-and-land — finish the chunk in hand, emit the completion, exit;
SIGKILL means the sweeper cleans up after it. The **chute** is the unit of
work moving through that harness.
Residency is deliberate and narrow: **the daemon is resident; every
harness and its chute are ephemeral.** Two facts force a resident daemon,
and they are easier one at a time. First: lane budgets are machine-wide
facts, so exactly one admission point must see every job (section 10
records the review catch that forced this). Second: a session exists
before its task arrives, and that gap needs a home. So hahod holds
sessions and queues, and does no model work.

hahod keeps exactly **one store** — bbolt-shaped, on disk, survivable —
recording sessions, harness process ids, and their scopes. This is a
deliberate consolidation: the same rows the sweeper walks are the rows the
hm leases mirror, and nothing else on the machine keeps a second copy. hm's
lease state is kube-native and *derived* (from heartbeats), not a database;
harnesses and chutes persist nothing of their own.

hahod's responsibilities are few and closed, and worth enumerating because
"thicker than expected" should still be countable: sessions, with an idle
TTL of hahod's own (a session created but never tasked sits *before* any
lease exists, so nothing else can reap it); admission against lane
budgets; the **sweeper**, which walks the store, kills any harness whose
scope is no longer valid, and reaps orphaned scratch directories left by
crashed chutes; publishing completions to the bus; and
holding the hm leases of section 13. Those leases complete a reaping
invariant: heartbeats begin when a harness accepts tasking — no valid
tasking, no heartbeats; no heartbeats, no lease renewal; no renewal,
reaped. A fallow harness structurally cannot outlive one heartbeat window,
not because a cleanup job hunts strays but because existence-without-scope
has no mechanism to sustain itself. hahod kills; hm observes and alarms;
the fleet never reaches onto the host. The sweeper's jurisdiction ends at
the host, and section 17 records the mesh-side half of the problem it
solves locally.

Clients speak to hahod over loopback HTTP. The `haho` CLI is a thin
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
execution, no shell. The shape, in scheme (the committed client is Go):

```scheme
;; a requestor, sketched
(define daemon (haho-dial))                  ; loopback; hahod's socket
(define session (haho-new daemon))           ; quiescent until tasked

(define events (haho-task session job-spec)) ; admission errors surface
                                             ; here, loudly
(for-each relay events)                      ; creation, heartbeats,
                                             ; progress, chunk misses,
                                             ; cache hits
(define done (haho-completion session))      ; the terminal record
```

Completions are also published to the bus (kafka) following the bento
convention — because frood does this — so a requestor that fails
mid-descent, or a pipeline three hops away, reads the landing from the
mesh's durable record rather than from a pipe that no longer exists.
Requestor-side delivery and bus publication carry the same record; the bus
copy is the one of record.

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
  reserved 10; // a Deadline lived here in drafts; the mesh makes no
               // velocity promises (speedy RFC, section 8)
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
// MLX, ane for AFM/SPEECH/TRANSLATION, api for the *_COMPATIBLE kinds and
// for CLAUDE_CLI) and a kind/lane mismatch is therefore impossible to
// express.
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
    CLAUDE_CLI           = 7; // api lane: an agent CLI spawned as a
                              // subprocess (`claude -p`), carrying its OWN
                              // authentication -- base_url and token_env
                              // MUST be empty and are refused if set.
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
    MARKDOWN_SECTIONS = 3; // cut on section boundaries. The full cutting
                           // rule, including non-markdown input, belongs to
                           // the chunker implementation (section 9) — the
                           // contract names the chunker, never its mechanics
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

// The trailing synthesis step over per-chunk results. The contract names
// the modes; assembly mechanics belong to the implementation (section 9).
// CONCAT is an ordered join with no model. NONE returns the per-chunk
// results as-is.
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

// Cache: memoization of model calls, shared across chutes and across time.
// A chute's heap dies with the chute — the point is that the NEXT chute, or
// tomorrow's re-run, never re-asks the model — so the cache lives in a
// furnished store, not in the process. Before a chunk is sent to a model,
// the chute computes key = hash(backend kind, model, model_fingerprint,
// params, filtered chunk) and asks the store; a hit skips the model call
// entirely. model_fingerprint keeps an upgraded model from serving stale
// answers (section 17).
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
    reserved 3;       // DROPPED_LATE lived here in drafts; the mesh never
                      // drops (speedy RFC, section 8)
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
{"jobId":"sig-corpus-c041","state":"busy","inFlightMs":1290,
 "stage":"map","chunksDone":2,"chunksTotal":4,"lane":"ane","model":"afm"}
```

### Three worked specs

holden's assessment. One job, api lane, whole or nothing:

```json
{
  "jobId": "ruling-delightd-121",
  "payload": {"messages": {"messages": [
    {"role": "user", "content": "<the assembled ruling prompt>"}]}},
  "backend": {"kind": "CLAUDE_CLI", "model": "claude-fable-5",
              "params": {"otel_resource_attributes":
                         "mesh.component=holden-judge,mesh.repo=delightd,mesh.pr=121"}},
  "framing": {"strategy": "WHOLE_OR_ERROR"},
  "synthesis": {"mode": "NONE"},
  "cache": {"kind": "NONE"},
  "allowPartial": false
}
```

This spec named ANTHROPIC_COMPATIBLE with a `baseUrl` and a
`tokenEnv` until 2026-08-21. It was changed because it described a holden
that does not exist: the judge spawns the agent CLI, which carries its own
authentication, and the api-key form would have moved the cost of every
ruling onto a key the operator does not use for it. The kind carries no
`baseUrl` and no `tokenEnv` — both are refused rather than ignored, because a
descriptor naming a credential nothing reads looks configured and is inert.

`otel_resource_attributes` is not decoration. The judge's telemetry is what
makes "what did this ruling cost" answerable per repo and per pull request; a
requestor talking to hahod cannot reach hahod's child process, so the
attribution has to ride in the spec or the accounting is lost on the way.

A wonderlib distillation. The requestor has decided — this is discernment,
worked externally — that no large local model comes up for this: the
document is chunked through the on-device foundation model, and those
per-chunk results are the map half of mapesis. The synthesis half runs on
the local ollama mistral. One disambiguation matters here: this is the
6-bit gguf quantization served by ollama over the api-shaped loopback door,
which is a different artifact from the Mistral Apple advertises for the
Apple stack via Core AI[^5] — same family, different weights, different
lane, and a spec names which one it means. The escalation runs local
because the entire point of this machinery is to avoid paying an api for
work the machine does free; the paid api lane is the last resort, not the
default:

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

lloyd's smoothing of one segment. lloyd is the
text-smoothing frood (the name because "destupidification" is not a
suitable name for a piece of software, section 18), and inside the
interpreting pipeline it is the stage between transcription and delivery.
A 50-to-100-word segment fits the window whole, so there is no mapesis
here — mapesis over two sentences would be machinery for its own sake;
resize is the contingency, concat the reassembly, and the tight backoff
keeps a distressed retry from stalling the segments behind this one:

```json
{
  "jobId": "lloyd-8841-s17",
  "correlationId": "sess-8841/seg-17",
  "payload": {"text": "<50-100 words of whisper output for this segment>"},
  "backend": {"kind": "AFM"},
  "framing": {"strategy": "RESIZE_ON_FAIL", "chunker": "SENTENCES",
              "chunkSize": 3, "maxAttemptsPerChunk": 2,
              "backoff": {"initialMs": 50, "multiplier": 2.0, "maxMs": 400}},
  "synthesis": {"mode": "CONCAT"},
  "cache": {"kind": "SURREALDB", "ttlSeconds": 900}
}
```

The pipeline this spec sits inside is mackerel, end to end: an audio
feed arrives at a client-side buffer that knows where to cut; each cut
segment goes to whisper on the metal lane; that text needs lloyd (the
spec above); lloyd's output is rendered to audio in the listener's
preferred language and delivered alongside the smoothed transcript. The
transcription hop is one more spec, not a special case: `backend {kind:
MLX, model: "whisper-large-v3", params {task: "translate"}}`, payload
`raw` audio, framing `WHOLE_OR_ERROR` (the client cut the segment to size
before sending it). Rendering and delivery — including what a late
segment means — belong to the terminus renderer (speedy RFC, section 11).

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
The current on-device foundation model has a small window (it fails visibly
on a 100-line markdown file). claude's is enormous. Framing is how haho fits work
to whatever window the chosen backend has, and it is the direct descendant
of what wonderlib built (section 18): profile the corpus, cut the hard parts
smaller, feed a small model many digestible pieces, synthesize once at the
end. The economics carry over unchanged: holden chunks diffs through local
models today not because claude cannot read a diff whole, but because $2.58
per review is not a price you pay at a merge gate.

First, the term the ladder turns on. A **window miss** is one specific
failure: the model failed *because the input exceeded its coherence window*
— a context-length error, output truncated mid-thought, or empty output on
oversized input. It is not a failed re-read (that is an attempt failure,
below) and not an empty map result. Window misses resize, when the strategy
allows; every other error — auth, network, a resident server down —
propagates immediately and honestly.

The ladder, as a chute executes it (illustration; the committed
implementation is Go inside the chute):

```scheme
(define (run-framing job)
  (if (eq? (strategy job) 'chunk-always)
      (run-chunks job)
      (let ((result (attempt (whole job))))
        (cond ((produced? result) result)
              ((or (eq? (strategy job) 'whole-or-error)
                   (not (window-miss? result)))
               (fail result))                   ; real failures propagate
              (else                             ; window misses resize
               (emit (chunk-miss result))
               (run-chunks job))))))

(define (run-chunks job)
  (synthesize job                               ; none | concat | mapesis
    (map (lambda (chunk)
           (or (cache-get (chunk-key chunk))    ; a hit is a free chunk
               (let ((out (attempt-with-retry chunk)))  ; re-read on fail;
                 (cache-put (chunk-key chunk) out)      ; backoff on distress
                 (emit (progress chunk))
                 out)))
         (cut (chunker job) job))))
```

The mechanisms, made concrete enough to implement:

- **Window-miss detection maps error classes, per backend — and the
  interface is frozen here.** Every backend implementation MUST provide the
  mapping `error -> { window_miss: bool, retryable: bool }`: window-miss
  true routes the error to the resize arm of the ladder, retryable true
  sends it to the attempt-with-retry loop, and both false propagates it
  immediately. The interface is contract; the per-backend mapping *tables*
  remain open and are learned from operationalizing wonderlib — for api
  kinds the provider's context-length error class and truncated output; for
  AFM the framework's size errors and empty output on non-empty input; for
  MLX the server's declared limits.
- **The window itself gets a benchmark.** `haho bench` (the suite is
  defined in section 15) measures, per backend and per hardware, the
  coherence boundary: declare N as the maximum input (in the chunker's
  units) before the re-read failure rate crosses a threshold, and fit the
  decoherence coefficient D — the rate at which trustworthiness falls as
  input grows past N. Default `chunk_size` per chunker ships from those
  measurements. The same benchmark re-runs on new hardware and new model
  releases so the defaults track the platform instead of fossilizing.
- **Re-read on fail.** Before an output is accepted, the chute re-reads it
  — two checks in code, not a component: is the output well-formed for the
  job's declared shape (jinja2-templated jobs declare one; plain-text jobs
  get the degenerate check of non-empty, non-error-phrase output), and does
  it address the chunk it was given. A failed re-read counts against
  `max_attempts_per_chunk` and triggers backoff.
- **Model distress is measured, not guessed**: the re-read failure rate
  over the last k attempts on well-formed chunks — the bench's threshold
  arithmetic applied live. Rising distress engages the backoff multiplier;
  the requestor sees it as `Degraded` events, never as silently worse
  output.
- **The cache key** is `hash(backend kind, model, model_fingerprint, params,
  filtered chunk)` — content-addressed, and its usefulness is concrete: a
  corpus re-run after a crash resumes at the first uncached chunk; a room's
  repeated announcements are answered for free; the same question
  never bills a model twice, across all chutes and across time. The
  fingerprint keeps a model upgrade from serving yesterday's answers
  (section 17).

Chunker mechanics live here, not in the contract: MARKDOWN_SECTIONS cuts
recognized markdown on section boundaries, and wraps anything else
exhaustively in xml and cuts on elements — wonderlib's rule, carried
forward as chunker behavior.

mapesis is the map half plus the synthesis half: chunk outputs are the map;
the synthesis pass — usually on a wider-window, cheaper-per-token lane than
the map — produces the merged artifact. For MAPESIS synthesis the per-chunk
outputs are assembled in chunk order into a single Messages payload, and
the instruction template (jinja2) renders with the outputs bound to
`chunks`; that rendered result is the synthesis model's prompt. Map on the
small free lane, synthesize once on the sturdy one, escalate to the api
lane only when the equation says so.

## 10. Dispatch and lanes

Dispatch in haho is borg in miniature, and it lives in exactly one place:
`hahod`. An earlier draft put admission in a per-requestor library;
review caught the contradiction — two requestors each enforcing "one metal
tenant" locally admit two metal tenants globally. Budgets are per-machine
facts, so the machine gets one admission point (section 8), and
requestors are clients of it.

```json
{
  "lanes": {
    "metal": { "budget": 1,  "queue": 4  },
    "ane":   { "doors": { "afm":    { "budget": 1 },
                          "speech": { "budget": 3 } },
               "queue": 8 },
    "api":   { "budget": 16, "queue": 64,
               "kinds": { "CLAUDE_CLI": { "budget": 1, "queue": 8 } } },
    "cpu":   { "budget": 8,  "queue": 32 }
  }
}
```

The api lane carries one PER-KIND bound, and it is the first of its shape in
this table. `CLAUDE_CLI` is limited to one at a time because a job of that kind
is an agent CLI session: minutes long, and spending the operator's own
allotment rather than a metered key. Two at once is not slowness, it is
expense, and the ruling ledger has already paid for it once — a duplicate pair
against one pull request, judged twice.

The bound is a bound, not a lock: a second job QUEUES and runs when the first
lands, which is what a requestor submitting two rulings back to back actually
wants. Only a full queue is refused, by name, so the operator can tell which
bound she hit.

One departure recorded rather than shipped around: a `CLAUDE_CLI` job takes its
own gate INSTEAD of the api lane's, not in addition to it, so the api lane can
carry `budget + 1` in the worst case — 17 rather than 16. Taking both gates
would be more faithful to "a lane budget bounds the lane"; it was not done
because a ticket holds one gate, and that held/queued state machine already
produced one real bug found by adversarial review. Rewriting it to bound a lane
6% tighter, against a budget nothing in this estate approaches, is not the
trade. The overshoot is one job, it is bounded, and it is written here.

The metal budget of 1 is tied directly to the lane table's "contended"
entry (section 4), and "heavy" has a definition: a job is heavy on a lane
when its expected utilization of that lane approaches 1.0 — it saturates
the resource for its duration, so lane throughput is serial in heavy jobs.
One whisper job is heavy on metal: `budget = floor(1 / utilization) =
1`. A future weight-class refinement can admit several *light* metal jobs
(small mlx-lm models at fractional utilization) under the same rule;
the budget arithmetic already accommodates it, and nothing changes at the
contract.

The ane budgets are **per-door**, and both are measurements rather than
estimates — and they differ. AFM's door serializes strictly (Appendix B):
concurrent submissions do not degrade it — service time stays constant and
the excess simply waits — so a queued job's expected start is queue-depth
times the measured service time, exact arithmetic instead of a guess. A
requestor whose value decays with time can read that arithmetic before
submitting; haho states it and enforces nothing about it, because the
mesh makes no velocity promises (speedy RFC, section 8). It is an
*AFM-door* property, not an ane-lane property. Speech's door is genuinely
concurrent (Appendix C: three heavy transcriptions at ~20% per-job cost,
wall at 0.42 of serial), so it takes an ordinary budget check, shipped at
the measured floor of 3. One welcome consequence: the interpreting
pipeline's transcribe and smooth stages contend less than the single-lane
model feared. The api budget is wide because someone else owns those
GPUs, and api latency is weather, not arithmetic. A job that arrives at a
full queue is REFUSED at admission, loudly, before spending — the same
posture as everywhere else on this mesh, and it requires exactly the
single queue view that centralizing admission provides.

That is the entire scheduler: a few dozen lines against a four-row resource
table, still recognizably the borg grammar — declared resources, admission
control, observable placement — at the scale of the actual machine.

## 11. Sidecars and haho

The mesh's pattern for resident host-native services is the sidecar: paling
runs its inference daemon host-side, owning its own models locally, and
`paling-sidecar` runs in the fleet carrying everything the mesh requires of
a frood — registration with delightd, health reporting, bus adjacency;
in a word, it keeps its resident froody (the term is defined in section
18). A sidecar exists because a host-native resident cannot live inside the
fleet, and the fleet still needs it to behave like a frood.

haho covers the case the sidecar cannot: ephemeral work that needs the
hardware the fleet cannot reach. The two do not overlap. haho describes the
boundary as:

| | sidecar | chute |
|---|---|---|
| exists for | a resident service | one job |
| lifetime | the service | the job |
| carries | its resident's froodiness | a JobSpec and nothing else |
| runs as | fleet pod | sandboxed native process |
| created by | deployment, with its service | hahod, at admission |

A chute does not get a sidecar. Its fleet-side representation is the lease
hahod maintains for it (section 13), which costs nothing and dies with
the job. If anything, haho strengthens the sidecar's definition: a sidecar
is *only* the compliance carrier for a resident — any model work a resident
wants is a JobSpec like everyone else's.

## 12. The first consumers

Each consumer requires more of the interface than the one before, and each
is a real workload:

1. **holden** — one job, api lane, `WHOLE_OR_ERROR`, no cache,
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
   at once, the cache as latency insurance,
   correlation ids threading the phone's capture through the mesh and back.
   Its mesh side reduces to two spec templates — transcribe on metal,
   interpret on ane — and a dispatch loop, which is the reduction section 1
   promised.

## 13. Observability

- **The events.** Creation, heartbeats (the mesh's standard
  `ServiceHealthHeartbeat`), progress, chunk misses, cache hits,
  degradations — delivered to the requestor over its harness connection.
- **The status endpoint.** Pull-based truth mid-descent, per chute,
  read-only, loopback (section 7).
- **The completion record.** Published to kafka following the bento
  convention at landing; the bus copy is the copy of record, and the
  requestor's synchronous copy is a convenience. The record carries model,
  lane, chunks, cache hits, tokens, and wall time — everything, because
  logs could not reconstruct the flight even in principle: the work happened
  on the metal, inside frameworks that do not narrate. Full model inputs
  stay OUT of the record on purpose — bus bloat, content leakage — and
  replay or audit reaches the inputs through the content-addressed cache
  keys the record carries.
- **The lease.** Every chute has an explicit lease with hm, and the lease is
  a contract like anything else on this mesh. Mechanically it is a
  Kubernetes Lease object — a small fleet-side record with a holder and an
  expiry, which the holder must renew to keep; the fleet's standard
  liveness primitive, adopted for delightd's needs and reused here. hahod
  takes the lease at admission (labeled from the spec), renews it through
  the descent, and releases it at landing. The fleet therefore sees
  every host-side flight as a first-class object: dashboards list them, and
  a lease that lapses without a landing is, by construction, the alarm for
  a hung or orphaned flight — including the case where hahod itself
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
- The sandbox covers *reach*; rlimits cover *appetite*: hahod sets
  process-level resource limits at spawn — CPU and memory rlimits, and a
  disk quota on the scratch directory — so a runaway chute exhausts its own
  allowance, not the machine.
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

0. **hello world.** Before any named consumer: `haho-hello-world`, one
   trivial spec (`text` in, `text` out, cpu lane), submitted with
   `haho new` / `haho task`, observed on `/status`, landed on the bus. The
   smallest complete flight.
1. **The flight.** Loft, work, land against a real model backend.
2. **The cache.** Run a real job, then the identical job; the second
   completion shows `cacheHits > 0` and near-zero model time.
3. **The lanes.** A metal transcription job and an ane interpretation job
   in flight simultaneously, each landing in the wall time it needs
   alone. This is the measurement that converts section 4's concurrency
   hypothesis into fact — or fails to, in which case the speedy pipeline's
   design assumptions get revisited *before* anything is built on them.
4. **The ladder.** A document that misses the window whole is resized,
   mapped with at least one cache hit, synthesized via mapesis, and lands
   PRODUCED.
5. **The bench.** `haho bench` produces the coherence-window numbers (N and
   D, section 9) for this hardware and the resident backends, and the
   shipped defaults cite them.

`haho bench` is not a one-off: it is a named, versioned suite that ships
with haho, so every number this document leans on can be re-verified at
will. The suite so far: **bench-70x** (Appendix A, the metal/torch gap),
**bench-ane-serialization** (Appendix B, why AFM's door budget is 1),
**bench-speech** (Appendix C: realtime factor, door concurrency, and the
accuracy taxonomy; its confidence-attribute variant — does Speech flag its
own errors where they actually occur — is planned and has not yet been
run), **bench-cross-lane** (proof 3 above, the concurrency hypothesis), and
**bench-window** (N and D, section 9). Each run reports under the same
hardware-signature discipline as the appendices, which is the point: send
the suite to a friend with a newer machine, re-run it when the OS updates —
localhost is, after all, a 27 beta running six months ahead — and the
document's claims re-ground themselves instead of fossilizing. Appendix C
is that argument working in practice: it contradicts a true observation
made one API generation earlier.

## 16. Non-goals

- **n>1.** Section 3. The design philosophy would change so substantially
  at n>1 that haho as specified would be moot.
- **Containers or VMs as chute shapes.** Section 5. Fleet services stay in
  containers; chutes are native processes.
- **Resident chutes or harnesses.** hahod is resident (it holds queues and
  sessions, and does no model work); a harness and its chute never are.
- **Owning models or weights.** The MLX server belongs to
  big-little-mesh's apple-silicon work; AFM belongs to the OS; ollama can
  be furnished if a job needs it. haho exposes furnished resources to jobs
  the way meubilair exposes them to the fleet; it owns none of them.
- **Internal discernment.** Version one enforces the requestor's placement;
  it does not compute placements itself (section 2).

## 17. Open questions

**A self-authenticating subprocess kind's sandbox reach.** `CLAUDE_CLI`
(section 8) enters the api lane by exec rather than by HTTP, and section 5
derives a chute's reach from its spec: the only outbound grant an api-backed
job gets is `(remote tcp (param "API_HOST"))`, taken from the spec's
`base_url`. This kind MUST carry no `base_url` — it refuses one, because the
CLI decides where it goes — so `API_HOST` is underivable and a profile
generated as written would grant the job no network at all, while the
deny-default file rules would also deny the CLI the credential store it
authenticates with. Today the kind is served in-process, so section 5's
sandbox constrains nothing and the question has not bitten; it must be
answered before the harness split lands. The candidates: derive the grant
from hahod's configuration rather than the spec, give the kind a declared
reach of its own, or decide that a self-authenticating subprocess is not
sandboxable on these terms and say so. Not pre-decided here.

The executable and argv are already settled and are NOT part of this
question: both are hahod's own configuration, never a spec's, and the child's
environment is constructed rather than inherited (section 14).


- **Cache fingerprints per backend.** The key includes a model fingerprint;
  each backend must define one. api models report identifiers; MLX can
  fingerprint weights; AFM offers no version API, so its fingerprint
  incorporates the OS build — coarse but safe. Confirm per backend during
  implementation.
- **TOKENS chunking.** Requires a tokenizer per backend (tiktoken-style for
  api kinds, mlx-lm's for metal, nothing public for AFM). Marked FUTURE in
  the contract; SENTENCES and MARKDOWN_SECTIONS carry the near-term load.
- **Scope cleanup beyond the host.** hahod's sweeper kills harnesses whose
  scope is no longer valid, and that jurisdiction ends at the host
  boundary. The mesh-side half is harder and is named as ours: a
  kafka-resource consumer — a consumer group holding partitions, offsets,
  a topic's attention — whose scope has died has no kafka-native reaper,
  and the bus will happily carry its debris indefinitely. Cleaning up
  kafka-resource-consuming things that no longer have a valid scope is a
  genuinely hard problem for kafka, we have this problem, and this line
  exists so nobody pretends otherwise. Candidate shapes (scope-stamped
  group ids swept against hm's lease truth; broker-side idle-group
  expiry) are noted, not chosen.
- **Chute framework access.** A Go chute cannot call AFM, Speech, or
  Translation directly: they are Swift/Objective-C frameworks. The
  mechanism is undecided, with the candidates named so it is a choice
  rather than a discovery: (a) cgo bridges into the frameworks; (b) a small
  Swift shim binary per framework door, spoken to over the pipe; (c) a
  resident Swift helper on the pattern the MLX server already implies.
  cardinal's python `apple-fm-sdk` bridge is the existence proof that the
  shim shape works. Decided at first implementation of the AFM door, not
  before.
- **Cancellation.** Jobs are immutable and chutes are not directable, so
  the only in-flight signal today is process termination by hahod (the
  sweeper, or operator action). Whether requestors get a graceful cancel — and
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

## 18. Definitions, prior work, future work

Definitions first, because the project names are deliberately whimsical and
the reader deserves a map. Whimsy is a tool here: it keeps long work light.
Then prior work — the projects that exist and taught this document what it
knows — and future work, clearly marked as such, because a reader should
never mistake an aspiration for an artifact.

### Definitions

- **frood** — the mesh's pipeline library
  ([janearc/big-little-mesh](https://github.com/janearc/big-little-mesh)):
  contracts, registration, heartbeats, bus adjacency.
- **froody** — the state of being a well-behaved frood. Nobody checks
  compliance at runtime except the CI gates; if it says it is a frood and
  it is on the network, it is froody, and it knows where its towel is.
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
- **speedy** — the mesh's recipe semantics: bounded, dialed composition of
  froods — packets, never streaming; near-real-time, never realtime; no
  velocity promises. haho implements speedy for chutes. Companion RFC:
  `docs/speedy-rfc.md`.

### Prior work (built; this document stands on it)

- **holden** — the assessment service (the judgment gate formerly living in
  the sprints repo). haho's first consumer; RFC: `docs/holden-rfc.md`.
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

- **paling** — inference and fine-tuning pipeline
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
  into haho.
### Future work (not built; second and third steps on this road)

- **mackerel** — the live-interpreting pipeline. Named because babelfish is
  correct but not correct enough: babelfish is realtime, and speedy
  promises only near-real-time. mackerel is an ambient listener that emits
  translated speech and a scrolling transcript when enough information has
  passed through to render them.
- **grackle** — the image-generation flavor of mackerel (the words rhyme):
  ephemeral text-to-image in the same ambient mode.
- **intruder** — the participation seam: a phone or wearable joins the mesh
  through an ephemeral cloudflared tunnel for the life of a connection,
  deposits capture, and receives product. Client-side capture and cutting;
  the mesh does the work.

One prior-work note rather than a history lesson: the judge — holden's
previous life as a CLI in the sprints repo — ran fifteen sprints as a proof
of concept and earned its keep; as context sizes grew it became a single
point of failure on large diffs. haho is the fix. Everything the judge
could not do — framing, caching, backend choice, events in flight — is this
document.

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

[^13]: "There's more than one way to do it" — Perl's motto, per Larry Wall,
    the patron saint of exactly this kind of pluralism.

[^12]: ThePrimeagen, at 9:34:
    <https://www.youtube.com/watch?v=8eXiWkPSb50&t=855s> (retrieved
    2026-07-12). Lightly trimmed from the spoken transcript; emphasis the
    operator's.

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

The next benchmark this appendix wants, once `haho bench` ships it as
bench-70x (section 15): one fixed 60-second file, transcribed three ways on
the same hardware — torch on cpu (the hours-of-the-day baseline), whisper
on metal (the 70x), and Apple Speech on the ane, where the OS treats
transcription as nearly free (the integration point of section 4) — with
the ane number carrying its framing caveat (section 9), since
`SpeechTranscriber`'s speed is bought at a coherence cost on proper nouns
and code-switching. Appendix C now carries the Speech half of that
comparison (63–77x realtime, measured), including the proper-noun receipt;
the three-way single-file run remains wanted.

## Appendix B: the ANE serializes, measured

The measurement behind the ane lane's budget of 1 (section 10), and the
verification of an operator observation from turtledove-era load testing:
the Neural Engine, given 100X work, does not degrade — it takes 100X time,
at constant service rate, and the excess waits. Measured 2026-07-11, same
hardware signature as Appendix A (`663bbc4d197add27`).

Method: three rounds against AFM (the ane lane's front door), with
generation-heavy prompts — an earlier round with trivial prompts produced a
misleading 0.56 wall-to-serial ratio because framework overhead overlaps
even when compute serializes. Distinct prompts each round so no cache can
answer.

| Round | Shape | Per-job | Wall | Reading |
|---|---|---|---|---|
| serial | 3 jobs, one at a time | 3.51 / 3.82 / 3.42s | 10.75s (sum) | consistent service time |
| gather | 3 jobs, one process, concurrent | 3.25 / 6.53 / 10.83s | 10.83s | wall/sum = 1.01 — a queue |
| processes | 3 jobs, three OS processes | 5.35 / 8.89 / 12.44s | 12s | the device itself serializes |

The staircase in the concurrent per-job times is the finding: each job's
latency is its predecessors' plus its own. Serialization happens at the
device (round three rules out an SDK or event-loop lock), service time is
constant under load, and overload manifests as waiting, never as
degradation.

The probe (functional; it produced the numbers above — python is committed
code by this document's convention):

```python
# bench-ane-serialization — serial vs concurrent, generation-heavy
# uv run --with apple-fm-sdk python bench_ane.py            (gather round)
# uv run --with apple-fm-sdk python bench_ane.py --single N (process round, xN)
import asyncio, sys, time
import apple_fm_sdk as fm

PROMPTS = [
    "Write a 200-word plain description of how tides work.",
    "Write a 200-word plain description of how yeast makes bread rise.",
    "Write a 200-word plain description of how a bicycle stays upright.",
]

async def one(p):
    s = fm.LanguageModelSession()
    t0 = time.monotonic()
    await s.respond(p)
    return time.monotonic() - t0

async def main():
    ok, reason = fm.SystemLanguageModel().is_available()
    if not ok:
        print(f"UNAVAILABLE: {reason}"); sys.exit(2)
    if len(sys.argv) > 2 and sys.argv[1] == "--single":
        t = await one(PROMPTS[int(sys.argv[2])])
        print(f"proc[{sys.argv[2]}] {t:6.2f}s"); return
    await one("hello")  # warm the framework once
    s = [await one(p) for p in PROMPTS]
    print(f"serial per-job: {['%.2f' % t for t in s]}  sum={sum(s):.2f}s")
    t0 = time.monotonic()
    each = await asyncio.gather(*(one(p) for p in PROMPTS))
    wall = time.monotonic() - t0
    print(f"gather per-job: {['%.2f' % t for t in each]}  wall={wall:.2f}s")
    print(f"wall/serial-sum = {wall/sum(s):.2f}  (1.0=serialized, 0.33=concurrent)")

asyncio.run(main())
```

Two caveats rode with this finding when it was first recorded, and one has
since resolved — instructively. This measured **AFM**; the original
observation was **Speech**, and "the same device queue is overwhelmingly
likely" turned out to be **wrong**: Appendix C ran the Speech round and
found the opposite behavior — Speech's door is concurrent. The ANE's
serialization is a property of a door, not of the lane, and the safe-sounding
inference from one door to the other did not survive measurement. The
second caveat stands: the METAL+ANE **cross-lane** hypothesis (section 4,
statement 2) is untouched by these numbers — whisper on the GPU while AFM
works the ANE is still bench-cross-lane / graduation proof 3, and it is
still where the realtime ambitions rest.

## Appendix C: the two ANE doors, measured

The companion measurement to Appendix B, run 2026-07-11 on the same
hardware signature (`663bbc4d197add27`), macOS 27.0 beta (build 26A5368g):
the same serialization probe shape pointed at the ane lane's *other* front
door, `SpeechAnalyzer`/`SpeechTranscriber`. It answers two questions —
how fast, and how concurrent — and begins a third (how honest about its
own errors).

Method. Ground truth first: three distinct ~70-word paragraphs, rendered
to audio by `say` — deterministic, shareable fixtures with a named caveat:
synthetic-clean TTS audio is easy mode for ASR, so accuracy numbers here
are optimistic relative to walking-around recordings. Short (~20s) and
heavy (~200s, ten repetitions per paragraph) rounds; one
`SpeechAnalyzer` + `SpeechTranscriber` per task so the concurrent round
has no shared-instance excuse; model assets installed and warmed before
any timing.

| Round | Shape | Per-job | Wall | Reading |
|---|---|---|---|---|
| short, serial | 3 clips (~20s each) | 0.35–0.40s | 1.12s (sum) | 49–62x realtime |
| short, concurrent | 3 tasks, one process | 0.43–0.50s | 0.50s | wall/sum 0.45 — but compute is too small to trust (Appendix B's tiny-prompt lesson) |
| heavy, serial | 3 clips (~200s each) | 3.13 / 2.85 / 2.94s | 8.92s (sum) | 63–77x realtime, consistent |
| heavy, concurrent | 3 tasks, one process | 3.47 / 3.59 / 3.77s | 3.77s | **wall/sum = 0.42 — concurrent**, ~20% per-job cost |

Against AFM's 1.01 staircase under the identical test shape, the finding
is unambiguous: **the ANE's two front doors behave differently.** AFM
queues strictly; Speech runs three heavy transcriptions nearly as fast as
one. Whatever arbitration Apple performs under these frameworks, it is
per-door, and budgets must therefore attach to doors (section 10).

This result *contradicts* a true operator observation from turtledove-era
load testing, and the reconciliation matters more than the contradiction:
the old `SFSpeechRecognizer` API was one-request-at-a-time by design;
`SpeechAnalyzer` is a ground-up rewrite; and localhost runs a beta six
months ahead of the world. Both observations are plausibly true of the
doors and builds they were made against — which is the entire argument for
bench-speech being a re-runnable suite entry rather than a fossilized
finding (section 15).

Accuracy, against ground truth (word error rate 6.1% / 8.1% / 1.6% before
normalization). Raw WER overstates real errors: `thirty→30`,
`seventeen→17`, `first→1st`, `harbor master→harbormaster` are
normalization choices, not mistakes. The real errors sort into exactly
three classes:

- **homophones** — `flour→flower`, `hulls→halls`, `ferries→fairies`: the
  class a context-bearing smoothing stage (lloyd, section 8's worked spec)
  fixes for free, which the interpreting pipeline's design anticipated;
- **proper nouns** — `Distelkade→"distal cade"`: turtledove's recorded
  "weaker on proper nouns" trade, now with a receipt;
- **unstable domain terms** — `arcsecond→arxicond / anarxicond /
  arcticond`: *different* wrong answers across ten repetitions of the same
  sentence, the most interesting cell in the table — the model is not
  consistently wrong, it is unsettled.

The third question — does Speech *flag* its errors where they occur — is
begun, not answered: the confidence-attribute variant of bench-speech
(correlating reported confidence against the error positions above) is
planned and has not been run.

The probe (functional; it produced the numbers above — Swift is committed
code by this document's convention):

```swift
// bench-speech — realtime factor, serial vs concurrent, transcripts out
// swift bench_speech.swift <fixture-dir> [clip-prefix]
import AVFoundation
import Foundation
import Speech

let scratch = CommandLine.arguments.count > 1
    ? CommandLine.arguments[1] : FileManager.default.currentDirectoryPath
let prefix = CommandLine.arguments.count > 2 ? CommandLine.arguments[2] : "gt"
func clipURL(_ i: Int) -> URL { URL(fileURLWithPath: "\(scratch)/\(prefix)_\(i).aiff") }

@available(macOS 26, *)
func transcribe(_ url: URL) async throws -> (text: String, wall: Double) {
    let transcriber = SpeechTranscriber(locale: Locale(identifier: "en_US"),
        transcriptionOptions: [], reportingOptions: [], attributeOptions: [])
    let analyzer = SpeechAnalyzer(modules: [transcriber])
    let collector = Task { () -> String in
        var out = ""
        for try await result in transcriber.results where result.isFinal {
            out += String(result.text.characters)
        }
        return out
    }
    let file = try AVAudioFile(forReading: url)
    let t0 = Date()
    if let last = try await analyzer.analyzeSequence(from: file) {
        try await analyzer.finalizeAndFinish(through: last)
    } else {
        await analyzer.cancelAndFinishNow()
    }
    return (try await collector.value, Date().timeIntervalSince(t0))
}

guard #available(macOS 26, *) else { print("UNAVAILABLE: needs macOS 26+"); exit(2) }

// assets, then a warm pass, stay out of the timings
if let req = try await AssetInventory.assetInstallationRequest(
    supporting: [SpeechTranscriber(locale: Locale(identifier: "en_US"),
        transcriptionOptions: [], reportingOptions: [], attributeOptions: [])]) {
    try await req.downloadAndInstall()
}
_ = try await transcribe(clipURL(0))

func duration(_ url: URL) throws -> Double {
    let f = try AVAudioFile(forReading: url)
    return Double(f.length) / f.processingFormat.sampleRate
}

var serialWall: [Double] = []
for i in 0 ..< 3 {
    let (text, wall) = try await transcribe(clipURL(i))
    serialWall.append(wall)
    try text.write(toFile: "\(scratch)/st_\(i).txt", atomically: true, encoding: .utf8)
    let dur = try duration(clipURL(i))
    print(String(format: "serial[%d]  %5.2fs  clip %5.2fs  rtf %.1fx", i, wall, dur, dur / wall))
}
print(String(format: "serial sum %.2fs", serialWall.reduce(0, +)))

let t0 = Date()
try await withThrowingTaskGroup(of: (Int, Double).self) { group in
    for i in 0 ..< 3 { group.addTask { (i, try await transcribe(clipURL(i)).wall) } }
    for try await (i, wall) in group { print(String(format: "gather[%d]  %5.2fs", i, wall)) }
}
let wall = Date().timeIntervalSince(t0)
print(String(format: "gather wall %.2fs   wall/serial-sum = %.2f  (1.0=serialized, 0.33=concurrent)",
             wall, wall / serialWall.reduce(0, +)))
```
