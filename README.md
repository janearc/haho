# haho

haho exposes borg-shaped things to fruit-flavored jobs: typed specs,
admission control, leases, and bus-carried completions, delivered to
native macOS processes with the GPU, the Neural Engine, and the OS's warm
foundation model an arm's reach away. One chute, one jump, one landing.

This README is the front door, not the reference. The reference is the
RFC set below.

## The documents

The design is a three-document tranche — one vocabulary, one review, one
ratification; none of the three ratifies alone:

| Document | What it holds |
|---|---|
| `docs/haho-rfc.md` | haho itself: the discernment equation, lanes, chutes, hahod, framing, the job contract, and the measurements behind every budget (appendices A–C) |
| `docs/speedy-rfc.md` | the mesh's recipe semantics — bounded, dialed composition of froods; haho implements speedy for chutes and does not define it |
| `docs/holden-rfc.md` | holden, haho's first consumer; continues ADR-0003 in janearc/holden, which keeps the decision record |

Status: DRAFT, under review as a tranche. Ratification by the operator
and fable marks the files read-only at their paths; from then on the
ratified set is the reference for the integrity of the project, as is
the usual convention.

## State of the code

What compiles here today is the pre-RFC proof of concept: an LLM proxy
with a control port, a filter pipeline, model providers, and a
SurrealDB-backed cache and RAG store. It predates the RFC and disagrees
with it on purpose-defining points — it calls itself a harness (the RFC
reserves that word for the per-job scope hahod creates), it containerizes
(chutes are sandboxed native processes; the RFC's placement study is
exactly why), and it proxies models rather than executing jobs. It earned
several of the lessons the RFC is built on, and the RFC's contract recast
— `haho.job.v1` (JobSpec / JobEvent / JobCompletion), hahod, chutes —
replaces it as the first post-ratification work.

The PoC's operational surface (config, endpoints, env) is deliberately
not documented here; it lives in this file's git history, so this README
never describes two systems at once.

- Contracts: `proto/` (buf v2); consumers vendor-generate, gen-freshness
  holds.
- Build and test: `make build`, `make test`.
- The RFC's measurements are re-runnable: `haho bench` ships with haho
  (RFC section 15), so the numbers the design leans on can be re-grounded
  on new hardware and new OS builds instead of fossilizing.
