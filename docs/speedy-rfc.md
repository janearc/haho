# speedy — RFC

> no clients, no servers,
> no gods, no masters,
> there is only bus,
> there is only state,
> there is only mesh.

- Status: DRAFT. Ratification by the operator and fable marks this file
  read-only, as is the usual convention. haho's ratification is blocked on
  this document; this document has no blockers of its own.
- Date: 2026-07-12.
- Authors: Max (operator); Claude (Fable 5, drafting). Review happens on
  the PR.
- Companions: `bento/v1/composition.proto` and `bento/v1/bento.proto` in
  big-little-mesh — the protocol this document binds semantics onto, which
  reserved a slot for it from the beginning[^1]; `docs/haho-rfc.md` (haho
  implements speedy for chutes; section 12); mackerel `DESIGN.md` (a case
  study, not a driver).

"No gods" reads precisely: there is no orchestrator node — composition
rides the data. "Only bus" reads the same way: one shared medium, no side
channels.

## 1. The frood law

Work enters the mesh only from outside it. The operator says: hey frood, go
do some work, look at /path/to/your/stuff — and the frood, which was hanging
out waiting to be asked, does one pass of strictly local work and exits. A
frood's output MUST NOT cause another frood to do work. That is the law, and
the bento is its enforcement: the atomicity barrier between one machine's
work and the next. A bento is like a pipe with richer context — a pipe is
one-dimensional, a bento can be a terabyte with a manifest — but the
property that matters is that a bento is *inert*. It sits in a directory. It
causes nothing.

The law exists because the mesh is a mesh of state machines — a machine
made of machines — and when the machines start invoking the machines, very
unexpected things happen. Every FSM keeps its states internal; nothing
exceeds its local scope; and therefore you can put mathematical boundaries
on what happens when you write data into a directory. Give up the law and
you give up the boundaries.

## 2. speedy bends the law

speedy asks: what if a frood's output causing another frood to work is a
feature? If every frood's internal states are lexically-scoped states of
the mesh, then the mesh is a decomposed monolith, and it can be recomposed:

> A recipe is a dynamic recomposition of an inherently decomposed lattice
> of lexically-scoped states.

This is not a large addition to the mesh. speedy breaks the regular-frood
assumption — output that causes work — but only within bounds the frood
contract has carried since its first cut: the hop bound, the convergence
cap, the banchan (section 3). This document explains that bending and
restates the contract's bounds as obligations (section 6). Bounds are the
mechanism, rather than proofs, because of the halting problem: there is no
general way to show that a set of state-calls terminates, saturates
nothing, and fills no disk. The contract does not try; it bounds.

What the bending buys: a microservices mesh cannot be told "call these
three services in order" — composition is buried in opaque RPC inside
services that are not allowed to know what they are part of. Froods went
to the opposite extreme: only the operator causes work, and nothing
composes. Recipes are external, explicit, inspectable, bounded
composition. The smoothing scope never learns that a podcast exists; the
operator holds the program in one hand, because the program is a document.

## 3. What the protocol already carries

speedy was always a possibility: the itinerary and the hop bound were
declared at the protocol's first cut, and the contract header reserves the
speedy binding by name as a follow-up[^1]. This RFC is that follow-up. It
adds **no fields**. The build surface is semantics bound onto messages that
exist, verbatim, in `bento/v1/composition.proto`:

- **`Recipe`** — the itinerary: ordered `RecipeStep`s, each naming a
  *capability*, never a service. "The bento carries the recipe; the caller
  never orchestrates. A pipeline reads the recipe from the bento it holds
  and works out the next step itself — no caller stands outside the mesh
  driving hops."[^2]
- **`Intent`** — the only thing a caller submits: recipe, input bento,
  identity, hop bound. `intent_id` is the idempotency key; the same intent
  submitted twice is one fact.
- **`Intent.hop_bound`** — prevention: every derived intent carries its
  parent's bound minus one, and an intent arriving at zero is REFUSED,
  loudly — never silently dropped. `parent_intent_id` — attribution: when a
  bound trips, walking the chain names the recipe and submitter that
  spiraled. Prevention without attribution leaves nothing to fix.[^2]
- **`Convergence`** — the one sanctioned cycle: an `acceptance` check and
  `max_passes`. A bento that reaches the bound without acceptance goes
  PARTIAL or FAILED, loudly; it is never looped past the bound.
- **`banchan`** — the irreducible element of a bento's batch[^3]. When
  speedy moves a flow in pieces, the pieces are banchans; the protocol
  header already says so: "a speedy session will later bind to a bento and
  its segments to banchans."[^1]

One value is not pinned anywhere: the maximum itinerary. The operator's
standing number is **10**. It is pinned **in the gen**: a generated
constant, produced with the contract code and inherited by the minting
tool and every citizen — so raising the ceiling is a regeneration, visible
in a diff and held by the gen-freshness gate, never a config edit or a
proto field. The proto's job is to carry the bound; the gen's job is to
set it.

## 4. Not streaming: the packet model

speedy is called speedy and not realtime because it is not streaming.
speedy is a system of packets:

```text
marshal (origin)
wire transit
processing            <- repeatable, up to max_itinerary - 1 times
marshal
wire transit
unmarshal (terminus)
```

Streaming, by comparison, is a persistent socket. The distinction is real
at the protocol level even though every transport is chunking underneath —
TCP is a chunking protocol, UDP is a chunking protocol, and the moment the
net is cast that wide (what is tar but a chunking protocol) everything is
realtime and nothing is not-realtime; the word stops drawing a line. The
useful line is drawn where the *unit of work* lives: streaming moves bytes
with no work-shape; speedy moves bentos — units of work with manifests —
through a bounded itinerary of processing states.

There are no clients and no servers anywhere in this picture. There is an
origin, a terminus, and at most MAX_HOPS states between:

```text
FSMa[state_final] -> FSMb[state_start] ... FSMb[state_final] -> FSMc[state_start]
```

Who is the client? The question has no referent.

## 5. Dialing, and the has_a shift

A classic frood waits for the operator. It polls its inbox; its shape is:

```text
frood_a    has_a : {input, output}
```

A speedy frood is a **named scope** that can be *dialed*: one scope emits
onto the mesh "dialing by name — work awaits at /path/to/work," and the
next scope is listening for exactly that. Dialing allows one service to
have just a tiny bit of information about another service: where its
inputs are. Nothing else. The shape changes to:

```text
recipe_X   has_a : {start data, end data}
speedy_a   has_a : {working directory}
speedy_b   has_a : {working directory}
speedy_c   has_a : {working directory}
...
speedy_z exits
recipe_X is complete : {working directory} -> {end data}
```

The bento stops being copied from output directory to input directory at
every seam; it becomes a single traveling **working directory**, allocated
once by the origin at recipe minting, handed by reference through dials,
advanced by each scope in turn. The recipe owns the endpoints; the scopes
own nothing but their turn. Nothing returns — there is nothing to return
*to* — and the result exists exactly once, at completion, when the final
scope exits and the working directory is published as end data.

The functional name for this shape is continuation-passing style, with one
sharpening that keeps the fracture honest: the working directory is the
**heap**. The executable part of each state machine is analogous to a
continuation (or a closure), but the scopes in a recipe do share state —
the data they operate on — and shared state between scopes is precisely
what classic frood does not have. That shared heap is the primary fracture
against frood, and the terms in section 6 are its containment.

A scope does not end by dialing; it ends by calling **`release`**, a
primitive generated with the contract code — the same place the ceiling
lives (section 3). `release` consults the itinerary, resolves the next
capability by discovery, dials the resolved scope with the heap's path and
the decremented bound — or publishes end data at the terminus — and exits.
The mechanical truth underneath is that the dial fires while the scope is
still alive, because a scope that has exited cannot dial; the brief overlap
this forces at every seam is where the pipeline's concurrency comes from.
But no scope author writes a dial: from the outside, one state ends and the
next begins, and the handoff is subsumed into `release`.[^5]

```scheme
;; a speedy scope never returns and never dials by name: it advances the
;; heap and releases. release is generated with the contract; it is the
;; only way a scope ends.
(define (scope step)
  (lambda (heap itinerary hop-bound)
    (when (zero? hop-bound)
      (refuse 'loudly))                ; the ceiling, enforced in the gen
    (advance step heap)                ; local states, invisible outside
    (seal heap)                        ; step-complete-before-release
    (release heap itinerary)))         ; dial the next resolved step -- or
                                       ; publish at terminus -- then exit
```

Three mechanics, precisely:

- **The dial carries a name and a path, never payload.** The heap does not
  transit the bus; its location does.
- **The recipe names capabilities; discovery resolves the citizen.** A
  recipe step never names a service (the composition doctrine forbids
  prescribed routes[^2]); `release` addresses the *resolved* scope by name,
  at dial time. "Dialing by name frood_b" is the resolved end of a
  capability lookup, not a route written into the recipe.
- **`release` is the only exit.** A scope cannot dial an arbitrary name —
  no such call exists in the generated scaffolding — and it cannot exit
  without releasing. What the gen provides is the only vocabulary there
  is, which is the same enforcement posture as the ceiling.

Why kafka and not amqp: dialing needs named, durable addressability with
many listeners, and the log doubles as the itinerary's witness — what was
dialed, when, by whom, is on the bus record without any additional
machinery. (The composition surface's larger intent-transport ruling — bus
versus dataprovider — stays open[^2] and is not implicated here: dialing
requires the bus by construction, and the dialing signal is the only
transport this document binds.)

The latency case for dialing: in the poll world, end-to-end lag has a
floor of hops × poll-interval no matter how fast the silicon is — the
polling is the slow part. Dialing deletes that floor; the wake-up becomes
a bus message, and the floor drops to hops × bus-latency.

Because speedy froods are named scopes of states on the mesh, the same
recipe could in principle run as separate daemons or as scopes within one
monolithic process. In reality they are processes; the point is that the
*recipe* is the program, not the process boundary. The mesh, because it is
decomposed, can be recomposed in different ways.

## 6. The terms

The terms begin at minting, and minting is in scope of this document:
there is no speedy without the recipe defined. Minting is the operator's
act — define the ordered capabilities, allocate the working directory,
stamp the bound from the generated ceiling (section 3), submit the intent.
Where the minting tool lives is an open question (section 14); that
minting exists, and that only the operator does it, is not.

Every term bounds the thing the halting problem says cannot be proven.
These are the conditions under which the frood law bends, and they are
MUSTs:

1. **Recipes are minted only outside the mesh.** The operator, or an
   operator-agent acting on his behalf. A scope executes its step and
   releases; `release` follows the itinerary *as written* and decrements
   the bound. A scope MUST NOT add, reorder, or substitute steps — no
   runtime itinerary growth, ever. The mesh does not program itself, and
   the generated scaffolding gives a scope no verb that could.
2. **Recipes are linear.** Branching is ruled out by name: once a recipe
   can fan out, there is no way to bound saturation of the one node this
   mesh lives on, or its disk. Parallelism comes from many linear intents
   in flight (section 9), never from one intent becoming several. Fan-out
   MAY be performed *internally* to a speedy frood — local states are
   invisible and bounded by their scope — but it is inappropriate for a
   speedy frood to dial itself to accomplish fan-out. The one sanctioned
   cycle is `Convergence.max_passes`, judged by its acceptance check and
   bounded like everything else.
3. **The itinerary never exceeds the maximum.** A set — the word is
   deliberate; itineraries do not revisit — of state-calls is bounded by
   `hop_bound`, decremented at every derivation, REFUSED loudly at zero.
   Operator's standing ceiling: 10.
4. **Step-complete-before-release.** The heap MUST be in a complete,
   discernible state before `release` fires: the bento-or-invisible rule
   applied per hop. A half-written step is invisible because the release
   that would reveal it has not happened.
5. **The itinerary is the trace.** What has been done and where we have
   been ride with the work (`parent_intent_id` chains, the bus log
   witnesses the dials). When something spirals, the chain names whose
   recipe to repair.
6. **Froodiness is transitive, and it is checked at the gate, not at
   runtime.** If something is admitted to the mesh, it passed the CI
   gates and runs generated code; if it says it is speedy, it is speedy,
   it is froody, it is meshy. Bad behavior is banned at the protocol/gen
   layer, where it is a compile failure instead of an incident. hm is the
   backstop: misbehave at runtime and hm takes your lease.

## 7. Failure and resume

Nothing new is invented for recovery; the existing laws compose.

A scope that dies mid-step is marked by its lapsed hm lease. Re-dialing
the failed step is safe because scopes discern applicability from the
artifact — "are these smoothed?" — so a re-invoked scope looks at the heap
and resumes from what is actually there, not from a resume-token.
Content-discernment makes retries idempotent. Term 4 guarantees the
previous step's work is either sealed or invisible, so the re-dialed scope
never sees a half-truth. Host-side residue (a dead scope's scratch) is the
sweeper's job, per haho's RFC.

## 8. "Behind" is the only degradation

The mesh never drops. A frood does not tolerate a drop at any point. When
the mesh is slower than its input, the flow is *behind*, and behind is
stated plainly rather than dressed as failure.

Behind is bounded by the model, not by an assumed microphone. The mesh is
not fixed to cameras and capture: the operator puts anything into the mesh
he chooses, and the recipe is minted with a **model** of how that flow
operates — the model describes the bounds within which things are created,
whether that is ambient picture generation, near-real-time language
interpretation, or a tarball of memos. Queue growth is then bounded three
ways: at the front by the minted model's declared input shape, in the
middle by `hop_bound` (no amplification), at the back by bus retention.
Where the input happens to be physical capture, the front bound comes
free — a room speaks at 1x while the processing states run at 50–77x
realtime, measured[^4], so a backlog is a transient the silicon eats —
but capture is an instance of the model, not the premise.

What a *terminus presentation* does with late-arriving pieces — skip,
elide, annotate — is edge rendering policy, outside the mesh's semantics
and outside this document. (mackerel's recorded "drop the segment rather
than block the stream" is re-ruled as exactly that: a rendering choice at
the terminus, invisible to the mesh, which delivered everything.)

## 9. Faster than realtime

Chunking can be faster than 1:1; a persistent socket cannot. The
operator's worked example: take a connection whose bandwidth is known — a
notorious refugee camp in the Netherlands, 500 kbyte/s — which seems to be
enough to stream 1080p. Streaming can never do better than that 1:1. With
enough computation, the same wire under speedy multiplexes the 1080p as
banchans *along with a second data channel*, exceeding what 1:1 realtime
streaming carries on the same bandwidth — the chunks use whatever capacity
exists whenever it exists. The computation is there: the processing states
run at 49–77x realtime, measured[^4].

Parallelism is many linear intents in flight; one intent never becomes
several (term 2). The overlap in the worked example below is successive
bentos pipelining through the same states.

## 10. The worked example

The chain: unpack a tarball of voice memos, transcribe, smooth, stitch a
podcast script and render its audio. First, the classic frood world — four
froods, each with `{input, output}`, each invoked by the operator,
polling. The lanes are independent and never intersect: classic frood has
one arrow, and it is time. Data moves between lanes only when the operator
moves it — rsync, or asking the next frood to cut a fresh bento — and that
movement is off-mesh, so the picture cannot honestly draw it as an arrow.
FSMc appears twice: the same frood, re-invoked, branching on what it
finds:

![The classic frood world: four independent polling lanes; the only arrow
is time; FSMc invoked twice](diagrams/speedy-poll.svg)

Now the same chain as `recipe_X`, minted once, released thereafter — solid
blocks are the first intent's heap W, dashed blocks are a second intent
(W2) pipelining behind it through the same states; each block ends in a
release, whose dial fires before the block ends:

![The dialed world: recipe_X minted once, W traveling by reference, hop
counts decrementing, W2 pipelining behind](diagrams/speedy-dialed.svg)

One mint instead of four invocations per bento; the heap travels by
reference; W2 pipelines behind W through the same states. The operator
mints the recipe and receives the end data; everything between is
release.

## 11. Scope

A scope is everything done under one intent: one heap's journey from start
data to end data, mint to publish. hahod keeps a row it also calls a scope
for every piece of work it supervises on the host; that row is the
host-local record of the same thing. The word is shared on purpose.

The reaping rule reads the same at both: no heartbeats without tasking, no
lease without heartbeats. A scope that is not working is not alive, and hm
reaps what is not alive.

## 12. haho

A speedy frood does not require haho. speedy is a property of the mesh —
any froody citizen can be a named scope, receive a dial, advance the heap,
and release. haho *implements* speedy for chutes: when a
recipe step's work is model-shaped, the scope that executes it is a haho
chute, and haho's admission, lanes, and framing apply to that step's
interior. haho consumes this document's semantics; it does not define
them, and nothing here depends on it.

## 13. What speedy is not

- **Not streaming.** A system of packets with a bounded itinerary, not a
  persistent socket (section 4).
- **Not client/server.** Origin, terminus, and at most MAX_HOPS states
  between.
- **Not orchestrated — self-orchestrating.** speedy is by definition
  self-orchestrating: the coordination is the itinerary, riding the data,
  executed one dial at a time. There is no orchestrator node to build,
  deploy, or misuse. (mackerel's held question — whether a converging
  coordinator is a misuse of the mesh — dissolves: there is no
  coordinator; convergence is `max_passes`, authored at minting.)
- **Not branching.** Ruled out by name, per the terms (section 6).
- **Not a delivery-guarantee change.** The bus stays what it is; intents
  stay idempotent by `intent_id`.
- **Not intruder.** intruder is a separate project — client-side, a
  resident in-browser wrapper over the terminus of a mesh flow, with a
  batcher/chunker underneath depositing bento-shaped work. It may serve as
  a case study for speedy someday; it is out of scope here.

## 14. Open questions

- **Dial authorization.** Transitive froodiness admits a citizen to the
  mesh; whether *any* froody citizen may dial *any* named scope, or
  whether dialing rides an allowlist minted with the recipe, is open. The
  terms lean on gen-layer bans and hm's backstop; a stronger story can
  wait for evidence it is needed.
- **The minting tool.** Minting itself is in scope and required (section
  6); what remains open is where the tool lives — a CLI, a holden verb, a
  library — and how `recipe_id` resolves, since the proto deliberately
  defines no registry[^2].
- **The branching trigger.** No workload on the roadmap needs a branching
  recipe; if one appears, it reopens the ruling with its own safety
  argument. Until then, linear.

## References

[^1]: `big-little-mesh/proto/bento/v1/composition.proto`, header: the
    Intent lifecycle enum and "the speedy binding (the mesh's
    near-realtime capability — close to realtime, not realtime, and
    deliberately not called streaming; a speedy session will later bind
    to a bento and its segments to banchans)" are deferred to follow-ups
    "so the contract never carries a half-formed concept." This RFC is
    the reserved follow-up.

[^2]: `big-little-mesh/proto/bento/v1/composition.proto` (Recipe,
    RecipeStep, Intent, Convergence) and
    `big-little-mesh/docs/composition-surface.md` (ADR-0002): capability
    resolution by discovery, prescribed routes forbidden, hop_bound
    prevention + parent_intent_id attribution, intent transport open.

[^3]: `big-little-mesh/proto/bento/v1/bento.proto`: "a banchan is an
    irreducible element of that batch"; a bento is complete when every
    declared banchan is present and its assets pass.

[^4]: haho `docs/haho-rfc.md`, Appendices A–C: whisper on MLX at 63–77x
    realtime; SpeechTranscriber at 49–77x, concurrent; measured on the
    hardware whose signature the appendices carry.

[^5]: The shape is old: perl's POE gives each state a kernel, a shared
    heap, and states that end by asking the kernel to call the next state
    in line — never a name the state knows itself. `release` is that
    idiom, generated with the contract.
