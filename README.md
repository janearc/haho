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

# Run with default config (local Ollama)
./bin/haho

# Run with custom config
./bin/haho -config configs/default.json
```

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
| `/v1/submit` | POST | Submit messages for LLM completion |
| `/v1/health` | GET | Harness health and state |
| `/v1/config` | GET | Current configuration |
| `/v1/filters` | POST | Update filter pipelines at runtime |

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

SurrealDB is discovered via k3s service discovery (or configured statically). Used for:
- **RAG**: Full-text search over indexed documents
- **Cache**: Response caching to avoid redundant LLM calls (TTL-based)

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
