# haho

haho is an LLM harness for the big-little-mesh and delightd projects. It provides a unified interface for running local models (Ollama), Claude (Opus), and DeepSeek, with SurrealDB-backed RAG and response caching.

## Architecture

```
┌──────────────────────────────────────────────────────────┐
│                     haho harness                         │
│                                                          │
│  ┌─────────┐   ┌──────────┐   ┌───────────┐             │
│  │  Input   │──▶│  Cache   │──▶│  Model    │             │
│  │ Filters  │   │  Lookup  │   │ Provider  │             │
│  │ (RTK)    │   │(SurrealDB)│  │(Ollama/   │             │
│  └─────────┘   └──────────┘   │ Opus/DS)  │             │
│                                └───────────┘             │
│                                     │                    │
│                               ┌─────▼─────┐             │
│                               │  Output   │             │
│                               │ Filters   │             │
│                               │(passthru/ │             │
│                               │  logger)  │             │
│                               └───────────┘             │
│                                                          │
│  ┌────────────────────────────────────────┐              │
│  │  Control Port (HTTP/JSON)              │              │
│  │  POST /v1/submit  GET /v1/health       │              │
│  │  GET /v1/config   POST /v1/filters     │              │
│  └────────────────────────────────────────┘              │
│                                                          │
│  ┌────────────────────────────────────────┐              │
│  │  SurrealDB (k3s-discovered)            │              │
│  │  - RAG document store                  │              │
│  │  - Response cache                      │              │
│  └────────────────────────────────────────┘              │
└──────────────────────────────────────────────────────────┘
```

## Quick Start

```bash
# Build
make build

# The harness authenticates to SurrealDB (2.x denies unauthenticated access)
# and, if the control port token is set, requires it on the mutating endpoints.
export SURREAL_CREDENTIALS='root:root'   # user:pass; operator-injected
export HAHO_CONTROL_TOKEN='...'          # optional; guards /v1/submit and /v1/filters

# Run with default config (local Ollama, SurrealDB on an exposed :8000)
./bin/haho

# Run with custom config
./bin/haho -config configs/default.json
```

Each harness runs in its own container; run as many as you need, all pointed at
the same SurrealDB via its exposed port (`surrealdb.discovery: "static"`).

## Environment

| Variable | Purpose |
|---|---|
| `SURREAL_CREDENTIALS` | SurrealDB `user:pass` (name set by `surrealdb.credentials_env`). Falls back to `root/root` with a warning. |
| `HAHO_CONTROL_TOKEN` | Bearer token required on `POST /v1/submit` and `POST /v1/filters`. Unset = open (logged loudly). |
| `<model.api_key_env>` | API key for the `opus`/`deepseek` providers. Startup fails loudly if the named var is empty. |
| `HAHO_K3S_INSECURE=1` | k3s discovery only: skip API-server TLS verification when no CA cert is present (dev). |

## Configuration

All configuration is JSON. See `configs/default.json` for a complete example.

```json
{
  "listen_addr": ":9090",
  "model": {
    "provider": "local|opus|deepseek",
    "endpoint": "http://localhost:11434",
    "model_name": "llama3",
    "api_key_env": "ANTHROPIC_API_KEY"
  },
  "surrealdb": {
    "discovery": "k3s|static",
    "namespace": "default",
    "service_name": "surrealdb",
    "database": "haho",
    "host": "localhost",
    "port": 8000
  },
  "filters": {
    "input": [{"name": "rtk", "type": "RTK"}],
    "output": [{"name": "passthrough", "type": "PASSTHROUGH"}]
  },
  "cache": {
    "enabled": true,
    "ttl_seconds": 3600
  }
}
```

## API

Contracts are defined in `proto/` (protobuf). The PoC implements HTTP/JSON.

| Endpoint | Method | Description |
|---|---|---|
| `/v1/submit` | POST | Submit messages for LLM completion (auth; single-flight — returns 429 while busy) |
| `/v1/health` | GET | Harness health and state |
| `/v1/config` | GET | Current configuration |
| `/v1/filters` | POST | Update filter pipelines at runtime (auth; `binary_path` rejected over the wire) |
| `/v1/metrics`, `/metrics` | GET | Counters (uptime, state, submit totals/busy/errors) |

### Health States

| State | Description |
|---|---|
| `idle` | Ready for work |
| `busy` | Processing a request |
| `waiting_subprocess` | Waiting on model (child process) |
| `waiting_input` | Blocked on user/upstream input |

### Example: Submit

```bash
curl -X POST http://localhost:9090/v1/submit \
  -H 'Content-Type: application/json' \
  -d '{"messages": [{"role": "user", "content": "hello"}]}'
```

### Example: Health Check

```bash
curl http://localhost:9090/v1/health
```

## Filter Pipeline

Input and output filters are composable and configurable at startup (JSON) or runtime (API).

| Filter | Type | Description |
|---|---|---|
| RTK | Input (default) | Pipes through `rtk` CLI for context filtering/summarization |
| Passthrough | Output (default) | No-op, passes messages through unchanged |
| Logger | Output | Logs all messages to stderr (or custom writer) |

## Model Providers

| Provider | Backend | Default Endpoint |
|---|---|---|
| `local` | Ollama `/api/chat` | `http://localhost:11434` |
| `opus` | Anthropic Messages API | `https://api.anthropic.com` |
| `deepseek` | OpenAI-compatible API | `https://api.deepseek.com` |

## SurrealDB

SurrealDB (2.x) is reached over its HTTP `/sql` API. The default deployment
connects statically to an exposed port (`discovery: "static"`, `host` + `port`);
k3s service discovery stays available for in-cluster runs (`discovery: "k3s"`).
All user-supplied values are bound as query variables, never interpolated into
SurrealQL. Used for:
- **RAG**: BM25 full-text search over indexed documents. The analyzer and search
  index are created at startup (`EnsureSchema`); search is disabled with a
  warning until they exist.
- **Cache**: Response caching with a real per-entry expiry (`cache.ttl_seconds`)
  to avoid redundant LLM calls.

## Testing

```bash
# All tests
make test

# Unit tests only
make test-unit

# Integration tests
make test-integration
```

## Project Structure

```
cmd/haho/          CLI entry point
proto/             Protobuf contracts
configs/           Default configuration
internal/
  config/          JSON config loading
  filter/          Input/output filter pipeline
  model/           LLM provider interface + implementations
  discovery/       k3s service discovery
  rag/             RAG store + cache (SurrealDB)
  health/          Health state tracking
  harness/         Core orchestrator
  server/          HTTP/JSON control server
test/integration/  Integration tests
```
