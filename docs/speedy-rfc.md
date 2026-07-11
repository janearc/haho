# speedy — RFC

- Status: DRAFT. Ratification by the operator and fable marks this file
  read-only, as is the usual convention. haho's ratification is blocked on
  this document (haho-rfc.md, section 17); this document has no blockers of
  its own.
- Authors: Max (operator); Claude (Fable 5, drafting). Review happens on the
  PR.
- Companions: `docs/haho-rfc.md` (haho enforces speedy's semantics on the
  host; sections 2, 8, and 10 there carry the fields this document gives
  meaning to); mackerel `DESIGN.md` (the pipeline speedy exists for);
  big-little-mesh PR 84, the composition surface (Intent/Recipe,
  hop-bounded provenance — the wire speedy's metadata rides).

Contents:

1. What speedy is
2. The promise: speedy, never realtime
3. The deposit
4. Semantics
5. The wire
6. Failure, gaps, and honesty
7. What speedy is not
8. The driving consumer, worked
9. Open questions

## 1. What speedy is

speedy is an idea, not software. It is the mesh's defined near-real-time
processing mode: a set of semantics — deadlines, drops, gaps, end-to-end
staleness — that pipelines opt into when their output is worth less the
later it arrives. Its entire build surface is contract changes (a small
metadata message and the fields haho already carries) plus the discipline
this document binds those fields to.

The mesh's standing composition rule is that pipelines never call
pipelines; work moves by **deposit** — a typed job published onto the
substrate that another pipeline happens to consume. speedy is what deposits
gain when time enters the contract: every deposited unit knows when it was
captured, how long it is worth pursuing, and what to do when that time
runs out.

One library carries the implementation: big-little-mesh grows a `speedy`
package with a capability declaration. There is deliberately no
speedy-streaming sibling, no speedy service, no speedy repo. A froody
service that declares the speedy capability participates in deadline-
bearing work; one that does not, does not, and never sees the fields.

## 2. The promise: speedy, never realtime

The promise is stated once and every consumer designs against it: **speedy
means near-real-time, and the mesh never promises realtime.** The formal
shape of the promise:

- A speedy pipeline is one that, given unbounded hardware and bandwidth,
  would be realtime. The lag is load and physics, never queued debt.
- Concretely it promises **bounded staleness**: every unit either produces
  its result within its deadline or is dropped, so the consumer is always
  at most one deadline behind the world. It never promises completeness.
- The axiom is **freshness over completeness**. mackerel's design states
  the human version: a passive listener would rather miss a sentence than
  fall a paragraph behind. A speedy pipeline that queues is lying to its
  consumer about what time it is.

This is why the mode is named speedy and not streaming or realtime: the
name is the promise, sized honestly.

## 3. The deposit

A deposit is one correlated unit of work published onto the substrate. For
speedy work, the unit is a **segment**: the client-side cut decides where
one segment ends (mackerel's client-A exists to make that cut well), and
everything downstream — transcription, interpretation, delivery — happens
to that segment as a unit, threaded by correlation.

The correlation key is `correlation_id` (a session) plus `segment` (a
dense ordinal within it). Dense matters: consumers reassemble by ordinal,
and a missing ordinal is *information* — a drop, renderable as a gap —
rather than ambiguity. Segment ordinals are assigned once, at capture, and
never renumbered downstream.

A deposit is not an RPC. The depositor holds no handle to the consumer, gets
no reply channel, and MUST NOT block on the result; results are themselves
deposits (or, at the mesh's edge, deliveries to a client). This is the
standing composition rule restated with time attached: choreography over
the bus, with every hop aware of the clock.

## 4. Semantics

The binding rules. haho enforces the host-local ones today (its `Deadline`,
`DROPPED_LATE`, and admission-drop are this section's mechanics); the
cross-pipeline ones bind any froody service declaring the speedy
capability.

1. **The deadline is end-to-end.** `deadline_ms` is measured from
   `created_at` — the segment's capture time at the origin — not from any
   hop's receipt. Every hop computes `remaining = deadline_ms - (now -
   created_at)` and works within it. A per-hop deadline would let each hop
   be individually punctual while the pipeline as a whole falls behind;
   the end-to-end clock makes lateness visible at the hop where it
   actually happened.
2. **Admission is the first deadline check.** A hop that cannot plausibly
   finish within `remaining` MUST drop at admission rather than start.
   Failing before spending — haho applies this rule at hahod's queue, and
   the bus-level rule is the same one.
3. **Late work is dropped, and a drop is an event, not an absence.** When
   `on_late = DROP` fires, the dropping hop publishes a `Dropped` record
   (section 5) naming the segment, the hop, the reason, and the segment's
   age. Consumers render gaps; operators count them. Nothing downstream
   waits on a segment whose deadline has passed.
4. **No retries past usefulness.** Retry policy inside a hop (haho's
   framing ladder, resize-on-fail) is bounded by `remaining` like
   everything else. There is no redelivery of a dropped segment; its time
   is gone, which was the point of dropping it.
5. **Ordering is the consumer's job, gaps allowed.** The bus does not
   promise cross-partition order. The consuming edge reassembles by
   `(correlation_id, segment)` and MUST NOT block a rendered stream
   waiting for a missing ordinal past that segment's deadline: a hole
   whose deadline has passed is a gap, rendered as one.
6. **Delivery is at-least-once; consumers dedupe.** The substrate may
   deliver a unit twice. `(correlation_id, segment, stage)` is the
   idempotency key; haho's content-addressed cache makes the duplicate's
   cost approach zero, but correctness comes from the key, not the cache.
7. **A speedy pipeline degrades loudly.** Drop-rate is a first-class
   health signal (section 6). Silently thinning output while reporting
   healthy is the one behavior this document exists to forbid.

## 5. The wire

The build surface, concretely. Two halves: fields haho already carries,
and one small message the composition surface gains.

**Already in haho** (`haho.job.v1`, defined in haho-rfc.md section 8):
`correlation_id`, `Deadline { deadline_ms, on_late }`, the `DROPPED_LATE`
outcome, and admission-drop at hahod. This document is where those fields'
cross-pipeline meaning lives; haho's RFC deliberately defers to it.

**The deposit metadata** — a sketch of the message the big-little-mesh
composition surface grows. Final package and placement follow the
composition surface's own contracts (Intent/Recipe, PR 84); the fields are
the RFC content, the coordinates are not:

```proto
// Rides every speedy deposit; absent on batch work. The composition
// surface owns the enclosing envelope; these are the speedy fields.
message Speedy {
  string correlation_id = 1;                 // the session
  uint32 segment = 2;                        // dense ordinal, assigned at capture
  google.protobuf.Timestamp created_at = 3;  // origin capture time; the
                                             // end-to-end clock starts here
  uint32 deadline_ms = 4;                    // end-to-end, from created_at
  OnLate on_late = 5;                        // DROP for speedy work

  enum OnLate {
    ON_LATE_UNSPECIFIED = 0;
    DROP   = 1;
    FINISH = 2;  // batch riding the same envelope; not a speedy promise
  }
}

// Published by the hop that drops. A drop is an event, not an absence.
message Dropped {
  string correlation_id = 1;
  uint32 segment = 2;
  string stage = 3;        // which hop dropped (transcribe, interpret, ...)
  string why = 4;          // admission-refused | deadline-expired | window-missed
  uint32 age_ms = 5;       // segment age at the drop, per the end-to-end clock
}
```

**The capability declaration.** A froody service that consumes or produces
deadline-bearing topics declares `speedy` in its capability set. The CI
gates hold declarations to behavior (a declared-speedy consumer MUST
implement admission-drop and `Dropped` publication); at runtime, the
declaration is simply trusted — if it says it is speedy and it is on the
network, it is speedy.

Hop provenance is the composition surface's own field (hop-bounded, per
PR 84) and speedy adds nothing to it beyond using it in `Dropped.stage`.

## 6. Failure, gaps, and honesty

A speedy pipeline has three observable failure currencies, each with a
home:

- **Drops** — per-segment `Dropped` events on the bus. The consuming edge
  renders them (intruder shows a gap marker, not a stall); operators see
  them in aggregate.
- **Drop-rate** — the pipeline-level health signal. Every declared-speedy
  service exposes its drop-rate over a sliding window on its existing
  health surface; a rate above the service's declared threshold is
  DEGRADED, reported loudly. The threshold is the service's to declare
  and the operator's to tune; the *existence* of the threshold is not
  optional.
- **Staleness** — the age of the freshest delivered segment, per
  correlation. This is the consumer's felt experience ("how far behind
  the room am I") and the number the promise in section 2 is audited
  against.

What is forbidden: queueing past deadline-usefulness (rule 2), blocking a
stream on a dead segment (rule 5), and thinning output silently (rule 7).
A speedy pipeline under load looks like: gaps appear, drop-rate rises,
DEGRADED is reported, staleness holds — the listener stays a few seconds
behind a thinner stream. It never looks like: everything delivered,
minutes late, health green.

## 7. What speedy is not

- **Not streaming.** No unbounded connections, no byte streams; discrete
  correlated segments with clocks on them. The word streaming is avoided
  on purpose (standing ruling, 2026-07-09).
- **Not realtime, and never promised as such** (section 2).
- **Not a service, a repo, or a binary.** A package in big-little-mesh's
  one library, a capability declaration, and these semantics.
- **Not a delivery-guarantee upgrade.** The bus stays at-least-once;
  speedy adds time semantics, not transactional ones.
- **Not the coordinator.** Whether mackerel's converging orchestration is
  pure choreography or a non-pipeline orchestrator is mackerel's held
  question and the composition surface's business; speedy defines what a
  deposited unit means under time pressure regardless of who deposits it.
- **Not batch's replacement.** `FINISH` exists precisely so batch work can
  ride the same envelope without inheriting the drop discipline; a corpus
  sweep is never speedy.

## 8. The driving consumer, worked

The interpreting loop (mackerel's mesh side, per its design and haho-rfc
section 12), as speedy sees it. One segment, target language English,
end-to-end budget 5000 ms from capture:

| Hop | Where | Typical | Speedy mechanics |
|---|---|---|---|
| capture + cut | client (intruder) | ~0 (the cut IS the boundary) | `created_at` stamped; `segment` assigned; deposit |
| transcribe (+translate to en) | haho chute, metal lane | ~1s for a 45s segment | admission checks `remaining`; whisper runs whole-or-error |
| interpret / smooth | haho chute, ane lane | ≤3s, deadline-bound | framing ladder bounded by `remaining`; cache hits are free deadlines |
| deliver text | bus → client | ~0 | consumer reassembles by ordinal; dead ordinals render as gaps |

The budget arithmetic is the design: roughly a second of slack absorbs
load variance before anything drops, and when the room outruns the mesh,
segments drop *at admission* — the newest work proceeds, the listener
stays current, and the transcript shows honest gaps. "A few seconds
behind, which is the whole reason this is tractable" — the mackerel design
sentence this document turns into mechanics.

holden, for contrast, is the null case: rulings ride `FINISH` with no
deadline, and speedy's machinery never touches them. The same envelope,
none of the drop discipline — which is exactly the point of `on_late`.

## 9. Open questions

- **Intent transport.** Whether Intents move over the bus or a
  dataprovider is an OPEN ruling on the composition surface (operator has
  not ratified; the bus recommendation stands unanswered, 2026-07-09).
  speedy's fields are transport-neutral on purpose; this document MUST NOT
  be read as deciding that question.
- **Per-segment spawner ergonomics.** The loop "consume segment → build
  JobSpec → `haho task`" will be written identically by every speedy
  pipeline; whether the big-little-mesh `speedy` package ships that loop
  as a helper (a segment-driven spawner) is a library-design question for
  when the second consumer exists.
- **Drop-rate thresholds.** Each service declares its own DEGRADED
  threshold; whether the mesh wants a default (and what it is) waits for
  operational data from the first speedy pipeline.
- **Client-side gap rendering.** How intruder renders gaps (marker, elision,
  timing hint) is intruder's design; the `Dropped` event carries enough
  for any of them.
