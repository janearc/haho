# speedy — RFC

> no clients, no servers,
> no gods, no masters,
> there is only state,
> there is only mesh.

- Status: DRAFT. Ratification by the operator and fable marks this file
  read-only, as is the usual convention. haho's ratification is blocked on
  this document; this document has no blockers of its own.
- Date: 2026-07-12. Full redraft; supersedes the 2026-07-11 draft, whose
  model (deadlines, drops, an invented envelope) did not survive review.
- Authors: Max (operator); Claude (Fable 5, drafting). Converged in
  conversation before drafting, per the operator's review direction; the
  ledger maps every review comment to its answer here.
- Companions: `bento/v1/composition.proto` and `bento/v1/bento.proto` in
  big-little-mesh — the protocol this document binds semantics onto, which
  reserved a slot for it from the beginning[^1]; `docs/haho-rfc.md` (haho
  implements speedy for chutes; section 12); mackerel `DESIGN.md` (a case
  study, not a driver).

The epigraph is the type system. "No gods" reads precisely: there is no
orchestrator node — composition rides the data, and nothing inside the mesh
stands above the mesh.

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

## 2. speedy is the licensed exception

speedy asks: what if a frood's output causing another frood to work is a
feature? If every frood's internal states are just lexically-scoped states
of the mesh, then the mesh is a decomposed monolith — and it can be
recomposed. The canonical definition:

> A recipe is a dynamic recomposition of an inherently decomposed lattice
> of lexically-scoped states.

That is what speedy is, and it is why speedy is scary: recomposition is the
exact thing the frood law forbids, licensed. The license comes with terms
(section 6), and every term traces to the same fear — the halting problem
wearing mesh clothes. There is no general way to prove that a set of
state-calls terminates, saturates nothing, and fills no disk. So the terms
do not try to prove it; they *bound* it.

The payoff justifying the license: a microservices mesh cannot be told
"call these three services in order" — composition is buried in opaque RPC
inside services that are not allowed to know what they are part of, and
there is no notion of provoking the whole from outside. Froods went to the
opposite extreme: only the operator causes work, and nothing composes at
all. Recipes are what neither world has: **external, explicit, inspectable,
bounded composition**. The smoothing scope never learns that a podcast
exists; the operator holds the entire program in one hand, because the
program is a document.

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
standing number is **10**. It SHOULD be pinned where recipes are minted
(the minting tool's default and ceiling), not in the proto — the proto's
job is to carry the bound, the license's job is to set it.

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
TCP is a chunking protocol, UDP is a chunking protocol, and if the net is
cast that wide, tar is a chunking protocol and nothing is not-realtime. The
useful line is drawn where the *unit of work* lives: streaming moves bytes
with no work-shape; speedy moves bentos — units of work with manifests —
through a bounded itinerary of processing states.

There are no clients and no servers anywhere in this picture. There is an
origin, a terminus, and at most MAX_HOPS states between:

```text
FSMa[state_final] -> FSMb[state_start] ... FSMb[state_final] -> FSMc[state_start]
```

Who is the client? The question has no referent. There is only state; there
is only mesh.

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

This is the deepest change in the model, so it is stated at full strength.
The bento stops being copied from output directory to input directory at
every seam; it becomes a single traveling **working directory**, allocated
once by the origin at recipe minting, handed by reference through dials,
advanced by each scope in turn. The recipe owns the endpoints; the scopes
own nothing but their turn. Nothing returns — there is nothing to return
*to* — and the result exists exactly once, at completion, when the final
scope exits and the working directory is published as end data.

The functional name for this shape is continuation-passing style, and it
illustrates in scheme exactly the way it runs on the mesh:

```scheme
;; a speedy scope never returns: it advances the working directory and
;; dials the rest of the itinerary. the tail call is the mesh.
(define (scope step)
  (lambda (workdir itinerary hop-bound)
    (when (zero? hop-bound)
      (refuse 'loudly))                    ; the ceiling (section 6)
    (advance step workdir)                 ; local states, invisible outside
    (seal workdir)                         ; step-complete-before-dial
    (if (null? itinerary)
        (publish workdir)                  ; terminus: workdir IS end data
        (dial (resolve (car itinerary))    ; capability -> citizen, by discovery
              workdir
              (cdr itinerary)
              (- hop-bound 1)))            ; the last act of a living scope
    (exit)))
```

Three mechanics, precisely:

- **The dial carries a name and a path, never payload.** The working
  directory does not transit the bus; its location does.
- **The recipe names capabilities; discovery resolves the citizen.** A
  recipe step never names a service (the composition doctrine forbids
  prescribed routes[^2]); the dial addresses the *resolved* scope by name,
  at dial time. "Dialing by name frood_b" is the resolved end of a
  capability lookup, not a route written into the recipe.
- **A scope MUST dial before it exits, because a scope that has exited
  cannot dial.** The dial is the last act of a living scope. The brief
  overlap this forces — the dialing scope still alive as the dialed scope
  wakes — is where the pipeline's concurrency comes from, and it is a
  *necessity*, not an optimization.

Why kafka and not amqp: dialing needs named, durable addressability with
many listeners, and the log doubles as the itinerary's witness — what was
dialed, when, by whom, is on the bus record without any additional
machinery.

And the latency argument, which is the speed in speedy: in the poll world,
end-to-end lag has a floor of hops × poll-interval no matter how fast the
silicon is — the polling is the slow part. Dialing deletes that floor; the
wake-up becomes a bus message, and the floor drops to hops × bus-latency.

Because speedy froods are named scopes of states on the mesh, the same
recipe could in principle run as separate daemons or as scopes within one
monolithic process. In reality they are processes; the point is that the
*recipe* is the program, not the process boundary. The mesh, because it is
decomposed, gains the ability to be recomposed in different ways — which is
the definition in section 2 doing literal work.

## 6. The license terms

Every term bounds the thing the halting problem says cannot be proven.
These are the conditions under which the frood law's exception is granted,
and they are MUSTs:

1. **Recipes are minted only outside the mesh.** The operator, or an
   operator-agent acting on his behalf. A scope executes its step, dials
   the next step *as written*, and decrements the bound. A scope MUST NOT
   add, reorder, or substitute steps — no runtime itinerary growth, ever.
   The mesh does not program itself.
2. **Recipes are linear.** Branching is ruled out by name: once a recipe
   can fan out, there is no way to bound saturation of the one node this
   mesh lives on, or its disk. Parallelism comes from many linear intents
   in flight (section 9), never from one intent becoming several. The one
   sanctioned cycle is `Convergence.max_passes`, judged by its acceptance
   check and bounded like everything else.
3. **The itinerary never exceeds the maximum.** A set — the word is
   deliberate; itineraries do not revisit — of state-calls is bounded by
   `hop_bound`, decremented at every derivation, REFUSED loudly at zero.
   Operator's standing ceiling: 10.
4. **Step-complete-before-dial.** The working directory MUST be in a
   complete, discernible state before the dial: the bento-or-invisible
   rule applied per hop. A half-written step is invisible because the dial
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
artifact — "are these smoothed?" — so a re-invoked scope looks at the
working directory and resumes from what is actually there, not from a
resume-token. Content-discernment makes retries idempotent. Term 4
guarantees the previous step's work is either sealed or invisible, so the
re-dialed scope never sees a half-truth. Host-side residue (a dead scope's
scratch) is the sweeper's job, per haho's RFC.

## 8. "Behind" is the only degradation

The mesh never drops. A frood does not tolerate a drop at any point — the
question "at what point does a frood tolerate a drop" has the same answer
as "who is the client": no referent. When the mesh is slower than its
input, the flow is *behind*, and behind is stated plainly rather than
dressed as failure.

Behind is also self-healing on this hardware, which is why drops were
never needed: producers are physically rate-bounded (a room speaks at 1x;
a camera records at 1x) while the processing states run at 50–77x
realtime, measured[^4]. A backlog is a transient that the silicon eats.
Queue growth is bounded by physics at the front (input arrives at
real-world speed), by `hop_bound` in the middle (no amplification), and by
bus retention at the back.

What a *terminus presentation* does with late-arriving pieces — skip,
elide, annotate — is edge rendering policy, outside the mesh's semantics
and outside this document. (mackerel's recorded "drop the segment rather
than block the stream" is re-ruled as exactly that: a rendering choice at
the terminus, invisible to the mesh, which delivered everything.)

## 9. Faster than realtime

speedy can exceed realtime, which streaming by definition cannot, and the
mechanism is chunk multiplexing. The operator's worked example: take a
connection of known bandwidth — 500 kbyte/s, a notorious refugee camp in
the Netherlands — which appears to be just enough to stream 1080p. A
stream is stuck at 1:1 with the clock: one second of video per second of
wire. speedy is not: cut the flow into banchans, run them through the
itinerary as independent intents multiplexed across the mesh's lanes, and
the aggregate can move faster than the clock — the same wire carries the
1080p *and* a second data channel, because chunks admit parallelism and
sockets do not.

Parallelism in speedy is always this shape: **many linear intents in
flight**, each obeying every license term, never one intent becoming
several. The overlap in the worked example below is the pipelining of
successive bentos through the same states, plus the dial-before-exit
overlap at every seam — concurrency as an emergent property of the
composition law, with no coordinator anywhere.

## 10. The worked example

The chain: unpack a tarball of voice memos, transcribe, smooth, stitch a
podcast script and render its audio. First, the classic frood world — four
froods, each with `{input, output}`, each invoked by the operator, polling:

```text
FSMa> invoked (via poll)
FSMa> check for files
FSMa> is it gzipped; gzip -dc; is it a tarball
FSMa> write tar output to output directory
FSMa> format the output directory as bento output    FSMb> invoked (via poll)
FSMa> exit                                           FSMb> check for files
                                                     FSMb> do these look like m4a files
                                                     FSMb> transcribe m4a -> text
FSMc> invoked (via poll)                             FSMb> complete work in output directory
FSMc> check for files                                FSMb> exit
FSMc> looks like text; needs smoothing
FSMc> smooth; write to output directory              FSMd> invoked (via poll)
FSMc> exit                                           FSMd> ingest text; generate script
                                                     FSMd> generate prompt.txt
FSMc> invoked (via poll)                             FSMd> wrap up output directory
FSMc> are these smoothed; they're smoothed           FSMd> exit
FSMc> stitch podcast script; render audio
FSMc> wrap up output: script, prompt, audio
FSMc> exit
```

(Diagram shape from the design conversation: time flows down, columns run
concurrently, and FSMc demonstrates content-discernment — the same frood,
re-invoked, branches on what it finds.)

The operator stands at every seam of that picture: each frood's output
waits, inert, until someone external says go. Now the same chain as
`recipe_X`, minted once, dialed thereafter:

```text
operator> mint recipe_X {start: memos.tgz, end: podcast/}; allocate W; hop_bound 10
speedy_a> dialed: W awaits
speedy_a> unpack tarball into W; seal W
speedy_a> dial speedy_b ------------->  speedy_b> dialed: W awaits
speedy_a> exit                          speedy_b> transcribe m4a -> text in W; seal W
operator> (second intent: W2)           speedy_b> dial speedy_c ------------->  speedy_c> dialed: W awaits
speedy_a> dialed: W2 awaits             speedy_b> exit                          speedy_c> smooth text in W; seal W
speedy_a> unpack into W2; seal W2                                               speedy_c> dial speedy_d --> ...
speedy_a> dial speedy_b ------------->  speedy_b> dialed: W2 awaits             speedy_c> exit
speedy_a> exit                          ...
                                                        speedy_d> stitch script; render audio; seal W
                                                        speedy_d> terminus: publish W as end data
                                                        speedy_d> exit
recipe_X is complete : {W} -> {end data: podcast/}
```

One mint instead of four invocations per bento; the working directory W
travels by reference; every dial is the last act of a living scope; the
second intent (W2) pipelines behind the first through the same states.
The operator's involvement collapsed to the two things only the operator
may do: mint the recipe and receive the end data.

## 11. Scope

The correlated flow — one working directory's journey from start data to
end data — is a **scope**. This is deliberately the same word hahod
stores: hahod's scope is the host-local view of the same lineage, and the
fallow-reaping invariant (no heartbeats without tasking, no lease without
heartbeats) applies to speedy scopes exactly as it applies to chutes. One
word, one concept, two altitudes.

## 12. haho

A speedy frood does not require haho. speedy is a property of the mesh —
any froody citizen can be a named scope, receive a dial, advance a working
directory, and dial on. haho *implements* speedy for chutes: when a
recipe step's work is model-shaped, the scope that executes it is a haho
chute, and haho's admission, lanes, and framing apply to that step's
interior. haho consumes this document's semantics; it does not define
them, and nothing here depends on it.

## 13. What speedy is not

- **Not streaming.** A system of packets with a bounded itinerary, not a
  persistent socket (section 4).
- **Not client/server.** Origin, terminus, and at most MAX_HOPS states
  between.
- **Not an orchestrator.** There is no coordinator node; the coordination
  IS the itinerary, riding the data. (mackerel's held question — whether a
  converging coordinator is a misuse of the mesh — is answered: there is
  no coordinator to misuse; convergence is `max_passes`, authored at
  minting.)
- **Not branching.** Ruled out by name, per the license terms.
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
  license leans on gen-layer bans and hm's backstop; a stronger story can
  wait for evidence it is needed.
- **Recipe-minting ergonomics.** Where the operator's minting tool lives
  (a CLI, a holden verb, a library) and how `recipe_id` resolves — the
  proto deliberately defines no registry[^2].
- **The branching trigger.** No workload on the roadmap needs a branching
  recipe; if one appears, it reopens the ruling with its own safety
  argument. Until then, linear.
- **Where the ceiling is pinned.** hop_bound's value (10) needs a home in
  the minting tool's config; it is deliberately not a proto constant.
- **Intent transport.** Bus vs dataprovider remains an OPEN ruling on the
  composition surface[^2]; nothing in this document presumes one, and the
  dial-over-kafka description above binds only the dialing signal, not
  intent submission.

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
