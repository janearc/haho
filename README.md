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

Two systems compile here today, deliberately separate:

**The tranche implementation**, landed ahead of ratification (the RFC set
is still DRAFT). The `haho.job.v1` contract is transcribed verbatim from
the RFC's section 8 sketch, generated code is committed (gen-freshness
holds and is checkable: regenerate == committed), and `bin/hahod` serves
the RFC's console walkthrough on loopback — sessions, the section 10
admission table, structured JSON logging, `/health` and `/metrics`. The
served surface is exactly: api lane, both api kinds
(`ANTHROPIC_COMPATIBLE` and `OPENAI_COMPATIBLE` — local ollama rides the
latter, credential-free), `WHOLE_OR_ERROR`, synthesis `NONE`, cache
`NONE`, no filters. Everything else the contract can express is REFUSED
at admission, loudly, by name.

Divergences from the RFC, surfaced (each is queued work, none is silent):

- Chutes run in-process inside hahod; the harness process split and the
  SBPL sandbox are not built.
- Completions land in hahod's store only; bus publication — the RFC's
  copy of record — is not built.
- No JobEvent surface exists; requestors poll `/sessions/{id}`.
- No session idle TTL: a created-but-never-tasked session is never
  reaped, and the store is never pruned — the sweeper is future work.
- No hm leases or heartbeats: the RFC's lease chain (taken at admission,
  renewed through the descent, released at landing) is unbuilt, and the
  vendored heartbeat message is carried by the contract but not yet
  emitted.
- The task response's status address is hahod's session URL, not a
  per-chute status port, and the chute's mid-descent status shape does
  not exist.
- The session envelope JSON (`new`/`task`/`status` bodies) matches the
  RFC's console examples but is hand-shaped, not contract messages.
- Trust model v1: loopback is the trust boundary. Any same-uid process
  can submit a spec naming any `base_url` and env-var reference — the
  RFC's worked specs require that freedom — so requestor authentication
  and a `base_url` allowlist are open operator questions, deliberately
  not pre-decided in code.

**The pre-RFC proof of concept** (`cmd/haho`, the older `internal/`
packages): an LLM proxy with a control port, a filter pipeline, model
providers, and a SurrealDB-backed cache and RAG store. It predates the
RFC and disagrees with it on purpose-defining points — it calls itself a
harness (the RFC reserves that word for the per-job scope hahod creates),
it containerizes (chutes are sandboxed native processes), and it proxies
models rather than executing jobs. It earned several of the lessons the
RFC is built on; its retirement is its own diff. Its operational surface
is deliberately not documented here — it lives in this file's git
history, so this README never describes two systems' knobs at once.

## Running hahod

hahod runs as a launchd user agent. Install once —
`deploy/install-daemon.sh` builds the binary, renders the plist with
absolute paths, and bootstraps it — and from then on it is managed with
launchctl like any other daemon on the host:

```
launchctl bootout   gui/$UID/com.janearc.hahod    # stop: SIGTERM, drain-and-land
launchctl bootstrap gui/$UID ~/Library/LaunchAgents/com.janearc.hahod.plist
launchctl kickstart -k gui/$UID/com.janearc.hahod # restart (e.g. after rebuild)
launchctl print     gui/$UID/com.janearc.hahod    # status
curl -s 127.0.0.1:8790/health                     # the daemon's own answer
```

Logs are structured JSON at `~/.local/state/haho/log/hahod.log`; the
store is `~/.local/state/haho/hahod.db`. The daemon serves loopback only
(`127.0.0.1:8790` by default; `HAHOD_ADDR`/`HAHOD_STORE` or flags
override). Stopping is graceful by construction: the listener stops,
in-flight jobs land (FAILED with the cancellation as reason if they
cannot finish), queued jobs land FAILED without running, and only then
does the store close. A crash instead of a stop is converged at the next
boot: the recovery sweep lands any orphaned row FAILED, loudly, before
serving — and launchd's KeepAlive restarts a crashed daemon, which that
sweep makes safe.

The hello world is the RFC's own walkthrough: the `haho` CLI (a thin
wrapper over the `client` package — any Go requestor links the package
instead) dialing the machine's own model, local ollama over the
`/api/chat`-shaped door, no credential anywhere, because the paid api
lane is the last resort and not the default:

```
SID=$(haho new | jq -r .id)
haho task $SID deploy/hello-world.yml   # or .json; YAML is the authored form
haho status $SID                        # in_flight, disposition
haho wait $SID                          # poll to landing, print the JobCompletion
```

The API underneath is plain loopback HTTP with protojson bodies
(`POST /sessions`, `POST /sessions/{id}/task`, `GET /sessions/{id}`,
`GET /sessions/{id}/completion`), so curl works wherever the CLI is not
at hand.

To dial a paid api instead, the spec's backend descriptor changes and
nothing else does: `kind` ANTHROPIC_COMPATIBLE, the api's base_url, and
a `tokenEnv` naming a credential. `token_env` is per-spec and
requestor-owned — holden's specs will name holden's credential, an
operator job names the operator's; hahod is bound to none of them and
only checks that the named var is set (unset = REFUSED at admission with
`secret_env_missing` naming the var, loud, spending nothing). Getting a
value into the supervised daemon's environment is `launchctl setenv` by
the operator's own hand; a durable secrets story is deliberately
deferred work.

- Contracts: `proto/` (buf v2); consumers vendor-generate, gen-freshness
  holds.
- Build and test: `make build`, `make test`.
- The RFC's measurements will be re-runnable: `haho bench` is designed to
  ship with haho (RFC section 15) so the numbers the design leans on can
  be re-grounded on new hardware and OS builds instead of fossilizing.
  Today the appendices' inline probes are what is re-runnable.
