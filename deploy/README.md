# attune — docker-compose deploy

One-command **attune + Postgres** stack for self-hosting. Requires Docker with
the Compose plugin (`docker compose version`).

> Full tutorial: [`docs/private-deploy.md`](../docs/private-deploy.md) — includes monitoring, SSL, upgrades, backup, troubleshooting, and per-tenant module configuration.

## 1. Configure

```bash
cd deploy
cp .env.example .env
```

Edit `.env` and set **at least**:

- `POSTGRES_PASSWORD` — a strong password (left empty in the example so a
  misconfig fails loudly).

Then edit `config.yaml`:

- put the same password into `database.url`;
- set `console.base_url`, `console.session_key`, and `console.bootstrap_admin`;
- generate a fresh Tink keyset and paste it into `secrets.tink_keyset`:
  ```bash
  docker compose run --rm attune secrets generate-keyset
  ```

## 2. Start

```bash
docker compose up -d
```

`attune` waits for Postgres to be healthy, then **runs migrations on startup**.
Check it came up:

```bash
docker compose logs attune        # expect: ...OK,path:/app/config.yaml,port:8090
curl http://localhost:8090/healthz   # -> ok
```

## 3. Create tenant, API key, and LLM route

The admin subcommands do **not** run migrations, so run them only **after** the
server above is up (it migrates on boot):

```bash
docker compose run --rm attune tenant create --slug acme --name "Acme"
docker compose run --rm attune keys issue --tenant acme --label main
docker compose run --rm attune llm channels create \
  --name openai --protocol openai-compat --base-url https://api.openai.com \
  --api-key sk-...
docker compose run --rm attune llm channels test \
  --id <channel-id> --provider-model gpt-4o-mini
docker compose run --rm attune llm abilities upsert \
  --channel <channel-id> --logical-model enrich-default --provider-model gpt-4o-mini
docker compose run --rm attune llm routes upsert \
  --purpose enrich --logical-model enrich-default
```

Send feedback with the printed key:

```bash
curl -X POST http://localhost:8090/v1/feedback/ingest \
  -H "X-API-Key: <key>" -H "Content-Type: application/json" \
  -d '{"content":"hello"}'
```

## 4. Observability (optional overlay)

Layer Prometheus + Grafana on top to see attune's metrics — zero manual setup:

```bash
docker compose -f docker-compose.yml -f docker-compose.obs.yml up -d
```

> **This is a reference / dev stack** — pinned images, single-node Prometheus
> (15 d / 2 GB retention), memory-capped, with built-in Prometheus rules but
> **no Alertmanager or HA**. For a real
> production setup, point your *existing* monitoring (a Prometheus / Grafana /
> VictoriaMetrics / Datadog running **separately from the app host**, with your
> own retention + alerting) at attune's `/metrics` — see the contract in
> [`../observability/README.md`](../observability/README.md). Don't run production
> monitoring on the same host as the app it's watching.

- **Grafana** → http://127.0.0.1:3000 — log in as `admin` with
  `GF_SECURITY_ADMIN_PASSWORD` (set it in `.env`). The "Attune Overview" dashboard
  is auto-loaded.
- **Prometheus** → http://127.0.0.1:9090 — `Status → Targets` shows the `attune`
  job UP.

Tear down with the **same** `-f` flags (or add `--remove-orphans`) so the obs
containers don't orphan:

```bash
docker compose -f docker-compose.yml -f docker-compose.obs.yml down
```

> **Exposure:** Grafana and Prometheus bind `127.0.0.1` by default; Prometheus
> exposes *all* your metrics unauthenticated. Front them with your proxy (or set
> `ATTUNE_BIND=0.0.0.0` only behind one). attune's own `/metrics` is likewise
> unauthenticated — keep `:8090` off the public internet.
>
> The `attune_enrich_duration_seconds` panels stay empty until enrichment runs
> against a real (or mock/ollama) LLM channel configured with `attune llm`.

attune exposes standard Prometheus/OpenMetrics — to use VictoriaMetrics, the
OpenTelemetry Collector, or another backend instead, point it at `/metrics` (see
[`../observability/README.md`](../observability/README.md)).

## Operations

- **Data** lives in the `attune-pg` volume; it survives `docker compose down`.
  `docker compose down -v` wipes it.
- **Backup / restore:**
  ```bash
  docker compose exec postgres pg_dump -U attune attune > backup.sql
  cat backup.sql | docker compose exec -T postgres psql -U attune attune
  ```
- **Tink key rotation:** generate the next keyset JSON with `attune secrets
  add-key`, roll it to every replica, switch primary with `set-primary`, run
  `reencrypt --apply`, retire the old DB key metadata with `retire-key --apply`,
  then remove the old key from YAML with `delete-key`. The full runbook is in
  [`../docs/private-deploy.md`](../docs/private-deploy.md#rotating-the-tink-keyset).
- **Exposure:** the host port binds `127.0.0.1` by default. Front attune with
  your own TLS reverse proxy; set `ATTUNE_BIND=0.0.0.0` only behind one.
- **Pinning:** `:latest` moves. Pin `ATTUNE_IMAGE` to a version or, best, a
  `@sha256:` digest for reproducible deploys.
- **No published image yet?** Build from source: `docker compose up -d --build`
  (uncomment the `build:` block in `docker-compose.yml`).
