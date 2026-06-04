# Listen

> **Feedback ingest + AI triage + smart fan-out**
>
> One POST in. AI 30-second classification. Auto-route to Lark / Slack / Jira.

Open-source service for collecting user feedback, classifying it with an LLM, and pushing the right rows to the right channel.

## Status

- **License**: Apache-2.0
- **Backend**: Go 1.25, single binary
- **Console**: React (in `console/` subdirectory)

## Architecture

| Layer | Tech |
|---|---|
| HTTP server | Go 1.25, chi router, structured slog |
| Storage | PostgreSQL 14+ |
| LLM enrichment | Any OpenAI-compatible endpoint (OpenAI / Azure / vllm / ollama / oneapi) |
| Outbound | Lark group bot webhooks + customer HTTPS webhooks |
| Console | React SPA (built separately, served as static files via nginx) |

### Package layout

```
cmd/listen/                Bootstrap: DI + signals + CLI subcommands
internal/
  domain/                  Pure types: IngestInput / Snapshot / Enriched
  repo/                    Data access — all SQL lives here
  service/                 Business logic + orchestration
  handlers/                HTTP routes + parameter parsing
  notify/                  Outbound webhooks + transport
  infra/
    apikey/                HTTP middleware + context keys
    config/                YAML + env override
    database/              Schema migrations
    llmclient/             OpenAI-compatible HTTP client
    lark/                  Inbound Lark protocol (signing + decode)
  observability/           Vendored OTel + slog helpers
console/                   Stage B web console (React + Vite + biome)
```

**Layering rule**: handlers never touch SQL; service never writes HTTP; notify never imports service; infra never imports service or repo.

## Quickstart (dev)

```bash
go build ./cmd/listen
go run ./cmd/listen server                                    # Start HTTP server
go run ./cmd/listen keys issue --tenant <slug> --label <s>    # Mint an API key
```

Required env / yaml: see [`config.example.yaml`](config.example.yaml).

### Quality gates

```bash
./scripts/check.sh    # build + vet + test + lizard + jscpd
```

| Check | Threshold |
|---|---|
| `go build ./...` | No errors |
| `go vet ./...` | No warnings |
| `go test ./...` | All pass; new code must have tests |
| `lizard . -l go -C 10 -T nloc=100` | CCN ≤ 15, NLOC ≤ 100 per function |
| `npx -y jscpd . --pattern '**/*.go' --threshold 5` | Duplication < 2% |
| Single file size | ≤ 300 lines |

## Docker

```bash
docker build -t listen:local .
docker run -p 8090:8090 \
  -e FEEDBACK_API_DATABASE_URL=postgres://user:pass@host:5432/listen \
  -e FEEDBACK_API_LLM_OPENAI_API_KEY=sk-... \
  listen:local
```

Production-ready `docker-compose.yml` with PostgreSQL + Grafana coming in `deploy/` (next phase).

## Configuration

All config lives in `config.yaml` (path overridable via `FEEDBACK_API_CONFIG`). Every field has an env var override too — see [`internal/infra/config/env.go`](internal/infra/config/env.go) for the full table. Minimal required set:

| Field | Env var | Notes |
|---|---|---|
| `database_url` | `FEEDBACK_API_DATABASE_URL` | Postgres connection string |
| `llm_openai_base_url` | `FEEDBACK_API_LLM_OPENAI_BASE_URL` | OpenAI-compatible endpoint |
| `llm_openai_api_key` | `FEEDBACK_API_LLM_OPENAI_API_KEY` | Bearer token (empty OK for local ollama / vllm) |

## License

Apache-2.0 — see [LICENSE](LICENSE).
