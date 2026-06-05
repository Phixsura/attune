# attune — private deployment guide

Self-host attune (feedback ingestion + LLM enrichment + fan-out) with Docker
Compose in about 10 minutes. Every command and output below was run against a
real cold deployment.

> **Scope.** This is the full guide. For the 3-command happy path see
> [`../deploy/README.md`](../deploy/README.md). This document is canonical for the
> deep topics (TLS, backup, upgrades, troubleshooting).

> **The happy path is smoke-tested in CI.** [`scripts/smoke-deploy.sh`](../scripts/smoke-deploy.sh)
> builds attune from source and walks §4–5 + §9 (up → healthz → tenant → key →
> ingest → enrich → backup) with a mock LLM, asserting the steps **plus** the
> enriched values and `/metrics` — so a code change that breaks a documented
> happy-path step fails CI. It is a *liveness* check, **not** a guarantee the prose
> is correct: it doesn't exercise the obs/TLS overlays, failure modes, or real LLM
> backends (the mock is permissive), and §7 public-domain TLS + §10 China-region
> pulls are verified manually.

---

## 1. Prerequisites

- **Docker 24+ with the Compose v2 plugin** — check: `docker compose version`.
- **~1 GB free RAM** is plenty. The base stack is tiny — measured live: attune
  **~7 MiB**, Postgres **~29 MiB**. The optional monitoring overlay adds
  Prometheus + Grafana (~250 MiB, capped at 512 MiB each).
- **An OpenAI-compatible LLM endpoint** for enrichment. attune calls
  `/v1/chat/completions` with a **fixed model name, `gpt-4o-mini`** — so OpenAI
  works out of the box; a local **ollama / vLLM** must serve a model
  **named/aliased `gpt-4o-mini`**. (Azure OpenAI isn't supported yet — its path
  and `api-version` differ; tracked in #60.) The stack **boots and serves without
  an LLM**; only enrichment needs it (see §5).

---

## 2. Step 1 — clone

```bash
git clone https://github.com/Phixsura/attune && cd attune
```

---

## 3. Step 2 — configure

```bash
cp deploy/.env.example deploy/.env
```

Edit `deploy/.env` and set **at minimum**:

- `POSTGRES_PASSWORD` — a strong password.
- the password slot in `FEEDBACK_API_DATABASE_URL` — the **same** password.
- `FEEDBACK_API_LLM_OPENAI_API_KEY` — your OpenAI-compatible key.

> **URL-encode special characters in the DB password.** `FEEDBACK_API_DATABASE_URL`
> is a URL, so `@ : / ? #` in the password must be percent-encoded (`@`→`%40`,
> `/`→`%2F`). An unescaped char fails at startup with, e.g.:
> `failed to parse as URL (invalid port ":pa" after host)`. (pgx, the Postgres
> driver, masks the password as `xxxxxx` in that specific parse error.)

For a **local LLM** instead of OpenAI, point at it and use any non-empty key. It
must serve a model named `gpt-4o-mini` (see §1 / #60):

```bash
# ollama on the host:
FEEDBACK_API_LLM_OPENAI_BASE_URL=http://host.docker.internal:11434
FEEDBACK_API_LLM_OPENAI_API_KEY=sk-local
```

On **Linux**, `host.docker.internal` isn't automatic — add
`extra_hosts: ["host.docker.internal:host-gateway"]` to the `attune` service in
`docker-compose.yml`, or use the host's LAN IP.

---

## 4. Step 3 — start

```bash
cd deploy
docker compose up -d
```

The first run pulls `ghcr.io/phixsura/attune:latest` from GitHub Container
Registry (public, no login), starts Postgres, waits for it to be healthy, then
attune **runs its migrations on boot**.

---

## 5. Verification

**a. Health.** First boot runs Postgres init + migrations — **give it up to ~60 s**,
then:

```bash
curl http://localhost:8090/healthz      # -> ok
docker compose logs attune | tail
# {"level":"INFO","msg":"attune server listening","addr":":8090"}
# {"level":"INFO","msg":"feedback enricher started",...}
# {"level":"INFO","msg":"outbox worker started",...}
```

**b. Create a tenant + API key.** Admin subcommands run in a one-off container and
do **not** migrate, so run them after the server above is up:

```bash
docker compose run --rm attune tenant create --slug acme --name "Acme Inc"
docker compose run --rm attune keys issue --tenant acme --label main
#   key:    fbk_live_0123456789abcdef0123456789abcdef   # example — yours differs
#   Store this key now — it is not recoverable.
```

> Copy the value on the **`key:`** line. The logs also print a 12-char
> `prefix:fbk_live_xxx` for identification — that is **not** the key.

**c. Send feedback** with the printed key (it works immediately — no restart):

```bash
curl -X POST http://localhost:8090/v1/feedback/ingest \
  -H "X-API-Key: fbk_live_..." -H "Content-Type: application/json" \
  -d '{"content":"the export button is broken on Safari","source":"web"}'
# {"enrichmentStatus":"pending","id":3}
```

**d. Confirm enrichment.** Within ~30 s the enricher calls your LLM and fills in
the classification. With a working key:

```bash
docker compose exec postgres psql -U attune attune \
  -c "select id, enrichment_status, enriched_kind, enriched_severity, enriched_title from user_feedback;"
#  3 | done | bug | P2 | Export button broken on Safari
```

If `enrichment_status` stays `pending`/`failed`, your LLM endpoint is rejecting
the call — see troubleshooting below. (attune **auto-retries** failed rows once
the LLM recovers.)

---

## 6. Optional — monitoring overlay

Prometheus + Grafana, zero manual setup:

```bash
docker compose -f docker-compose.yml -f docker-compose.obs.yml up -d
```

Grafana → http://localhost:3000 (`admin` / `GF_SECURITY_ADMIN_PASSWORD`); the
"Attune Overview" dashboard auto-loads. Prometheus → http://localhost:9090.
Tear down with the **same** `-f` flags. Details + the metrics contract:
[`../observability/README.md`](../observability/README.md).

> This overlay is a **reference/dev** stack. For production, scrape attune's
> `/metrics` from your existing monitoring (separate from the app host).

---

## 7. Optional — HTTPS with Caddy

Put attune behind Caddy for automatic Let's Encrypt TLS:

```bash
cp Caddyfile.example Caddyfile          # then set your domain in it
docker compose -f docker-compose.yml -f docker-compose.tls.yml up -d
```

`Caddyfile.example`:

```caddyfile
attune.example.com {
	reverse_proxy attune:8090
}
```

Caddy terminates TLS on 443 and reverse-proxies to attune over the compose
network (verified: requests arrive with a `via: 1.1 Caddy` header). **Automatic
TLS needs a public domain whose DNS points here and ports 80/443 reachable from
the internet** (the ACME challenge). Caddy reaches attune over the compose
network, so **leave `ATTUNE_BIND` at its `127.0.0.1` default** — attune's own
port then listens on loopback only, never reachable in plaintext from outside.
(Setting `ATTUNE_BIND=0.0.0.0` behind Caddy would expose attune in plaintext on
`:8090` beside the TLS front door — don't.)

---

## 8. Upgrades

```bash
cd deploy
docker compose pull && docker compose up -d
```

> **Pinning.** `:latest` moves, so plain `docker compose up -d` tracks it. For a
> **reproducible** deploy, pin a release in `.env`. A version tag is simplest —
> `ATTUNE_IMAGE=ghcr.io/phixsura/attune:0.2.0` (or `:0.2` to follow that minor
> line's patches). A digest is strongest (fully immutable):
> `ATTUNE_IMAGE=ghcr.io/phixsura/attune@sha256:<digest>`
> (get it with `docker inspect --format '{{index .RepoDigests 0}}' ghcr.io/phixsura/attune:0.2.0`).
> The obs/TLS overlay images are already pinned.

---

## 9. Backup + restore

Postgres holds all your data (in the `attune-pg` volume). Dump it:

```bash
docker compose exec postgres pg_dump -U attune attune > backup.sql
```

Restore into a fresh database:

```bash
cat backup.sql | docker compose exec -T postgres psql -U attune attune
```

(Round-trip verified: dump → scratch DB → rows restored intact.)

---

## 10. Troubleshooting

**LLM rejected (401 / rate-limit / unknown model).** Enrichment fails, feedback
stays `pending`/`failed`. The real error is in the logs:

```bash
docker compose logs attune | grep "enrich failed"
# ... status 401: "Incorrect API key provided: sk-...". code: invalid_api_key
```
Fix the key / base URL in `.env` and recreate: `docker compose up -d`. **"unknown
model"** means your backend doesn't serve `gpt-4o-mini` — the model name is fixed,
so alias it on ollama/vLLM (or follow #60).

**Port 8090 already in use.** `docker compose up` fails with:
```
Bind for 127.0.0.1:8090 failed: port is already allocated
```
Set a free port in `.env`: `ATTUNE_PORT=8091`, then `docker compose up -d`.

**DB password special chars not URL-encoded.** Startup logs:
```
[database.NewPool] parse url failed ... failed to parse as URL (invalid port ":pa" after host)
```
Percent-encode `@ : / ? #` in `FEEDBACK_API_DATABASE_URL` (see §3).

**"invalid api key" on ingest.** You sent the 12-char `prefix:` from the logs, not
the full `key:` value. Re-issue and copy the `key:` line. (Keys work immediately;
no restart needed.)

**Lark signature mismatch** (if you wire inbound Lark events). `400`/rejected
webhooks mean `FEEDBACK_API_LARK_SIGNING_SECRET` doesn't match the secret in your
Lark app's event-subscription config. Also set `FEEDBACK_API_LARK_DEFAULT_TENANT_SLUG`.

**China-region image pulls time out.** The attune image is on **ghcr.io** (usually
reachable); Postgres/Prometheus/Grafana/Caddy are on **Docker Hub**, whose CDN
(`cloudfront.docker.com`) often resets mid-download here. Docker resumes partial
layers, so **retrying** `docker compose pull` frequently completes it; otherwise
configure a registry mirror in Docker's `daemon.json`.

---

## See also

- [`../deploy/README.md`](../deploy/README.md) — minimal quickstart.
- [`../observability/README.md`](../observability/README.md) — metrics contract.
- [`../config.example.yaml`](../config.example.yaml) — every config field + env override.

