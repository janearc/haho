# speedy — RFC

> no clients, no servers,
> no gods, no masters,
> there is only bus,
> there is only state,
> there is only mesh.

- Status: DRAFT. Ratification by the operator and fable marks this file
  read-only, as is the usual convention. speedy ratifies as one tranche
  with the haho RFC (`docs/haho-rfc.md`) and the holden RFC
  (`docs/holden-rfc.md`): the three documents share one vocabulary and one
  worldview, and none of them ratifies alone.
- Date: 2026-07-12.
- Authors: Max (operator); Claude (Fable 5, drafting). Review happens on
  the PR.
- Companions: `bento/v1/composition.proto` and `bento/v1/bento.proto` in
  big-little-mesh — the protocol this document binds semantics onto, which
  reserved a slot for it from the beginning[^1]; `docs/haho-rfc.md` (haho
  implements speedy for chutes; section 14 below); `docs/holden-rfc.md` (holden
  is not speedy work, and its RFC says why); mackerel `DESIGN.md` (a case
  study, not a driver).

"No gods" reads precisely: there is no orchestrator node — composition
rides the data. "Only bus" reads the same way: one shared medium, no side
channels.

## 1. speedy bends the frood law

The frood law is the mesh's ground rule: work enters the mesh only from
outside it. A frood does one pass of strictly local work and exits, and
its output MUST NOT cause another frood to do work; the bento — inert,
sitting in a directory, causing nothing — is the enforcement, the
atomicity barrier between one machine's work and the next. The law exists
because the mesh is a machine made of machines, and when the machines
start invoking the machines, very unexpected things happen: keep every
frood's states local and you can put mathematical boundaries on what
happens when you write data into a directory.

speedy asks: what if a frood's output causing another frood to work is a
feature? The mesh is an inherently decomposed lattice of machines, each
keeping its states local — which means it is a decomposed monolith, and a
decomposed thing can be recomposed:

> A recipe is a dynamic recomposition of an inherently decomposed lattice
> of locally-scoped state machines.

This is not a large addition to the mesh. speedy breaks the regular-frood
assumption — output that causes work — but only within bounds the frood
contract has carried since its first cut: the hop bound, the convergence
cap, the banchan (section 2). This document explains that bending and
restates the contract's bounds as obligations (section 6). Bounds are the
mechanism, rather than proofs, because of the halting problem: there is no
general way to show that a set of state-calls terminates, saturates
nothing, and fills no disk. The contract does not try; it bounds.

What the bending buys: a microservices mesh cannot be told "call these
three services in order" — composition is buried in opaque RPC inside
services that are not allowed to know what they are part of. Froods went
to the opposite extreme: only the operator causes work, and nothing
composes. Recipes are external, explicit, inspectable, bounded
composition. The smoothing frood never learns that a podcast exists; the
operator holds the program in one hand, because the program is a document
(section 5 shows it — a file you can physically hold).

## 2. What the protocol already carries

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
  loudly — never silently discarded. `parent_intent_id` — attribution:
  when a bound trips, walking the chain names the recipe and submitter
  that spiraled. Prevention without attribution leaves nothing to fix.[^2]
- **`Convergence`** — the one sanctioned cycle: an `acceptance` check and
  `max_passes`. A bento that reaches the bound without acceptance goes
  PARTIAL or FAILED, loudly; it is never looped past the bound. Section 5
  states how passes and hops share one purse.
- **`banchan`** — the irreducible element of a bento's batch[^3]. When
  speedy moves a flow in pieces, the pieces are banchans; the protocol
  header already says so: "a speedy session will later bind to a bento and
  its segments to banchans."[^1]

One value is not pinned anywhere: the maximum itinerary. The operator's
standing number is **10**. It is pinned **in the gen**: a generated
constant, produced with the contract code and inherited by the minting
tool and every frood — so raising the ceiling is a regeneration, visible
in a diff and held by the gen-freshness gate, never a config edit or a
proto field. The proto's job is to carry the bound; the gen's job is to
set it.

## 3. Not streaming: the packet model

speedy is called speedy and not realtime because it is not streaming.
speedy is a system of packets:

```text
marshal (origin)
wire transit
processing            <- repeatable, within the hop bound
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
origin, a terminus, and a bounded number of states between:

```text
FSMa[state_final] -> FSMb[state_start] ... FSMb[state_final] -> FSMc[state_start]
```

Who is the client? The question has no referent.

## 4. Dialing, and the has_a shift

A classic frood waits for the operator. It polls its inbox; its shape is:

```text
frood_a    has_a : {input, output}
```

A speedy frood is addressable: it can be **dialed**. One frood, ending its
turn, emits onto the mesh "dialing by name — work awaits at
/path/to/work," and the next frood is listening for exactly that. Dialing
allows one service to have just a tiny bit of information about another
service: where its inputs are. Nothing else. The shape changes to:

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
advanced by each frood in turn. The recipe owns the endpoints; each frood
owns nothing but its turn — one **step** of the itinerary. Nothing returns
— there is nothing to return *to* — and the result exists exactly once, at
completion, when the final step ends and the working directory is
published as end data (section 11).

The functional name for this shape is continuation-passing style, with one
sharpening that keeps the fracture honest: the working directory is the
**heap**. Each step is analogous to a continuation (or a closure), but the
steps of a recipe do share state — the data they operate on — and shared
state between machines is precisely what classic frood does not have. That
shared heap is the primary fracture against frood, and the terms in
section 6 are its containment.

A step does not end by dialing; it ends by calling **`release`**, a
primitive generated with the contract code — the same place the ceiling
lives (section 2). `release` consults the itinerary, resolves the next
capability by discovery, dials the resolved frood with the heap's path and
the decremented bound — or publishes end data at the terminus — and exits.
`release` is the only way a turn ends, and the terminal `release` is the
only way a scope ends (section 12). The mechanical truth underneath is
that the dial fires while the releasing process is still alive, because a
process that has exited cannot dial; the brief overlap this forces at
every seam is where the pipeline's concurrency comes from. But no frood
author writes a dial: from the outside, one state ends and the next
begins, and the handoff is subsumed into `release`.[^5]

```scheme
;; a speedy step never returns and never dials by name: it advances the
;; heap and releases. release is generated with the contract; it is the
;; only way a turn ends.
(define (step capability)
  (lambda (heap itinerary hop-bound)
    (when (zero? hop-bound)
      (refuse 'loudly))                ; the ceiling, enforced in the gen
    (advance capability heap)          ; local states, invisible outside
    (seal heap)                        ; step-complete-before-release
    (release heap itinerary)))         ; dial the next resolved step -- or
                                       ; publish at terminus -- then exit
```

Three mechanics, precisely:

- **The dial carries a name and a path, never payload.** The heap does not
  transit the bus; its location does.
- **The recipe names capabilities; discovery resolves the frood.** A
  recipe step never names a service (the composition doctrine forbids
  prescribed routes[^2]); `release` addresses the *resolved* frood by
  name, at dial time. "Dialing by name frood_b" is the resolved end of a
  capability lookup, not a route written into the recipe.
- **`release` is the only exit.** A step cannot dial an arbitrary name —
  no such call exists in the generated scaffolding — and it cannot exit
  without releasing. What the gen provides is the only vocabulary there
  is, which is the same enforcement posture as the ceiling.

Why kafka and not amqp: dialing needs named, durable addressability with
many listeners, and the log doubles as the itinerary's witness — what was
dialed, when, by whom, is on the bus record without any additional
machinery (section 13). (The composition surface's larger intent-transport
ruling — bus versus dataprovider — stays open[^2] and is not implicated
here: dialing requires the bus by construction, and the dialing signal is
the only transport this document binds.)

The latency case for dialing: in the poll world, end-to-end lag has a
floor of hops × poll-interval no matter how fast the silicon is — the
polling is the slow part. Dialing deletes that floor; the wake-up becomes
a bus message, and the floor drops to hops × bus-latency.

## 5. The recipe, specifically

Everything so far says what a recipe does. This section says what one
*is*, because the previous revision never put a recipe on the page and a
program that is never shown is a program nobody can check.

A recipe exists in three forms, and only one of them is written by hand:

1. **Authored.** A yml file the operator writes. This is the same
   convention the mesh already uses for declared things (`meubilair.yaml`
   furnishes the fleet; haho jobs are tasked from `job.yml`), and it is
   the form section 1 means by "the program is a document."
2. **Marshaled.** The minting tool validates the authored file and
   marshals it into `bento.v1.Recipe` — protobuf, the contract, the form
   in which the recipe is checkable against the gen. On any wire and at
   rest it is protojson, per mesh convention.
3. **Sealed.** At minting the marshaled recipe is written into the heap
   itself, at a fixed path. The recipe travels with the work — the
   bento-carries-the-recipe doctrine[^2], satisfied literally.

The recipe does not transit the bus. The bus could only check its shape,
and shape is not the property that matters; what matters is that no step
can rewrite the program mid-flight. That enforcement is content
addressing: **`recipe_id` is the hash of the marshaled recipe.** `release`
re-hashes the sealed artifact at every hop and compares it against the
intent's `recipe_id`; a mismatch is REFUSED, loudly, and the chain names
the step that held the heap when the program changed. The heap is shared
state and a step can physically scribble on it — that is the fracture,
stated honestly — so tampering is *detected at the next seam* rather than
prevented, and the gen gives a step no verb that could act on a doctored
itinerary anyway. The wire enforces the dial; the gen enforces the verbs;
the hash enforces the program. The bus carries signals, not programs.

The authored form, worked — this is `recipe_X` from the example the rest
of this document keeps drawing (section 10), the memos-to-podcast chain.
Contract subjects are illustrative; capabilities and the convergence shape
are exact:

```yaml
# recipe_x.yml -- authored by the operator; the minting tool validates
# this, marshals it to bento.v1.Recipe, seals it into the heap, and
# stamps the intent's hop_bound from the generated ceiling
recipe: podcast-from-memos
steps:
  - capability: unpack          # tarball of voice memos -> audio files
    input:  bento.archive.v1.MemoTarball
    output: bento.audio.v1.MemoSet
  - capability: transcribe      # audio -> text; resolves to a haho chute
    input:  bento.audio.v1.MemoSet
    output: bento.text.v1.Transcripts
  - capability: smooth          # lloyd
    input:  bento.text.v1.Transcripts
    output: bento.text.v1.Smoothed
  - capability: stitch          # podcast script + rendered audio
    input:  bento.text.v1.Smoothed
    output: bento.podcast.v1.Episode
convergence:
  acceptance:                   # a BanchanAsset: a declared, runnable
    kind: ACCEPTANCE_TEST       # check, never a shell script smuggled in
    location: checks/episode-reads-clean
  max_passes: 2                 # the stitch may be re-entered twice
```

Reading it against the contract, field by field:

- **`steps`** — the itinerary, in order. Each step's `capability` is a
  kind of work, never a service name; its `input` and `output` are the
  contract subjects the step consumes and produces — the same subjects a
  frood claims in its `ContractDescriptor`, which is what makes
  resolution *checkable*: discovery may only resolve a capability to a
  frood that claims it consumes the step's input and emits its output.
- **`convergence`** — the acceptance check is a `BanchanAsset`, the same
  declared-runnable shape a banchan already carries[^3]; unset means the
  bento's default completeness rule applies (every declared banchan
  present, assets passing). `max_passes: 0` means no looping: one pass,
  then judge.
- **The input bound is not a field, and does not need to be.** What this
  recipe accepts is declared by its first step's input contract, and what
  any *particular* run works on is the input bento's own manifest —
  finite, declared, and complete before the intent exists, because a
  bento is inert and bento-or-invisible held at the origin like
  everywhere else. Section 8 leans on this.
- **`hop_bound` is not authored.** The minting tool stamps it from the
  generated ceiling (section 2). An operator does not choose a bound per
  recipe; the mesh has one ceiling, held in the gen.

The hop arithmetic, in one place. Every dial derives an intent and
decrements the bound — including convergence re-entries. A convergence
pass is not a second currency: re-entering a step is a dial, a dial is a
derivation, and a derivation spends a hop. `max_passes` is the *author's*
tighter allowance inside that one purse, plus the semantics the bound
alone cannot give: the acceptance check names *why* the loop ended —
accepted, versus passes-exhausted, which goes PARTIAL or FAILED, loudly.
One law, one purse; convergence adds meaning, not budget.

That arithmetic is checkable before anything runs, so minting checks it:
a recipe of S steps and worst-case P convergence re-entries needs
S − 1 + P dials, and if that exceeds the ceiling the mint is REFUSED —
the recipe was impossible as authored, and it fails at the operator's
desk instead of mid-flight on hop 10.

Minting, then, is exactly this sequence, and it is the operator's act
(section 6, term 1): validate the authored file against the contract and
the arithmetic; allocate the working directory; seal the marshaled recipe
into it; deposit the input bento; stamp `intent_id`, `recipe_id` (the
hash), and `hop_bound` (the ceiling); submit the intent. Where the minting
tool lives is an open question (section 16); what it does is not.

## 6. The terms

The terms begin at minting, and minting is in scope of this document:
there is no speedy without the recipe defined (section 5). Every term
bounds the thing the halting problem says cannot be proven. These are the
conditions under which the frood law bends, and they are MUSTs:

1. **Recipes are minted only outside the mesh.** The operator, or an
   operator-agent acting on his behalf. A step executes and releases;
   `release` follows the itinerary *as written* and decrements the bound.
   A step MUST NOT add, reorder, or substitute steps — no runtime
   itinerary growth, ever. The mesh does not program itself: the gen
   gives a step no verb that could, and the recipe hash catches what a
   scribbled heap might try (section 5).
2. **Recipes are linear.** Branching is ruled out by name: once a recipe
   can fan out, there is no way to bound saturation of the one node this
   mesh lives on, or its disk. Parallelism comes from many linear intents
   in flight (section 9), never from one intent becoming several.
   Fan-out MAY be performed *internally* to a speedy frood — local states
   are invisible and bounded by their process — but it is inappropriate
   for a speedy frood to dial itself to accomplish fan-out. The one
   sanctioned cycle is `Convergence.max_passes`, judged by its acceptance
   check, spending from the same hop purse as everything else (section 5).
3. **The itinerary never exceeds the maximum.** A set — the word is
   deliberate; itineraries do not revisit, except through convergence —
   of state-calls is bounded by `hop_bound`, decremented at every
   derivation, REFUSED loudly at zero. Operator's standing ceiling: 10,
   held in the gen, checked at the mint.
4. **Step-complete-before-release.** The heap MUST be in a complete,
   discernible state before `release` fires: the bento-or-invisible rule
   applied per hop. A half-written step is invisible because the release
   that would reveal it has not happened.
5. **The itinerary is the trace.** What has been done and where we have
   been ride with the work (`parent_intent_id` chains; the bus log
   witnesses the dials, section 13). When something spirals, the chain
   names whose recipe to repair.
6. **Froodiness is transitive, and it is checked at the gate, not at
   runtime.** If something is admitted to the mesh, it passed the CI
   gates and runs generated code; if it says it is speedy, it is speedy,
   and it is froody. Bad behavior is banned at the protocol/gen layer,
   where it is a compile failure instead of an incident. hm is the
   backstop: misbehave at runtime and hm takes your lease.

## 7. Failure and resume

Nothing new is invented for recovery; the existing laws compose.

A frood that dies mid-step is marked by its lapsed hm lease (section 12).
Re-dialing the failed step is safe because froods discern applicability
from the artifact — "are these smoothed?" — so a re-invoked frood looks at
the heap and resumes from what is actually there, not from a
resume-token. Content-discernment makes retries idempotent. Term 4
guarantees the previous step's work is either sealed or invisible, so the
re-dialed frood never sees a half-truth. Host-side residue (a dead
process's scratch) is the sweeper's job, per haho's RFC.

## 8. "Behind" is the only degradation

The mesh never drops. A frood does not tolerate a drop at any point. When
the mesh is slower than its input, the flow is *behind*, and behind is
stated plainly rather than dressed as failure.

Behind is bounded, and the bounds are already on the page. Queue growth is
bounded three ways: at the front by the input bento's manifest — what was
deposited is finite, declared, and complete before the intent exists
(section 5) — in the middle by `hop_bound` (no amplification), at the back
by bus retention. The mesh is not fixed to cameras and capture: the
operator puts anything into the mesh he chooses — ambient picture
generation, near-real-time language interpretation, a tarball of memos —
and each of those is just an input bento with a manifest. Where the input
happens to be physical capture, the front bound comes cheap — a room
speaks at 1x while the processing states run at 49–77x realtime,
measured[^4], so a backlog is a transient the silicon eats — but capture
is an instance, not the premise.

How fast the product needed to arrive is not the mesh's concern. Velocity
requirements belong to the client at the edge: the mesh moves every piece
it was given and never discards one, and what a late piece *means* is
decided where the product is rendered (section 11). There is no deadline
anywhere in these semantics, on purpose — a deadline is a promise about
the future, and the mesh only makes promises about what it has already
done.

## 9. Faster than realtime

> "Never underestimate the bandwidth of a station wagon full of tapes
> hurtling down the highway."
> — Andrew S. Tanenbaum[^6]

That is speedy's actual ambition, stated by its patron saint: not high
velocity, but realtime-or-better performance by the measure that counts —
product delivered per unit of wall clock — achieved with capacity and
computation rather than with a low-latency socket.

Chunking can be faster than 1:1; a persistent socket cannot. The
operator's worked example: take a connection whose bandwidth is known — a
notorious refugee camp in the Netherlands, 500 kbyte/s, enough to stream
1080p at exactly 1:1 and no better. Streaming spends that wire at the
stream's own rate, always. Under speedy the same wire carries banchans:
chunks that use whatever capacity exists whenever it exists, compressed
and prioritized by computation at both ends — and with the processing
states running at 49–77x realtime, measured[^4], there is computation to
spare. The same 500 kbyte/s carries the 1080p *and a second data channel*,
because the product is packed, not streamed. The wire did not get faster;
the station wagon got fuller.

Parallelism is many linear intents in flight; one intent never becomes
several (term 2). The overlap in the worked example below is successive
bentos pipelining through the same states.

## 10. The worked example

The chain: unpack a tarball of voice memos, transcribe, smooth, stitch a
podcast script and render its audio — `recipe_X` of section 5. First, the
classic frood world — four froods, each with `{input, output}`, each
invoked by the operator, polling. The lanes are independent and never
intersect: classic frood has one arrow, and it is time. Data moves between
lanes only when the operator moves it — rsync, or asking the next frood to
cut a fresh bento — and that movement is off-mesh, so the picture cannot
honestly draw it as an arrow. FSMc appears twice: the same frood,
re-invoked, branching on what it finds — which is the by-hand version of
exactly what `Convergence` declares (section 5).

![The classic frood world: four independent polling lanes; the only arrow
is time; FSMc invoked twice](diagrams/speedy-poll.svg)

Now the same chain minted once as `recipe_X`, released thereafter — solid
blocks are the first intent's heap W, dashed blocks are a second intent
(W2) pipelining behind it through the same states; each block ends in a
release, whose dial fires before the block ends:

![The dialed world: recipe_X minted once, W traveling by reference, hop
counts decrementing, W2 pipelining behind](diagrams/speedy-dialed.svg)

One mint instead of four invocations per bento; the heap travels by
reference; W2 pipelines behind W through the same states. The operator
mints the recipe and receives the end data; everything between is
release.

## 11. The terminus

The final step's `release` has no next capability to resolve, so it
publishes instead of dialing — and publication is the scope's only exit
into the world:

1. The heap is sealed one last time (term 4 applies to the terminal step
   like any other).
2. The end data is published as a bento, following the bento convention —
   a manifest of declared banchans, complete when every banchan is
   present and its assets pass[^3]. End data is *start data's* equal: an
   inert artifact in a directory, causing nothing.
3. The completion record rides the bus — the scope's landing, the durable
   fact that recipe_X over intent W produced end data at this path. The
   bus copy is the copy of record, exactly as haho completions work.
4. The process exits, and the scope ends (section 12).

Everything after publication is **edge software**: a renderer — a podcast
feed, mackerel's scrolling transcript, a wall display — reads published
end data and presents it by its own policy. Rendering policy is where
late-arriving pieces get their meaning: skip, elide, annotate, hold — the
renderer's choice, invisible to the mesh, which delivered everything.
(mackerel's recorded "drop the segment rather than block the stream" is
re-ruled as exactly that: a rendering choice at the terminus. The mesh
dropped nothing.)

The terminus is also where *behind* stops being an adjective and becomes a
number, so the receipts live here:

- **On the completion record** (mesh-side, per intent): mint-to-publish
  wall time; per-step wall times as witnessed by the dials (section 13);
  convergence passes consumed versus allowed; hops consumed versus
  stamped.
- **On the renderer** (edge-side, ordinary `/metrics` per fleet rule):
  arrival spread per banchan, pieces elided or annotated by policy,
  presentation lag against whatever clock the product cares about.

None of this is mesh semantics — the mesh does not know what "late" means
— but all of it is measurable, and a flow that is behind SHOULD be visible
on a dashboard without anyone grepping a log. The numbers are receipts,
not promises.

## 12. Scope

A **scope** is everything done under one intent: one heap's journey from
start data to end data, mint to publish. The word deliberately does
double duty, and the double duty is the design:

- **On the mesh**, the scope is the correlated flow — the intent chain,
  the dials, the heap's travel. It begins at minting and ends at the
  terminal `release`, and nothing else ends it.
- **On the host**, hahod keeps a row it also calls a scope for every
  piece of work it supervises; that row is the host-local record of the
  same thing. When a recipe step's work runs as a haho chute (section
  14), the chute's scope row and the mesh scope it serves are two views
  of one fact.

What a scope is *not* is a frood's insides. A frood's internal states are
its own: invisible outside the process, bounded by it, never mesh
semantics. The mesh sees a frood take the heap at a dial and release it
once — everything between is local, which is what makes the boundaries of
section 1 mathematical.

hm mirrors host-side flights as leases, and the reaping rule reads the
same at both levels: heartbeats begin when there is tasking; no tasking,
no heartbeats; no heartbeats, no lease renewal; no renewal, reaped. A
scope that is not working is not alive, and hm reaps what is not alive.
This is the lease chain section 7 leans on for failure marking.

## 13. Observability

The bus log is the itinerary's witness, and that sentence has concrete
receipts:

- **Every dial is a bus record**: which intent, which capability was
  resolved to which frood, the heap's path, the remaining bound, when.
  Tail the topic and watch a recipe walk its itinerary hop by hop —
  no additional machinery, the log *is* the trace.
- **The intent chain is attribution**: `parent_intent_id` from any
  derived intent walks back to the root and names the recipe and
  submitter. When a bound trips, the chain says whose program to repair
  (term 5).
- **The completion record is the landing** (section 11): one durable fact
  per scope, on the bus, carrying the receipts.
- **Liveness is the lease chain** (section 12): a step in flight is a
  renewed lease; a lapsed lease without a landing is the alarm, on a
  dashboard, by construction.
- **Post-hoc, the heap itself**: sealed step by step (term 4), it is the
  physical audit trail of what each capability actually did — and the
  sealed recipe pins what the program was (section 5).

What the operator watches mid-recipe is therefore: the dial topic for
motion, the leases for liveness, the completion for the landing. Anything
model-shaped inside a step additionally carries haho's full observability
(events, status endpoint, chute leases) per haho's RFC section 13 — that
machinery is referenced here, not restated.

## 14. haho

A speedy frood does not require haho. speedy is a property of the mesh —
any froody frood can be dialed, advance the heap, and release. haho
*implements* speedy for chutes: when a recipe step's work is model-shaped,
the frood that executes it is a haho chute, and haho's admission, lanes,
and framing apply to that step's interior. haho consumes this document's
semantics; it does not define them, and nothing here depends on it.

## 15. What speedy is not

- **Not streaming.** A system of packets with a bounded itinerary, not a
  persistent socket (section 3).
- **Not client/server.** Origin, terminus, and a bounded number of states
  between (section 3).
- **Not orchestrated — self-orchestrating.** The coordination is the
  itinerary, riding the data, executed one dial at a time. There is no
  orchestrator node to build, deploy, or misuse. (mackerel's held
  question — whether a converging coordinator is a misuse of the mesh —
  dissolves: there is no coordinator; convergence is `max_passes`,
  authored at minting.)
- **Not branching.** Ruled out by name, per the terms (section 6).
- **Not a delivery-guarantee change.** The bus stays what it is; intents
  stay idempotent by `intent_id`.
- **Not a velocity promise.** No deadline exists in these semantics; how
  fast the product needed to arrive is the client's concern at the edge
  (sections 8 and 11).
- **Not intruder.** intruder is a separate project — client-side, a
  resident in-browser wrapper over the terminus of a mesh flow, with a
  batcher/chunker underneath depositing bento-shaped work. It may serve
  as a case study for speedy someday; it is out of scope here.

## 16. Open questions

- **Dial authorization.** Transitive froodiness admits a frood to the
  mesh; whether *any* froody frood may dial *any* addressable name, or
  whether dialing rides an allowlist minted with the recipe, is open. The
  terms lean on gen-layer bans and hm's backstop; a stronger story can
  wait for evidence it is needed.
- **The minting tool.** Minting itself is in scope and required (section
  6); what remains open is where the tool lives — a CLI, a holden verb, a
  library. Content addressing narrows the companion question: `recipe_id`
  resolves by hash against the sealed artifact, so a recipe registry, if
  one ever exists, is an index for humans, not infrastructure the mesh
  needs[^2].
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
    prevention + parent_intent_id attribution, intent transport open, a
    recipe registry deliberately undefined.

[^3]: `big-little-mesh/proto/bento/v1/bento.proto`: "a banchan is an
    irreducible element of that batch"; a bento is complete when every
    declared banchan is present and its assets pass; `BanchanAsset` is
    the declared-runnable check shape `Convergence.acceptance` reuses.

[^4]: haho `docs/haho-rfc.md`, Appendices A–C: whisper on MLX at 63–77x
    realtime; SpeechTranscriber at 49–77x realtime serial, and genuinely
    concurrent at ~20% per-job cost (wall/sum 0.42 on three heavy
    transcriptions); measured on the hardware whose signature the
    appendices carry.

[^5]: The shape is old: perl's POE gives each state a kernel, a shared
    heap, and states that end by asking the kernel to call the next state
    in line — never a name the state knows itself. `release` is that
    idiom, generated with the contract.

[^6]: Andrew S. Tanenbaum, *Computer Networks* (the aphorism appears from
    the first edition, 1981, onward). Usually quoted of station wagons
    and tapes; the operator's telling has DLTs in it, which only
    strengthens the arithmetic.
