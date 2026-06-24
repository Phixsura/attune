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

## What counts as a reasonable private deployment

Private deployment is reasonable only when the operator can own the runtime
risks around data, identity, upgrades, and observability. Use this guide for a
fast single-node install, but treat it as the first rung of a deployment ladder,
not the whole production story.

Deployment tiers:

- **Evaluation**: local trial, demo data, or short-lived pilot. Docker Compose,
  bundled Postgres, and loopback-only HTTP are acceptable.
- **Small private install**: low-risk internal workflow with manual operations.
  Use Compose or one VM, encrypted config, scheduled backups, and a TLS proxy.
- **Production private install**: customer data, multiple teams, SSO, or uptime
  expectations. Use Kubernetes/Helm, external Postgres with PITR, managed
  secrets, monitoring, and a rollback plan.

Non-negotiable production gates:

- **Secrets are operator-owned.** Real `config.yaml`, database URLs, Tink
  keysets, session keys, OIDC client secrets, bootstrap credentials, and
  provider keys stay outside git. In Kubernetes, use `config.existingSecret`;
  do not put production secrets in Helm values.
- **Postgres is durable and recoverable.** Use external PostgreSQL with
  backups, point-in-time recovery, restore drills, and pgvector 0.5.0+.
  The embedded Postgres container is for evaluation and low-risk installs.
- **The Tink keyset is backed up with the database.** Losing the keyset means
  encrypted inbound credentials, webhook secrets, and managed LLM API keys are
  unrecoverable even if the database backup exists.
- **The public entrypoint is TLS-fronted.** Expose Attune through a reverse
  proxy or ingress with HTTPS. Bind directly to `0.0.0.0` only behind that
  proxy.
- **Identity is deliberate.** Use Console bootstrap only for first admin
  creation, then remove bootstrap credentials. For production, prefer OIDC/SSO
  with controlled allowed groups.
- **Upgrades are rehearsed.** Pin image tags or digests, read the changelog,
  take backups before destructive migrations, and test rollback on staging.
- **Observability exists before traffic.** Scrape `/metrics`, collect logs,
  alert on readiness failures, DB errors, queue lag, and enrichment failures.
- **Shutdown and rollout are graceful.** Kubernetes installs should use
  `/readyz`, at least two replicas, PDB, `maxUnavailable: 0`, and the graceful
  shutdown defaults described in
  [`docs/k8s-deploy.md`](k8s-deploy.md#graceful-shutdown).

Before putting real customer data through a private install, run this acceptance
check:

```bash
curl -fsS http://127.0.0.1:8090/healthz
curl -fsS http://127.0.0.1:8090/readyz
docker compose logs attune --tail=200
docker compose exec -T postgres psql -U attune attune <<'SQL'
CREATE EXTENSION IF NOT EXISTS vector;
SELECT extversion FROM pg_extension WHERE extname = 'vector';
SQL
docker compose exec postgres pg_dump -U attune attune \
  >/tmp/attune-backup-smoke.sql
test -s /tmp/attune-backup-smoke.sql
```

For production Kubernetes, use the Helm path in
[`docs/k8s-deploy.md`](k8s-deploy.md). It has chart-level fail-fast validation
for production secrets, external Postgres, replica counts, HPA resources, PDB
settings, NetworkPolicy rules, DNS smoke tests, rolling deploys, and
blue/green traffic switching.

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

Edit `.env` and set:

| Variable | What to set |
|---|---|
| `POSTGRES_PASSWORD` | A strong password, also pasted into `database.url` in `config.yaml`. |

Then edit `config.yaml` and set `database.url`, `console.*`,
`secrets.tink_keyset`, and the audit retention block. Generate the keyset with:

```bash
docker compose run --rm attune secrets generate-keyset
```

> ⚠️ **Treat this Tink keyset like a database password.** Lose it, and every
> stored inbound webhook secret, IMAP credential, and managed LLM API key
> becomes unrecoverable. Back it up to the same secret manager you use for
> `POSTGRES_PASSWORD`.

> **The real `config.yaml` is private**. Never commit real DB URLs, bootstrap
> credentials, provider keys, or Tink keysets.

Recommended audit settings:

```yaml
audit:
  retention_days: 365
  prune_interval: 1h
```

- `audit.retention_days` defines how long immutable Console audit rows are kept.
- `audit.prune_interval` controls how often the background pruner removes expired rows.
- CSV audit exports contain actor metadata and sanitized before/after payloads; treat them like internal security records and avoid sharing them through public channels.

If this deployment already has inbound sources encrypted by the old
`ATTUNE_INBOUND_MASTER_KEY`, paste that old 32-byte key as hex/base64 into
`secrets.legacy_inbound_master_key` before the first rollout. After startup,
run `attune secrets reencrypt --apply` so old inbound envelopes are rewritten
with Tink, then remove `legacy_inbound_master_key` from every replica config.

---

## Step 3 -- Start

```bash
docker compose up -d
```

attune waits for Postgres to become healthy, then **runs schema migrations
automatically on startup**. The first boot creates all tables; subsequent boots
apply incremental migrations.

The production image serves the built Console SPA under `/console/*`, so the
first login page is `http://localhost:8090/console/login` for the default
compose stack.

---

## Verification

Confirm everything is running:

```bash
# 1. Health check
curl http://localhost:8090/healthz
# -> ok

# 2. Server logs
docker compose logs attune
# expect: ...server started,port:8090,console_enabled:true,inbound_adapters:[email webhook]
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

attune supports four DB-managed LLM channel protocols. Configure them after the
server has run migrations. For day-to-day operation, sign in to the Console and
use `/console/llm-config`; the CLI below is the scriptable equivalent.

| Protocol | What it calls | Key required |
|---|---|---|
| `openai-compat` | Any `/v1/chat/completions` endpoint | Bearer key, unless `auth_mode=none` |
| `openai-responses` | OpenAI Responses API (official SDK) | Yes |
| `anthropic` | Anthropic Messages API (official SDK) | Yes |
| `gemini` | Gemini GenerateContent (official SDK) | Yes |

Example -- Anthropic:

```bash
docker compose run --rm attune llm channels create \
  --name anthropic --protocol anthropic --api-key sk-ant-...
docker compose run --rm attune llm channels test \
  --id <channel-id> --provider-model claude-sonnet-4-5
docker compose run --rm attune llm abilities upsert \
  --channel <channel-id> --logical-model enrich-default --provider-model claude-sonnet-4-5
docker compose run --rm attune llm routes upsert \
  --purpose enrich --logical-model enrich-default
```

### Using local ollama

```bash
docker compose run --rm attune llm channels create \
  --name ollama --protocol openai-compat --base-url http://host.docker.internal:11434 \
  --auth-mode none
docker compose run --rm attune llm channels test \
  --id <channel-id> --provider-model llama3.1
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

## Embedding Clustering

Attune can automatically group semantically similar feedback using vector
embeddings. When 50 users report "checkout button doesn't work", they become
one cluster instead of 50 separate rows.

### Prerequisites

- **pgvector 0.5.0+** — the Postgres image in `docker-compose.yml` includes
  pgvector. If you use an external Postgres, ensure the `vector` extension is
  installed (`CREATE EXTENSION IF NOT EXISTS vector;`).

### Enabling clustering

Clustering is **off by default**. Enable it per tenant via SQL:

```bash
docker compose exec postgres psql -U attune attune -c \
  "UPDATE tenants SET clustering_enabled = TRUE WHERE slug = 'acme';"
```

Optional: adjust the similarity threshold (default 0.85, range 0.0–1.0):

```bash
docker compose exec postgres psql -U attune attune -c \
  "UPDATE tenants SET clustering_threshold = 0.80 WHERE slug = 'acme';"
```

### How it works

1. After enrichment completes, a task is queued in `embedding_task`.
2. A background worker calls the configured embedding model (via managed LLM
   channels with `purpose = 'embed'`) to generate a 256-dim vector.
3. The worker searches for existing feedback with cosine similarity above the
   threshold. If found, the new feedback joins that cluster; otherwise a new
   cluster is created.
4. Clusters with 3+ members get an LLM-generated label.

### Backfilling existing feedback

To embed feedback that was ingested before clustering was enabled:

```bash
docker compose run --rm attune embed backfill --tenant acme
```

Check queue depth and progress:

```bash
docker compose run --rm attune embed status --tenant acme
```

### Metrics

| Metric | Description |
|---|---|
| `attune_embed_cluster_assignments_total` | Feedback assigned to clusters (new or existing) |
| `attune_embed_errors_total` | Embedding failures |
| `attune_embed_duration_seconds` | Embedding latency histogram |
| `attune_embed_queue_depth` | Pending tasks in the embedding queue |

---

## Daily Digest

Instead of a webhook per enriched row, a tenant can receive one **daily roll-up**
of yesterday's feedback with LLM-labeled top themes (#27).

### Set up the delivery target

The digest is delivered to the tenant's **`raw-webhook` notify target whose
`audience` is `digest`** — a routing filter that keeps the digest target out of
per-event traffic. Create it in Console (Settings → Notify Targets) or via the API with
`audience = digest`; the digest worker addresses it by
`(tenant, raw-webhook, digest)`.

### Configure the schedule

In Console (Settings → Daily Digest) or via `PUT /fb/v1/console/digest-subscription`,
set `enabled`, `frequency` (`daily` / `weekly` + `byweekday`), the tenant-local
`send_hour`, the `llm_min_feedback` theme threshold, and `send_on_empty`. The
worker fires at most once per tenant per local day (timezone- and DST-aware),
aggregates yesterday's enriched feedback, and POSTs the rendered digest to the
target above. Themes reuse the embedding clusters when clustering is enabled;
otherwise a single LLM call names them. Volume tiers the output: 0 rows skip
(unless `send_on_empty`), 1–5 send a themeless list, and `≥ llm_min_feedback`
send LLM-named themes.

### Metrics

| Metric | Description |
|---|---|
| `attune_digest_runs_total` | Digest runs by outcome (`sent` / `skipped_empty` / `failed`) |
| `attune_digest_duration_seconds` | Digest aggregation + delivery latency histogram |

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
> (15 d / 2 GB retention), memory-capped, with built-in Prometheus rules but no
> Alertmanager or HA. For production, point your existing monitoring at attune's
> `/metrics` endpoint (see
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

Set the compose port-mapping variable `ATTUNE_BIND=0.0.0.0` in `.env` **only**
behind your TLS proxy. This changes Docker's host bind address, not Attune's
runtime config. For certificate management,
[Let's Encrypt](https://letsencrypt.org/getting-started/) with certbot is the
simplest path.

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

### Upgrading to v0.3 — channel-agnostic inbound (#66)

v0.3 ships the channel-agnostic inbound framework and **integrally removes
Lark** from the codebase. The migration is destructive — `user_feedback` rows
where `source LIKE 'lark-%'`, every `tenant_lark_install` row, and the
`tenants.lark_install` / `tenants.lark_tenant_key` / `tenant_users.lark_open_id`
columns are hard-deleted. Pre-1.0 carries no retention guarantee.

**Preflight runbook:**

1. **Inventory.** Count what you'll lose:
   ```bash
   docker compose exec postgres psql -U attune attune -c \
     "SELECT count(*) FROM user_feedback WHERE source LIKE 'lark-%';"
   docker compose exec postgres psql -U attune attune -c \
     "SELECT slug FROM tenants WHERE lark_install IS NOT NULL;"
   ```
2. **Export if needed.**
   ```bash
   docker compose exec postgres pg_dump -U attune attune \
     --table=user_feedback --table=tenant_lark_install \
     --table=lark_install > pre-v0.3-lark-snapshot.sql
   ```
3. **Generate** `secrets.tink_keyset` with `attune secrets generate-keyset`
   and back up the config alongside DB credentials.
4. **Set** `console.bootstrap_admin` in `config.yaml` for the first start.
5. **Opt in** to the Lark delete by setting `migrations.confirm_lark_delete: true`
   in `config.yaml`. Without it, startup hard-fails when lark-typed rows exist —
   a deliberate guard against silent loss.
6. **Pull and start.**
   ```bash
   docker compose pull && docker compose up -d
   ```
   Watch the logs for `ConfirmLarkDelete OK` → `migrations 015..017 applied`
   → `inbound_adapters:[email webhook]`.
7. **Sign in** at `http://localhost:8090/console/login` with the bootstrap
   credentials, change the password, then clear `console.bootstrap_admin`
   and reset `migrations.confirm_lark_delete` if desired.
8. **Re-onboard** any feedback that used to flow via Lark — the console's
   `Inbound Sources` page (route `/console/inbound-sources`) is where you
   create webhook + email sources. The webhook adapter speaks Stripe-style
   HMAC; the email adapter polls IMAP.

If a step fails, the migration is wrapped in a single transaction — Postgres
rolls back and the image keeps the old schema. Diagnose, fix, retry.

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

### Restore drills (#151)

A backup you have never restored is not a backup. attune ships a drill that
proves a *restored* database is actually recoverable — schema/migration state,
row counts, the pgvector extension, and, critically, that the managed
Tink-encrypted secrets in it (LLM credentials, webhook signing secrets, inbound
IMAP passwords) can still be decrypted with your live keyset. The drill never
sends production traffic; it verifies a throwaway restored database and never
logs decrypted plaintext.

**1. Restore the backup into a throwaway database** (never production):

```bash
createdb -U attune attune_restore_test
pg_dump -U attune attune | psql -U attune attune_restore_test   # or restore a real backup file
```

**2. Run the drill** against the restored database. The drill reads your live
keyset and production DB URL from `--config`. The example DSNs below omit the
password — supply it via `PGPASSWORD` or `~/.pgpass`:

```bash
attune --config ./config.yaml restore-drill run \
  --target  "postgres://attune@localhost:5432/attune_restore_test?sslmode=disable" \
  --baseline-url "postgres://attune@localhost:5432/attune?sslmode=disable" \
  --backup-ref "backup-$(date +%Y%m%d)" \
  --backup-taken-at "2026-06-24T02:00:00Z" \
  --restore-duration 4m12s \
  --rpo-target 24h --rto-target 30m \
  --record
```

`--backup-taken-at` (when the backup was taken) yields the **RPO** (data-loss
window); `--restore-duration` (measured by your restore step) is the **RTO**.
Both are graded against `--rpo-target` / `--rto-target` by the
`recovery_objectives` check, which warns — not fails — when a target is breached
(the data is still recoverable, just outside SLA). They are persisted for audit
and trended by `attune restore-drill history`.

```
attune restore drill — recoverability report

  ✓ connectivity     connected to the restored database
  ✓ schema           24/24 migrations applied, checksums + manifest OK
  ✓ pgvector         pgvector 0.8.3 present; sample similarity query OK
  ✓ row_counts       restored counts within RPO band of live across 4 table(s)
  ✓ decryptability   5/5 managed secret samples decrypted with the live keyset

Overall: PASS  (verify 4200ms)
```

The command exits non-zero if the drill **fails** (e.g. a `decryptability`
failure means the restored database and your keyset have drifted — restore the
keyset that was active when those rows were written). `--record` appends the
full JSON report to the `restore_drill_runs` table in production; that report is
your audit evidence. Retrieve the latest with `attune restore-drill status` or
view it on the Console **System Readiness** page (the `backup:restore_drill`
check, which warns if no drill has run in the last 7 days), and via
`attune doctor`.

The drill runs a 12-check battery (connectivity, schema, pgvector, row counts,
decryptability, constraints, sequences, encoding, materialized views,
extensions, recovery objectives). Add `--deep` for an extra structural tier
(index validity + amcheck B-Tree verification; slower). To let attune perform
the restore itself instead of pointing at a pre-restored DB, use the
**push-button** mode:

```bash
attune --config ./config.yaml restore-drill run \
  --restore-from /backups/attune-$(date +%Y%m%d).sql \
  --admin-url "postgres://attune@localhost:5432/postgres?sslmode=disable" \
  --rpo-target 24h --rto-target 30m --record
```

It provisions an ephemeral database, restores the backup (`psql` for plain SQL,
`pg_restore` for custom/dir/tar), **auto-measures the RTO**, runs the battery,
and drops the ephemeral DB. To check a `pg_basebackup` artifact's integrity
before restoring, run `attune restore-drill verify-backup <dir>`
(`pg_verifybackup`). These modes need `psql` / `pg_restore` / `pg_verifybackup`
in the runtime.

**3. Tear down** the throwaway database: `dropdb -U attune attune_restore_test`.

> The keyset is *not* in the database — keep it backed up alongside it. A
> database backup without the matching Tink keyset cannot decrypt any managed
> secret. See "Rotating the Tink keyset" below.

For Kubernetes, the Helm chart ships an opt-in scheduled CronJob
(`restoreDrill.enabled=true`) that runs the drill against a restored target you
provide — for example a CloudNativePG recovery `Cluster`, which bootstraps a
fresh isolated cluster from a base backup + WAL by construction:

```yaml
restoreDrill:
  enabled: true
  schedule: "0 2 * * 0"
  targetDatabaseURL:   "postgres://attune@attune-restore:5432/attune?sslmode=disable"
  baselineDatabaseURL: "postgres://attune@attune-postgres:5432/attune?sslmode=disable"
```

The shipped CronJob runs the **verify-only** mode (`--target`) — it verifies a
database you have already restored (e.g. a CloudNativePG recovery `Cluster`).
The push-button (`--restore-from`) and `verify-backup` modes need the PostgreSQL
client tools, which the default attune image does not bundle; to schedule those
in-cluster, run them from a custom Job built on an image that adds
`postgresql-client`.

---

## Rotating the Tink keyset

All replicas must share a decrypt-capable `secrets.tink_keyset`. Rotate it in
stages so old and new replicas can decrypt the same Postgres state throughout
the rollout.

1. Inspect the current key ids without printing key material:
   ```bash
   docker compose run --rm attune secrets keyset-info
   ```
2. Print a new keyset JSON that contains the old key plus a new enabled key.
   Paste the output into `secrets.tink_keyset` and roll it to every replica while
   the old key remains primary:
   ```bash
   docker compose run --rm attune secrets add-key
   ```
3. Make the new key primary, paste the output into `secrets.tink_keyset`, and
   roll every replica again:
   ```bash
   docker compose run --rm attune secrets set-primary --key-id <new-key-id>
   ```
4. Dry-run and then apply DB-wide re-encryption for old-key ciphertexts:
   ```bash
   docker compose run --rm attune secrets reencrypt --from-key-id <old-key-id>
   docker compose run --rm attune secrets reencrypt --from-key-id <old-key-id> --apply
   ```
5. Dry-run and then apply registry retirement. Apply refuses while any LLM or
   inbound secret still references the old key:
   ```bash
   docker compose run --rm attune secrets retire-key --key-id <old-key-id>
   docker compose run --rm attune secrets retire-key --key-id <old-key-id> --apply
   ```
6. Remove the old key from the local keyset JSON only after retirement succeeds,
   paste the output into `secrets.tink_keyset`, and roll every replica one final
   time:
   ```bash
   docker compose run --rm attune secrets delete-key --key-id <old-key-id>
   ```

`reencrypt` locks the affected secret rows in one transaction and rewrites
managed LLM channel credentials plus inbound source configs, including nested
webhook current/previous secrets and email passwords.

---

## Troubleshooting

### LLM returns 401 / rate limit / unknown model

**Symptoms:** enrichment never completes; logs show `llm_err` or HTTP 401/429.

**Fix:**
- Verify the DB-managed LLM channel has `has_api_key=true` unless it is a local
  `auth_mode=none` channel.
- For `openai-compat`, confirm the channel `base_url` points to a reachable
  endpoint. Try: `curl <base_url>/v1/models`.
- Check rate limits with your LLM provider.

### Inbound webhook returns 401

**Symptoms:** Customer POST to
`/v1/inbound/webhook/<tenant-slug>/<source-slug>` receives
`401 invalid signature`; logs show `signature_mismatch` or
`unknown_source`.

**Fix:**
- Confirm the source still exists and is **enabled** in the console
  (Inbound Sources page, route `/console/inbound-sources`). attune
  answers `401` on the same URL for any unknown slug (enumeration
  resistance), so a typo in the tenant slug or source slug looks
  identical to a bad signature.
- The webhook adapter expects Stripe-style headers:
  `X-Attune-Timestamp: <unix-seconds>` and
  `X-Attune-Signature: sha256=<hex-hmac-sha256("<ts>.<body>")>`.
  Replay window is ±300 s; sync the sender's clock to NTP.
- If you just rotated the secret (`POST .../rotate`), the **old**
  secret is accepted for the 24 h grace window. A **second** rotate
  inside the grace window is refused with
  `409 rotation_in_grace_window` — wait for the grace to expire (the
  response's `next_eligible_at` tells you when) and try again.

### Tink keyset was lost

**Symptoms:** startup rejects `secrets.tink_keyset`, or every encrypted runtime
secret fails to decrypt.

**Fix:** There is no recovery — every stored runtime secret was encrypted with
that keyset. You must:

1. Restore the keyset from the secret-manager backup you took at
   provisioning time, OR
2. Generate a fresh Tink keyset, then delete + recreate every inbound source
   and rotate every managed LLM channel API key. Existing `feedback` data is
   unaffected.

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

**Fix:** URL-encode special characters in `database.url` in `config.yaml`. For
example, `p@ss:word` becomes `p%40ss%3Aword`:

```bash
<!-- trufflehog:ignore -->
database:
  url: "<your-postgres-connection-string>"
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

## Migration management

attune applies SQL migrations automatically on startup. Starting with v0.9.0,
migrations include integrity tracking (checksums, execution metadata) for
enterprise-grade auditability.

### Pre-deploy verification

Before deploying, verify migration integrity:

```bash
attune migrations verify
```

This checks:
- No duplicate numeric prefixes
- All applied migrations match embedded files (checksum)
- No-transaction migrations are correctly formatted

### Status inspection

View current migration state:

```bash
attune migrations status                  # Full status
attune migrations status --format json    # Machine-readable
attune migrations status --pending        # Pending only
```

### Dry-run

Preview pending migrations without applying:

```bash
attune migrations dry-run
```

### Recovery procedures

#### Checksum drift

If startup fails with "migration checksum drift":

1. **Investigate.** Determine why the file changed:
   ```bash
   git log -p -- internal/infra/database/migrations/NNN_*.sql
   ```

2. **If the change was intentional and safe** (whitespace, comment):
   ```sql
   -- Calculate new checksum
   -- On Linux: sha256sum internal/infra/database/migrations/NNN_name.sql
   -- On macOS: shasum -a 256 internal/infra/database/migrations/NNN_name.sql

   UPDATE schema_migrations_feedback
   SET checksum = '<new-sha256-hex>'
   WHERE filename = 'NNN_name.sql';
   ```

3. **If the database state is correct but the file is wrong,** restore from git:
   ```bash
   git checkout <commit> -- internal/infra/database/migrations/NNN_name.sql
   ```

4. **If both are inconsistent,** restore from backup and reapply migrations.

#### Missing migration file

If startup fails with "migration applied but file missing":

1. The binary was built from a different source than what's in the database.
2. Restore the migration file from the branch that was deployed, or
3. Rebuild the binary from the correct source.

#### Duplicate prefixes (development only)

If lint fails with "duplicate migration prefixes":

1. Renumber one of the conflicting files:
   ```bash
   git mv internal/infra/database/migrations/058_foo.sql \
          internal/infra/database/migrations/070_foo.sql
   ```

2. If already applied, update the tracker:
   ```sql
   UPDATE schema_migrations_feedback
   SET version = 70, filename = '070_foo.sql'
   WHERE filename = '058_foo.sql';
   ```

### Expand-contract pattern

For breaking schema changes, use the expand-contract pattern:

1. **Expand** (migration N): Add new column/table, nullable or with default
2. **Deploy code** that writes to both old and new
3. **Backfill** (migration N+1 or background job): Populate new from old
4. **Deploy code** that reads only from new
5. **Contract** (migration N+2): Remove old column/table

This ensures zero-downtime deploys with rollback capability at each step.

#### Dirty state recovery

A migration is in "dirty" state when it started but did not complete (e.g., the
process crashed mid-apply). On next startup, attune detects this and refuses to
continue.

**Error message format:**

```
dirty migration detected: version 42 (042_add_index.sql) started but did not complete

This indicates a previous migration attempt crashed or was interrupted.

Recovery options:
  1. If the migration partially applied, manually verify database state
  2. Run 'attune migrations repair --version 42' to mark it resolved
  3. If safe to retry, delete the row: DELETE FROM schema_migrations_feedback WHERE version = 42
```

**Recovery steps:**

1. Inspect the database to determine how much of the migration applied:
   ```sql
   \d+ table_name  -- check if column/index exists
   ```

2. If the migration fully applied (just the tracker didn't update):
   ```bash
   attune migrations repair --version 42
   ```

3. If partially applied, either:
   - Manually complete the remaining statements, then repair
   - Manually undo the partial changes, delete the tracker row, and let it
     re-apply:
     ```sql
     DELETE FROM schema_migrations_feedback WHERE version = 42;
     ```

#### Manifest hash mismatch

The manifest hash detects migration reordering—inserting a new migration between
existing ones or renumbering files after they've been applied. Migrations must
be applied in the same order they were originally recorded.

**Error message format:**

```
migration reordering detected:
  stored manifest:   a1b2c3d4
  computed manifest: e5f6g7h8

This happens when migrations are reordered (e.g., inserting a new file
between existing ones). Migration order must match the order in which
they were originally applied.

Recovery options:
  - Restore original migration order from git history
  - If reordering was intentional, update stored manifest (see docs/private-deploy.md)
```

**Recovery steps:**

1. If the reordering was accidental, restore the original order from git:
   ```bash
   git log -p -- internal/infra/database/migrations/
   ```

2. If reordering was intentional and safe (e.g., renumbering gaps), update the
   stored manifest:
   ```sql
   -- First compute the new manifest hash
   -- Run: attune migrations verify --format json | jq .manifest_hash
   UPDATE schema_migrations_manifest SET hash = '<new-hash>', updated_at = NOW() WHERE id = 1;
   ```

### Repair command

Marks a dirty or failed migration as successfully applied and recalculates its
checksum from the current embedded file.

```bash
attune migrations repair --version N [--force]
```

| Flag | Description |
|------|-------------|
| `--version N` | Migration version to repair (required) |
| `--force` | Skip confirmation prompt |

**When to use:**

- Migration crashed after applying SQL but before updating tracker
- You manually verified/completed a partial migration
- Migration file was intentionally modified after apply (recalculate checksum)

**What it does:**

1. Sets `success = TRUE` in the tracker
2. Recalculates and stores the checksum from the current embedded file
3. Records the repair operation in `applied_by`

### Baseline command

Records migrations 1..N as already applied without executing them. Use this to
adopt an existing database that was migrated externally (e.g., manual SQL or
another migration tool).

```bash
attune migrations baseline --version N [--force]
```

| Flag | Description |
|------|-------------|
| `--version N` | Target version—marks migrations 1 through N as applied (required) |
| `--force` | Skip confirmation prompt |

**Safety:** Baseline only works on databases with an empty tracker table. If any
migrations are already tracked, the command fails. This prevents accidental
re-baselining of a production database.

**What it does:**

1. Creates the tracker table if missing
2. Inserts records for migrations 1..N with current checksums
3. Marks all as `success = TRUE` with `applied_by = baseline:<version>`

### CLI reference

All migration commands use `--config` to locate the database (same as `attune server`).

```bash
attune migrations status [--format text|json] [--pending]
attune migrations verify [--format text|json]
attune migrations dry-run
attune migrations repair --version N [--force]
attune migrations baseline --version N [--force]
```

**Exit codes:**

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | Failure (error printed to stderr) |

**JSON output examples:**

`attune migrations status --format json`:
```json
{
  "total": 72,
  "applied": 72,
  "pending": 0,
  "migrations": [
    {
      "version": 1,
      "filename": "001_initial.sql",
      "status": "applied",
      "applied_at": "2024-01-15T10:30:00Z",
      "checksum": "a1b2c3d4e5f6...",
      "duration_ms": 45,
      "applied_by": "attune/v0.9.0"
    }
  ],
  "checksums": {
    "total": 72,
    "verified": 72,
    "drifted": []
  },
  "duplicates": []
}
```

`attune migrations verify --format json`:
```json
{
  "duplicates": true,
  "checksums": true,
  "no_tx": true,
  "passed": true
}
```

---

## References

- [`deploy/README.md`](../deploy/README.md) -- compose kit quick-reference.
- [`config.example.yaml`](../config.example.yaml) -- full annotated
  config-first reference.
- [`observability/README.md`](../observability/README.md) -- metric contract
  and dashboard details.
- [CLAUDE.md](../CLAUDE.md) -- engineering guidelines (for contributors).
