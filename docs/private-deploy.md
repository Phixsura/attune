# Private Deployment Quickstart

A 5--15 minute guide to running attune on your own infrastructure. By the end
you will have attune + Postgres serving feedback ingestion on `localhost:8090`,
with optional Prometheus/Grafana monitoring.

---

## Prerequisites

- **Docker 24+** with the Compose v2 plugin (`docker compose version`).
- An **OpenAI-compatible LLM endpoint** — any of:
  - OpenAI API (`https://api.openai.com`)
  - Azure OpenAI
  - Local **ollama** (`http://localhost:11434`)
  - Local **vLLM** (`http://localhost:8000`)
  - oneapi / LiteLLM proxy / any `/v1/chat/completions`-compatible gateway
- (Optional) An Anthropic or Gemini API key — attune supports four LLM
  protocols; see [LLM protocol](#choosing-an-llm-protocol) below.

No Go toolchain is needed — the docker-compose kit ships a pre-built
distroless image.

---

## Step 1 -- Clone

```bash
git clone https://github.com/Phixsura/attune && cd attune
```

---

## Step 2 -- Configure

```bash
cd deploy
cp .env.example .env
```

Edit `.env` and set **at minimum** these three values:

| Variable | What to set |
|---|---|
| `POSTGRES_PASSWORD` | A strong password. Left empty in the example so a misconfigured first boot fails loudly. |
| `FEEDBACK_API_DATABASE_URL` | Paste the **same** password into the DSN: `<your-postgres-connection-string>` |
| `FEEDBACK_API_LLM_OPENAI_API_KEY` | Your OpenAI / compatible API key. The stack boots and `/healthz` passes without it, but enrichment will 401 until it is real. |

All other variables are optional and have sensible defaults. See the annotated
`.env.example` for the full list.

> **Secrets stay in `.env`** — it is gitignored. Never commit real keys to
> `config.yaml` or any tracked file.

---

## Step 3 -- Start

```bash
docker compose up -d
```

attune waits for Postgres to become healthy, then **runs schema migrations
automatically on startup**. The first boot creates all tables; subsequent boots
apply incremental migrations.

---

## Verification

Confirm everything is running:

```bash
# 1. Health check
curl http://localhost:8090/healthz
# -> ok

# 2. Server logs
docker compose logs attune
# expect: ...OK,port:8090,console_enabled:false,lark_enabled:false
```

### Create your first tenant and API key

```bash
docker compose run --rm attune tenant create --slug acme --name "Acme Corp"
docker compose run --rm attune keys issue --tenant acme --label main
```

The `keys issue` command prints an API key. Use it to send your first feedback:

```bash
curl -X POST http://localhost:8090/v1/feedback/ingest \
  -H "X-API-Key: <your-key>" \
  -H "Content-Type: application/json" \
  -d '{"content":"The checkout page crashes when I add more than 10 items to the cart."}'
```

Check the row landed:

```bash
docker compose exec postgres psql -U attune attune \
  -c "SELECT id, source, LEFT(content, 60) AS content FROM user_feedback ORDER BY id DESC LIMIT 5;"
```

---

## Choosing an LLM protocol

attune supports four LLM backends. The default (`openai-compat`) works with any
OpenAI-compatible endpoint. Set `FEEDBACK_API_LLM_PROTOCOL` in `.env` to switch:

| Protocol | Env value | What it calls | Key required |
|---|---|---|---|
| OpenAI Chat Completions | `openai-compat` (default) | Any `/v1/chat/completions` endpoint | `FEEDBACK_API_LLM_OPENAI_API_KEY` (blank OK for local ollama) |
| OpenAI Responses | `openai-responses` | OpenAI Responses API (official SDK) | Yes |
| Anthropic Messages | `anthropic` | Anthropic Messages API (official SDK) | Yes |
| Google Gemini | `gemini` | Gemini GenerateContent (official SDK) | Yes |

Example -- switching to Anthropic:

```bash
# in deploy/.env
FEEDBACK_API_LLM_PROTOCOL=anthropic
FEEDBACK_API_LLM_OPENAI_API_KEY=sk-ant-...
```

> The env var name `FEEDBACK_API_LLM_OPENAI_API_KEY` is kept across all
> protocols for backward compatibility. For SDK-backed protocols the base URL
> field (`FEEDBACK_API_LLM_OPENAI_BASE_URL`) is ignored -- the SDK uses the
> vendor's default host.

### Using local ollama

```bash
# in deploy/.env
FEEDBACK_API_LLM_OPENAI_BASE_URL=http://host.docker.internal:11434
FEEDBACK_API_LLM_OPENAI_API_KEY=
```

On Linux, add `extra_hosts` to the attune service in `docker-compose.yml`:

```yaml
    extra_hosts:
      - "host.docker.internal:host-gateway"
```

---

## Configuring per-tenant modules

By default the enricher lets the LLM invent module labels freely (free-form
mode). For production use you will want to **declare a fixed module vocabulary
per tenant** so that downstream aggregation (weekly digest top-modules,
per-module filtering, notification cards) is reliable.

### Via the Console UI (recommended)

Open the console at `/settings`. You will see:

1. **Prompt template** -- the classification prompt sent to the LLM. Edit it
   to match your product's language and domain. Use the `{{content}}` token
   where the user's feedback text should appear, and `{{modules}}` where the
   allowed-module list should appear. Click **Restore default** to reset.
2. **Module whitelist** -- add your module names one by one (e.g. `cart`,
   `checkout`, `shipping`). When non-empty, the enricher guarantees that
   output modules are a subset of this list (canonical spelling).
3. **Preview** -- enter a sample feedback string and click the preview button
   to see the exact prompt that will be sent to the LLM.

### Via the API

```bash
# Read current config
curl http://localhost:8090/fb/v1/console/enrich-config \
  -H "Cookie: session=<cookie>"

# Update modules + prompt
curl -X PUT http://localhost:8090/fb/v1/console/enrich-config \
  -H "Cookie: session=<cookie>" \
  -H "Content-Type: application/json" \
  -d '{"modules":["cart","checkout","shipping","payments","account"]}'

# Preview rendered prompt
curl -X POST http://localhost:8090/fb/v1/console/enrich-config/preview \
  -H "Cookie: session=<cookie>" \
  -H "Content-Type: application/json" \
  -d '{"sample_content":"The cart page is slow when adding 10+ items"}'
```

### How enforcement works

Module whitelist enforcement uses three layers:

1. **Prompt guidance** -- the rendered prompt names the allowed modules.
2. **Structured output** -- the LLM request carries a JSON Schema that pins
   `modules` to an enum of the allowed list (provider-dependent).
3. **Post-parse filter** -- after the LLM responds, off-list modules are
   dropped and replaced with their canonical spelling. This is always on,
   regardless of whether the provider honored the schema.

Modules that the LLM emitted but that are not on the whitelist are recorded as
a **suggested-module** signal (metric `attune_enrich_suggested_modules_total`
and a structured log line), so you can review them offline and decide whether
to add them to the whitelist.

---

## Optional monitoring

Layer Prometheus + Grafana on top to see attune's metrics with zero manual
setup:

```bash
docker compose -f docker-compose.yml -f docker-compose.obs.yml up -d
```

- **Grafana** -- http://127.0.0.1:3000 -- log in as `admin` with the password
  you set in `GF_SECURITY_ADMIN_PASSWORD`. The "Attune Overview" dashboard is
  auto-provisioned.
- **Prometheus** -- http://127.0.0.1:9090 -- `Status > Targets` shows the
  `attune` job as UP.

Key metrics to watch:

| Metric | Type | Labels | What it tells you |
|---|---|---|---|
| `attune_ingest_total` | counter | `tenant`, `source`, `result` | Ingest request volume and success rate |
| `attune_enrich_duration_seconds` | histogram | `tenant`, `module_mode`, `result` | End-to-end AI enrichment latency |
| `attune_enrich_modules_dropped_total` | counter | `tenant` | Modules removed by whitelist filter |
| `attune_enrich_suggested_modules_total` | counter | `tenant` | Off-list modules the LLM tried to emit |

Tear down with the **same** `-f` flags to avoid orphaning obs containers:

```bash
docker compose -f docker-compose.yml -f docker-compose.obs.yml down
```

> This is a **reference / dev stack** -- pinned images, single-node Prometheus
> (15 d / 2 GB retention), memory-capped, no alerting or HA. For production,
> point your existing monitoring at attune's `/metrics` endpoint (see
> [`../observability/README.md`](../observability/README.md)).

---

## Optional SSL

attune binds `127.0.0.1:8090` by default (loopback only). To expose it over
HTTPS, front it with a reverse proxy. A minimal nginx configuration:

```nginx
server {
    listen 443 ssl http2;
    server_name attune.example.com;

    ssl_certificate     /etc/letsencrypt/live/attune.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/attune.example.com/privkey.pem;

    location / {
        proxy_pass http://127.0.0.1:8090;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

Set `ATTUNE_BIND=0.0.0.0` in `.env` **only** behind your TLS proxy. For
certificate management, [Let's Encrypt](https://letsencrypt.org/getting-started/)
with certbot is the simplest path.

---

## Upgrades

When using the `:latest` tag (default):

```bash
docker compose pull
docker compose up -d
```

attune runs migrations on startup, so pulling a new image and restarting is
all that is needed. For production, pin to a specific version or digest:

```bash
# in .env
ATTUNE_IMAGE=ghcr.io/phixsura/attune:v0.2.0
# or strongest pin:
# ATTUNE_IMAGE=ghcr.io/phixsura/attune@sha256:<digest>
```

---

## Backup and restore

Data lives in the `attune-pg` Docker volume and survives `docker compose down`.
(`docker compose down -v` wipes it.)

**Backup:**

```bash
docker compose exec postgres pg_dump -U attune attune > backup-$(date +%Y%m%d).sql
```

**Restore:**

```bash
cat backup.sql | docker compose exec -T postgres psql -U attune attune
```

For automated backups, schedule the `pg_dump` command via cron or your
preferred backup tool.

---

## Troubleshooting

### LLM returns 401 / rate limit / unknown model

**Symptoms:** enrichment never completes; logs show `llm_err` or HTTP 401/429.

**Fix:**
- Verify `FEEDBACK_API_LLM_OPENAI_API_KEY` is set and valid:
  `docker compose logs attune | grep api_key_set` should show `api_key_set:true`.
- For `openai-compat`, confirm `FEEDBACK_API_LLM_OPENAI_BASE_URL` points to
  a reachable endpoint. Try: `curl <base_url>/v1/models`.
- Check rate limits with your LLM provider.

### Lark signature mismatch

**Symptoms:** Lark event verification fails; logs show signature errors.

**Fix:**
- Copy the exact signing secret from the Lark Open Platform "Event
  Subscription" tab into `FEEDBACK_API_LARK_SIGNING_SECRET`.
- Ensure `FEEDBACK_API_LARK_DEFAULT_TENANT_SLUG` matches a tenant that already
  exists (`docker compose run --rm attune tenant create --slug <slug> --name
  "<name>"`).

### Port 8090 already in use

**Symptoms:** `docker compose up` fails with `address already in use`.

**Fix:**

```bash
# in .env
ATTUNE_PORT=8091
```

Then access at `http://localhost:8091`. Remember to update any downstream proxy
config to match.

### Postgres password contains `@` or `:` and connection fails

**Symptoms:** attune fails to start; logs show Postgres connection errors.

**Fix:** URL-encode special characters in `FEEDBACK_API_DATABASE_URL`. For
example, `p@ss:word` becomes `p%40ss%3Aword`:

```bash
<!-- trufflehog:ignore -->
FEEDBACK_API_DATABASE_URL=<your-postgres-connection-string>
<!-- /trufflehog:ignore -->
```

### China-region servers timing out on image pulls

**Symptoms:** `docker compose up` hangs on `Pulling attune ...`.

**Fix:** Use a registry mirror. Add to your Docker daemon config
(`/etc/docker/daemon.json`):

```json
{
  "registry-mirrors": ["https://mirror.ccs.tencentyun.com"]
}
```

Or pull manually from a mirror and retag:

```bash
docker pull <mirror>/attune:v0.2.0
docker tag <mirror>/attune:v0.2.0 ghcr.io/phixsura/attune:latest
```

### Enrichment output modules are unstable / inconsistent

**Symptoms:** the same feedback produces different module labels across runs.

**Fix:** Configure a per-tenant module whitelist (see [Configuring per-tenant
modules](#configuring-per-tenant-modules) above). This constrains the LLM to a
fixed vocabulary and enforces it with a post-parse filter.

---

## References

- [`deploy/README.md`](../deploy/README.md) -- compose kit quick-reference.
- [`config.example.yaml`](../config.example.yaml) -- full annotated config
  reference with env-var overrides.
- [`observability/README.md`](../observability/README.md) -- metric contract
  and dashboard details.
- [CLAUDE.md](../CLAUDE.md) -- engineering guidelines (for contributors).
