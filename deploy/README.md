# attune — docker-compose deploy

One-command **attune + Postgres** stack for self-hosting. Requires Docker with
the Compose plugin (`docker compose version`).

> Full tutorial lands with #7; this is the minimal ordered path.

## 1. Configure

```bash
cd deploy
cp .env.example .env
```

Edit `.env` and set **at least**:

- `POSTGRES_PASSWORD` — a strong password (left empty in the example so a
  misconfig fails loudly).
- the password slot in `FEEDBACK_API_DATABASE_URL` — **same** password.
- `FEEDBACK_API_LLM_OPENAI_API_KEY` — your OpenAI-compatible key. The stack
  boots and `/healthz` passes without it, but enrichment will 401 until it's
  real.

## 2. Start

```bash
docker compose up -d
```

`attune` waits for Postgres to be healthy, then **runs migrations on startup**.
Check it came up:

```bash
docker compose logs attune        # expect: ...OK,port:8090,console_enabled:false,lark_enabled:false
curl http://localhost:8090/healthz   # -> ok
```

## 3. Create the first tenant + API key

The admin subcommands do **not** run migrations, so run them only **after** the
server above is up (it migrates on boot):

```bash
docker compose run --rm attune tenant create --slug acme --name "Acme"
docker compose run --rm attune keys issue --tenant acme --label main
```

Send feedback with the printed key:

```bash
curl -X POST http://localhost:8090/v1/feedback/ingest \
  -H "X-API-Key: <key>" -H "Content-Type: application/json" \
  -d '{"text":"hello"}'
```

## Operations

- **Data** lives in the `attune-pg` volume; it survives `docker compose down`.
  `docker compose down -v` wipes it.
- **Backup / restore:**
  ```bash
  docker compose exec postgres pg_dump -U attune attune > backup.sql
  cat backup.sql | docker compose exec -T postgres psql -U attune attune
  ```
- **Exposure:** the host port binds `127.0.0.1` by default. Front attune with
  your own TLS reverse proxy; set `ATTUNE_BIND=0.0.0.0` only behind one.
- **Pinning:** `:latest` moves. Pin `ATTUNE_IMAGE` to a version or, best, a
  `@sha256:` digest for reproducible deploys.
- **No published image yet?** Build from source: `docker compose up -d --build`
  (uncomment the `build:` block in `docker-compose.yml`).

## Not included

The **web console UI** is a separate deployment (its own front-door, image, and
local-login feature) — tracked in a follow-up issue. This kit is the backend +
Postgres only.
