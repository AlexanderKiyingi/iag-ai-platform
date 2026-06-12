# iag-ai-platform

Shared **AI orchestrator** for the IAG platform. It gives every microservice one
authenticated, observable place to run multi-agent workflows, LLM inference,
reusable prompts, and embeddings — with per-caller usage attribution.

Built in Go on the same platform plumbing as `iag-fleet` / `iag-procurement`:
Bearer + `aud` JWT verification (iag-authentication JWKS), OpenTelemetry tracing,
Postgres migrations, health/ready probes, and boot-time permission registration.

## Why a shared service

Instead of each microservice embedding its own model client, API keys, prompts,
agents, and cost tracking, they call `iag-ai-platform` with a service token. A
single request can be answered by a **team of agents** that pull live data from
across the platform.

## Multi-agent orchestration

The orchestrator decomposes a task and routes subtasks to specialist agents,
which call IAG microservices to get real data, then synthesizes the answer:

```
POST /orchestrate {"task": "..."}
        │
   coordinator agent ── delegate ──▶ finance-analyst ──▶ call_microservice → iag-finance
        │                        └─▶ procurement-analyst ─▶ call_microservice → iag-procurement
        ▼
   synthesized answer + full run trace (GET /runs/:id)
```

- **Agents** ([`internal/agent`](internal/agent)) are DB-backed: a system prompt,
  a model, and the tools they may use. A `coordinator` plus `finance-analyst`,
  `procurement-analyst`, and `ops-analyst` are seeded at boot; manage them via
  `/api/v1/agents`.
- **Tools** ([`internal/tools`](internal/tools)): `list_services` and
  `call_microservice` (service-to-service auth, read-only by default, RBAC still
  enforced by the target service) expose backends to agents; `delegate` hands a
  subtask to another agent. Recursion is depth-bounded.
- **Runner** ([`internal/agent/runner.go`](internal/agent/runner.go)) drives the
  Claude tool-use loop and records every step to `ai_runs` / `ai_run_steps`.

Configure callable backends with `AI_SERVICES_JSON`, e.g.
`{"finance":{"audience":"iag.finance","baseUrl":"http://iag-finance:3006","description":"GL, AR/AP, budgets"}}`.

## Provider

The inference backend is pluggable ([`internal/provider`](internal/provider)):

- **Anthropic (Claude)** — used when `ANTHROPIC_API_KEY` is set (the real backend).
- **Stub** — deterministic, key-free responses for local dev and tests; selected
  automatically when no API key is configured. Production refuses to start on the
  stub.

Embeddings use a built-in deterministic feature-hash embedder
([`internal/provider/embeddings.go`](internal/provider/embeddings.go)) so
semantic-similarity flows work with no external dependency; swap in a real
embeddings provider later.

## API (all under `/api/v1`, Bearer required)

| Method | Path | Permission | Purpose |
|--------|------|-----------|---------|
| GET  | `/overview` | `ai.use_inference` | Service status, active provider/model |
| POST | `/orchestrate` | `ai.run_agents` | Run the multi-agent coordinator on a task |
| POST | `/agents/:name/run` | `ai.run_agents` | Run a single agent (tool-use loop) |
| GET  | `/agents` · POST `/agents` | `ai.manage_agents` | List / create-update agents |
| GET·DELETE | `/agents/:name` | `ai.manage_agents` | Get / delete an agent |
| GET  | `/tools` | `ai.run_agents` | List tools + callable services |
| GET  | `/runs/:id` | `ai.run_agents` | Fetch a run's full step trace |
| POST | `/completions` | `ai.use_inference` | Chat/text completion (`prompt` or `messages`) |
| POST | `/embeddings` | `ai.use_inference` | Vector embeddings (`input` or `inputs`) |
| GET  | `/prompts` · POST `/prompts` | `ai.manage_prompts` | List / create-update prompts |
| GET·DELETE | `/prompts/:name` | `ai.manage_prompts` | Get / delete a prompt |
| POST | `/prompts/:name/run` | `ai.use_inference` | Render `{{variables}}` and run |
| GET  | `/usage?hours=N` | `ai.view_usage` | Usage/cost by caller + model |

`GET /health` and `GET /ready` are unauthenticated probes.

### Examples

```bash
# Completion
curl -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"prompt":"Summarize: ...","maxTokens":256}' \
  localhost:3007/api/v1/completions

# Register a reusable prompt, then run it
curl -H "Authorization: Bearer $TOKEN" -d '{
  "name":"invoice.summary",
  "system":"You are a finance assistant.",
  "template":"Summarize invoice {{id}} for vendor {{vendor}}."
}' localhost:3007/api/v1/prompts

curl -H "Authorization: Bearer $TOKEN" -d '{
  "variables":{"id":"INV-42","vendor":"Acme"}
}' localhost:3007/api/v1/prompts/invoice.summary/run
```

## Run locally

```bash
cp .env.example .env      # leave ANTHROPIC_API_KEY blank to use the stub
go run ./cmd/server
```

## Config

See [`.env.example`](.env.example). Key knobs: `ANTHROPIC_API_KEY` (real vs stub),
`AI_DEFAULT_MODEL` (default `claude-sonnet-4-6`), `AI_MAX_OUTPUT_TOKENS`,
`AI_EMBEDDING_DIM`.

Registry: [`subrepos.json`](../../subrepos.json)
