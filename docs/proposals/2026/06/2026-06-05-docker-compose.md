# Proposal — `docker compose up -d` private-deploy stack (attune + postgres)

| | |
|---|---|
| **Issue** | #5 |
| **Status** | Implemented |
| **Started** | 2026-06-05 16:58 CST |
| **Related** | #14 (Dockerfile, reused) · #2 (release → `ghcr.io` image; **sequencing dependency**) · #6 (observability overlay) · #7 (quickstart docs) |

## Problem

External operators have no one-command way to stand up attune. The standalone
multi-arch image exists (#14) and the release workflow publishes it (#2), but
nothing wires that image to a Postgres instance with sane defaults. This blocks
the v0.3 "Deployable" milestone and the OSS "5-minute setup" promise — the
README still says *"Production docker-compose kit lands in v0.3. For now, build
from source."*

> **Reviewed by a 3-lens panel (ops / security / spec) on 2026-06-05.** Verdict:
> APPROVE_WITH_CHANGES ×3 (high confidence). This v3 folds in their must-fixes
> and two maintainer decisions: (a) **cut a release** so `:latest` exists;
> (b) **remove `/health`** (no deprecation window) with justification.

## Goals

- `cd deploy && cp .env.example .env && docker compose up -d` brings up a
  working **attune + postgres** stack with nothing else installed.
- `curl http://localhost:8090/healthz` returns `200` (and `/health` → `404`).
- A documented path to the **first tenant + API key**, so the stack is usable,
  not just alive.
- Postgres data survives `docker compose down && docker compose up`.
- **Secure-by-default**: loopback bind, hardened containers, no weak default
  that boots silently.
- Every knob documented in `.env.example`; secrets via env only.

## Non-goals

- **Observability overlay** (Prometheus/Grafana/OTel) — #6, a separate overlay.
- **Quickstart prose / tutorial** — #7. This ships the files + a minimal
  kit-local README only.
- **TLS termination / reverse proxy** — operators front attune themselves.
- **New env overrides** for `port` / `enricher_*` — out of scope; `config.yaml`
  stays the escape hatch for those (see "Config precedence").
- **Full backup automation / resource tuning** — the kit documents a `pg_dump`
  one-liner and ships commented limits; turnkey backups are a follow-up.
- **Console UI deployment** — deferred to a dedicated follow-up issue, and
  `.env.example` ships **no** console vars. Reason: a *standard* self-host login
  (Grafana-style local bootstrap admin, SSO optional) needs a backend capability
  attune lacks today — the console authenticates **only** via Lark OAuth (or a
  test-only `dev_login` backdoor); there is no local password auth. Adding it is
  a vertical, security-sensitive feature (password login + a `tenant_users`
  migration + a new SPA login page) that must not ride in a deploy PR (§6). The
  console front-door (nginx serving the SPA + proxying `/fb/v1/*`) and a
  published `attune-console` release image land **on top of** that feature.
  Keeping console out also preserves acceptance #4 (`console_enabled:false` by
  default) and avoids a footgun where setting `CONSOLE_SESSION_KEY` alone makes
  the server refuse to boot (it then requires `LARK_APP_*` + `CONSOLE_BASE_URL`).

## Proposal

### Files (all under `deploy/`)

- **`docker-compose.yml`** — two services, one named volume.
- **`.env.example`** — every env var, commented; copied to `.env` (gitignored).
- **`config.yaml`** — optional YAML template for the *only* fields with no env
  override; its compose mount is **commented out** (env-only is the default).
- **`README.md`** — minimal ordered quickstart (the *only* in-kit prose; the
  full tutorial is #7). Needed because the bootstrap ordering must ship with the
  files, not live only in this proposal.

### `attune` service

- `image: ${ATTUNE_IMAGE:-ghcr.io/phixsura/attune:latest}` — pinnable via env;
  see **Release sequencing** for how `:latest` comes to exist.
- Commented `build: { context: .., dockerfile: Dockerfile }` fallback.
- `env_file: .env`; `ports: ["${ATTUNE_BIND:-127.0.0.1}:${ATTUNE_PORT:-8090}:8090"]`
  — **binds loopback by default** (CLAUDE.md §8); operators set `ATTUNE_BIND=0.0.0.0`
  when fronting with their own proxy.
- `restart: unless-stopped`.
- `depends_on: postgres: { condition: service_healthy }` — attune pings the DB
  and **runs migrations on startup** (`server.go:189` → `database.RunMigrations`),
  so it must not race postgres' first boot.
- **Hardening** (safe on distroless/static, no shell, uid 65532):
  `security_opt: ["no-new-privileges:true"]`, `cap_drop: ["ALL"]`,
  `read_only: true` (+ `tmpfs: /tmp` only if the e2e shows it needs scratch).
- **No container healthcheck** — distroless has no shell/`curl`, so a
  `CMD-SHELL` probe can't run. Liveness is host-side via `GET /healthz`. (Cost:
  no auto-restart on a wedged-but-alive process; #6 adds real probes.)
- `logging: { options: { max-size: "10m", max-file: "3" } }` — bound log growth.

### `postgres` service

- `image: postgres:17-alpine`, `env_file: .env` (reads `POSTGRES_*`).
- Named volume `attune-pg:/var/lib/postgresql/data`.
- `healthcheck: pg_isready -U ${POSTGRES_USER:-attune}`, `interval: 5s`,
  `timeout: 5s`, `start_period: 10s`, `retries: 10` — `start_period` stops the
  first-init restart window from burning retries.
- **Hardening**: `security_opt: ["no-new-privileges:true"]` only. **Not**
  `cap_drop: ALL` / `read_only` — the official entrypoint needs CHOWN/SETUID/
  SETGID to init the data dir and gosu-drop to the `postgres` user; dropping them
  breaks first-init. (Documented asymmetry: hardened app, root-capable DB init.)
- `logging` caps as above; `restart: unless-stopped`.

### Health endpoint: `/healthz` only — **remove `/health`** (no deprecation window)

Decision (maintainer): **`/healthz` is the single canonical liveness path** — the
trailing-`z` is the Google/Kubernetes convention so a health route never collides
with a real application path
([origin](https://stackoverflow.com/questions/43380939/where-does-the-convention-of-using-healthz-for-application-health-checks-come-f)).
`/health` is dropped, not aliased.

**Why no deprecation window** (the panel correctly noted the accepted #14 proposal
advertised `/health` as the liveness contract, so this is a real break): we're
pre-1.0 — CLAUDE.md §3 permits breaking changes on a flagged minor bump — there
are **no known external deployments** (the kit that would create them is *this*
issue), and the in-repo blast radius is one file (`router.go`). The carrying cost
of an alias nobody is confirmed to use isn't worth it. It ships as a flagged
`Removed`/BREAKING changelog entry; #14's historical e2e table will be noted as
superseded.

The otelchi trace filter **keeps matching the `/health` prefix** (which still
covers `/healthz`) — purely to suppress trace-span noise from any lingering
`/health` probes (now `404`). The comment is reworded so it reads as trace
hygiene, not a deprecation contract.

### Postgres DSN inside the compose network

`FEEDBACK_API_DATABASE_URL=postgres://attune:<pw>@postgres:5432/attune?sslmode=disable`

- Host is the compose **service name** `postgres`.
- `sslmode=disable`: the connection never leaves the compose bridge network and
  `postgres:17-alpine` serves plaintext by default — distinct from the
  `sslmode=require` external-DB default in the root `config.example.yaml`.

### Config precedence (what `.env` covers vs `config.yaml`)

Env overrides YAML for every field **except the three with no override**:
`port`, `enricher_interval`, `enricher_batch`. (`custom_webhooks` *does* have an
env override — the `FEEDBACK_API_CUSTOM_WEBHOOKS` JSON blob — so it is **not** in
that set; v2 of this proposal and the draft `config.yaml`/compose comment were
wrong on this and are corrected.) First boot needs only `.env`; `config.yaml` is
the optional escape hatch for those three.

### Secrets / weak-default handling

- `.env` is gitignored (verified — `.gitignore:21` matches `deploy/.env`); only
  `.env.example` + `config.yaml` (no secrets) are committed.
- `.env.example` ships **`POSTGRES_PASSWORD=` empty** (and the DSN with an empty
  password slot) so a first boot **fails loud** until the operator sets one,
  rather than silently running on `change-me-please`.
- Password duplicated in `POSTGRES_PASSWORD` and the DSN (attune takes one DSN
  string) — flagged with a "keep in sync" comment; the kit README repeats it.
- **No console env vars** in `.env.example` (console is a separate issue — see
  Non-goals). This removes the footgun where `CONSOLE_SESSION_KEY` set alone made
  the server refuse to boot.

### First-tenant bootstrap (after the stack is healthy)

A bare stack accepts no feedback until a tenant + API key exist. The admin
subcommands do **not** run migrations (only `server` does), so they must run
**after** `up -d` has booted and migrated:

```
docker compose up -d                  # boots server → runs migrations
# wait until `docker compose logs attune` shows the startup OK line, then:
docker compose run --rm attune tenant create --slug acme --name "Acme"
docker compose run --rm attune keys issue --tenant acme --label main
```

Works on distroless (entrypoint is the exec-form `attune` binary, no shell). The
kit README ships these ordered commands; full walk-through is #7.

### Release sequencing (decision: cut a release first)

The kit defaults to `:latest`, which **does not exist yet** (verified: no git
tags, no releases, `ghcr.io/phixsura/attune:latest` not pullable). So acceptance
#1 cannot pass on a clean host until a release publishes it. Plan:

1. Implement #5 **including the `/healthz` change** and merge to `main`.
2. Cut **v0.2.0** per CLAUDE.md §2 "On release": move `[Unreleased]` → `[0.2.0]`,
   update compare links, tag `v0.2.0`. (Pre-1.0 minor; bundles the already-
   unreleased `listen→attune` rename **and** this kit + the breaking `/health`
   removal — all flagged.)
3. #2's workflow builds + pushes `ghcr.io/phixsura/attune:{0.2.0,latest}` **from
   the tagged commit that contains `/healthz`**, so `:latest` satisfies `curl
   /healthz`.

Until the tag runs, the e2e uses a **stand-in image built from source** and
tagged `:latest` locally (compose then uses it without pulling). The actual
`git tag` push (publishes to ghcr + a GitHub Release) is a maintainer action,
done only on explicit go-ahead.

## Alternatives considered

- **Keep `/health` as a deprecated alias for one release.** *Rejected* by the
  maintainer — pre-1.0, no known external deployers, blast radius one file.
- **Build-from-source as the default** (instead of cutting a release).
  *Rejected* — maintainer chose to publish `:latest` via a real v0.2.0 so the
  documented one-liner works verbatim.
- **Mount `config.yaml` by default.** *Rejected* — env covers every required
  field and keeps secrets out of a committed file. Mount stays commented.
- **Blanket `cap_drop: ALL` on both services** (panel suggestion). *Rejected for
  postgres* — breaks its root-capable first-init; applied to `attune` only.
- **Build the DSN from `POSTGRES_*` parts.** *Rejected* — attune takes one DSN
  string; no clean compose splice. Duplication flagged inline.
- **Healthcheck attune via the `:debug` distroless tag.** *Rejected* — ships a
  shell into prod purely for a probe; host-side `/healthz` suffices.

## Risks / tradeoffs / compatibility

- **BREAKING — `/health` removed.** Any deployment probing `/health` must switch
  to `/healthz`. Flagged minor bump (`Removed`), justified above.
- **`:latest` is a moving tag.** Fine for quickstart, risky for prod; the
  `ATTUNE_IMAGE` override + a commented **digest-pin** example (`@sha256:…`,
  recommended over a version tag) steer toward pinning. `postgres:17-alpine` is
  tag- not digest-pinned; digest pin recommended in a comment for auditable runs.
- **Password duplicated** in `POSTGRES_PASSWORD` and the DSN — drift ⇒ connect
  failure. Empty-by-default + sync comment mitigate.
- **No attune healthcheck** (distroless) → restart only on crash, not on a
  wedged-but-alive process. `restart: unless-stopped` + #6 cover it.
- **Placeholder LLM key boots green but enrichment 401s** — "alive ≠ usable."
  `.env.example` + README call this out and point at the `api_key_set` log field.
- **`pg_isready` first-init transient** — `start_period: 10s` + attune's
  migration retry tolerate it; to be confirmed in the e2e.

## Implementation plan

1. `router.go`: `mountHealth` registers **`/healthz` only**; drop `/health`;
   reword the trace-filter comment (`router.go:29,42,44,85`).
2. `router_test.go`: assert `/healthz` → `200 ok` **and** `/health` → `404`.
3. `deploy/docker-compose.yml`, `.env.example`, `config.yaml`, **`README.md`**
   — with hardening, loopback bind, empty password, healthcheck `start_period`/
   `timeout`, logging caps, ordered bootstrap, digest-pin + `pg_dump` notes.
4. `CHANGELOG.md` `[Unreleased]`: `Added` (deploy kit, `/healthz`) +
   `Removed` (BREAKING: `/health`).
5. Root README Quickstart: replace the "lands in v0.3" note with the compose
   recipe + first-tenant one-liner.
6. Verify (below).
7. **Release** (maintainer go-ahead): merge → move `[Unreleased]`→`[0.2.0]` +
   compare links → tag `v0.2.0` → #2 publishes `:latest`.

## Verification — run 2026-06-05, ✅ all green

Gates: `go build ./...` ✓, `go vet ./...` ✓, `go test -short ./...` ✓ (all pkgs),
`docker compose config` ✓, `lint-slog` ✓ (my change clean; 2 pre-existing
warn-only hits elsewhere), `lizard router.go` ✓ (CCN 1.4, NLOC 75). E2e against
a stand-in `:latest` built from source (real one comes from the v0.2.0 release):

- [x] `docker compose up -d` succeeds — postgres `healthy` then attune started via
  `depends_on`; attune live in ~2s; `read_only` container stayed up; postgres port
  not published; host bound `127.0.0.1:8090`.
- [x] `curl http://localhost:8090/healthz` → `ok` (200); `/health` → `404`.
- [x] `docker compose logs attune` → `OK,port:8090,console_enabled:false,lark_enabled:false`.
- [x] First-tenant bootstrap `docker compose run --rm attune tenant create` works on
  distroless; tenant row survived `docker compose down && up` (volume persists).

Release step (tag `v0.2.0` → publish real `:latest`) pending maintainer go-ahead.

## References

- #14 standalone Dockerfile · #2 release workflow (`:latest` source) · #6 / #7.
- `/healthz` convention: https://stackoverflow.com/questions/43380939/where-does-the-convention-of-using-healthz-for-application-health-checks-come-f
- Root `config.example.yaml` + `internal/infra/config/env.go` (env override table).
- Panel review 2026-06-05 (ops / security / spec lenses): APPROVE_WITH_CHANGES ×3.
