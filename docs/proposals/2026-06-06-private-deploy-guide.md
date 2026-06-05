# Proposal — `docs/private-deploy.md` private-deployment guide

| | |
|---|---|
| **Issue** | #7 |
| **Status** | Accepted (2026-06-06) |
| **Started** | 2026-06-06 |
| **Related** | #5 (main compose) · #6 (obs overlay — PR #57) · #10 (per-tenant module config) |

## Problem

attune has a working deploy kit (#5) + an obs overlay (#6), but **no guide an
external OSS user can follow** to stand it up. `deploy/README.md` is a minimal
ordered path and even says "Full tutorial lands with #7". Without a real
quickstart, a self-host service is dead-on-arrival for adoption.

## Value & audience (honest)

- **What it is:** the **adoption on-ramp** — P0 on the v0.3 「可装/Deployable」
  milestone. It converts "looks cool, can't run it" into "running in 15 minutes."
- **Who benefits:** external evaluators/operators with **zero attune-code
  knowledge**. Not contributors.
- **Where the value really sits:** *accuracy + completeness*. A quickstart with
  one wrong command is **worse than none** — it erodes trust instantly. So the
  whole game is verified, copy-pasteable commands. Our edge: the stack was just
  **live-run end-to-end**, so every command/output below is captured from a real
  run, not guessed.
- **What it is *not*:** a production-ops manual (HA, scaling, k8s, observability
  deep-dives). It's the "get it running + the handful of things that bite you"
  doc. Docs-only → changelog-exempt (CLAUDE.md §2).

## Benchmark — how top self-host repos structure this

Consistent across Sentry self-hosted, Plausible CE, Immich, Supabase:

- **README = thin "no-choices" quickstart** (clone → `.env` → `up` → verify).
- **A dedicated guide = the comprehensive one** (prerequisites, reverse-proxy/TLS,
  backup, upgrade, troubleshooting). README **links** to it; deep content is
  **single-sourced** there.
- Reverse-proxy/TLS is usually shown via **Caddy/Traefik auto-TLS** (far simpler
  for the target user than nginx + manual certbot).

## Proposal

### Resolved decisions (this brainstorming pass)

| Decision | Choice | Rationale |
|---|---|---|
| DRY boundary | **`docs/private-deploy.md` is canonical/comprehensive; `deploy/README.md` slims to quickstart + link** | top repos (Immich/Sentry/Plausible) keep README thin, deep content single-sourced; avoids drift |
| SSL/reverse-proxy | **Caddy auto-TLS** — `deploy/Caddyfile.example` + a runnable `deploy/docker-compose.tls.yml` overlay | simplest for zero-ops external users (auto Let's Encrypt, ~3 lines) vs nginx+certbot; matches the `-f` overlay pattern from #6 |
| command accuracy | **every command live-run + captured** | §9-style "verify, don't claim"; fixes the issue's wrong `docker exec attune-postgres` → `docker compose exec postgres` |
| registry troubleshooting | **written from the firsthand Docker-Hub-CDN failure**, distinguishing ghcr.io (attune) vs Docker Hub (deps) | we hit it live; the real fix is retry-resume or a registry mirror |
| module config (#10) | **pointer only** | out of scope; #10 owns it |
| doc-rot guard | **L1 smoke (`scripts/smoke-deploy.sh`) + L2 `deploy-docs` gate + L3 link check** | defense-in-depth. *Honest scope* (per the 3-agent review): L1 is a happy-path **liveness** check (mock LLM, no overlays/failure-modes), L2 is a nudge (path-presence, gameable), L3 = relative inline links only. Code gaps it can't cover (hardcoded model, Azure) → #60 |

### Files

- **Create `docs/private-deploy.md`** — the canonical guide (sections below).
- **Create `deploy/Caddyfile.example`** — Caddy reverse proxy, auto-TLS, reverse-
  proxies `attune:8090` over the compose network.
- **Create `deploy/docker-compose.tls.yml`** — a `caddy` service (publishes 80/443,
  mounts the Caddyfile, on the compose network) so the TLS example is *runnable*:
  `docker compose -f docker-compose.yml -f docker-compose.tls.yml up -d`.
- **Edit `deploy/README.md`** — slim: keep the 3-command quickstart + obs pointer;
  move deep Operations (backup/restore detail, exposure, pinning, troubleshooting)
  to a one-line "Full guide → `../docs/private-deploy.md`". Keep only a data-volume
  one-liner inline.
- **Edit root `README.md`** — add a link to `docs/private-deploy.md` (acceptance #3).

### `docs/private-deploy.md` sections (from the issue outline, refined)

1. **Prerequisites** — Docker 24+ / Compose v2 (`docker compose version`); an
   OpenAI-compatible LLM endpoint (or local ollama/vllm — note `host.docker.internal`).
2. **Step 1 — clone** — `git clone https://github.com/Phixsura/attune && cd attune`.
3. **Step 2 — configure** — `cp deploy/.env.example deploy/.env`; set
   `POSTGRES_PASSWORD` (+ the matching slot in `FEEDBACK_API_DATABASE_URL`) and
   `FEEDBACK_API_LLM_OPENAI_API_KEY`. **Call out URL-encoding** if the password has
   `@` / `:` / `/` (ties to troubleshooting #4).
4. **Step 3 — start** — `cd deploy && docker compose up -d`.
5. **Verification** *(live-captured)* —
   - `curl http://localhost:8090/healthz` → `ok`.
   - boot log: `docker compose logs attune` → the **real** lines
     (`"attune server listening","addr":":8090"`, enricher/outbox/digest started).
   - create a tenant + key (`docker compose run --rm attune tenant create … / keys
     issue …`), POST an ingest with the printed key, confirm `attune_ingest_total`.
6. **Optional monitoring** — `-f docker-compose.obs.yml`; Grafana `:3000`
   (admin + `GF_SECURITY_ADMIN_PASSWORD`); "Attune Overview" auto-loads. Links to
   `deploy/README.md` §4 + `observability/README.md` (the contract). *(Depends on #6.)*
7. **Optional TLS** — Caddy auto-TLS: edit `deploy/Caddyfile.example` (your domain),
   `docker compose -f docker-compose.yml -f docker-compose.tls.yml up -d`; Caddy
   fronts attune on 443 with an automatic Let's Encrypt cert.
8. **Upgrades** — `docker compose pull && docker compose up -d`; note the
   **pin-vs-`:latest`** nuance (obs images are pinned per #6; pin `ATTUNE_IMAGE` for
   reproducible prod).
9. **Backup + restore** *(corrected command)* —
   `docker compose exec postgres pg_dump -U attune attune > backup.sql` /
   `cat backup.sql | docker compose exec -T postgres psql -U attune attune`.
10. **Troubleshooting (≥5)** —
    - LLM rejected (401 / rate-limit / unknown model) — where to look in logs.
    - Lark signature mismatch (`FEEDBACK_API_LARK_SIGNING_SECRET` ↔ the Lark app).
    - Port 8090 already in use → override `ATTUNE_PORT`.
    - Postgres password with `@`/`:`/`/` not URL-encoded in `FEEDBACK_API_DATABASE_URL`.
    - **China-region image pulls** — ghcr.io (attune) vs Docker Hub (postgres/
      prom/grafana, whose CDN is what stalls); fix = **retry (`docker compose pull`
      resumes)** or a registry mirror. *(Firsthand.)*

## Alternatives considered

- **README as the only doc / pure stub** — rejected: a `deploy/`-local user wants
  the 3 commands right there; pure-stub adds a hop. Thin-README + canonical-guide is
  the top-repo norm.
- **nginx + Let's Encrypt** (issue's literal wording) — rejected in favor of Caddy:
  auto-TLS is materially simpler for the zero-ops audience (the issue's own goal).
  nginx example can be a follow-up if demanded.
- **Caddyfile without a runnable overlay** — rejected: a config file you can't run
  isn't "copy-pasteable." The tiny tls overlay makes it real, reusing the `-f` pattern.
- **Cover per-tenant module config inline** — rejected: #10 owns it; pointer only.

## Risks / tradeoffs

- **Doc rot** — commands drift as the stack evolves. Mitigation: every command is
  live-verified now; the obs/tls overlays are CI-`config`-checked (#6 pattern — add
  the tls overlay to that job too).
- **Caddy deviates from the issue's "nginx"** — intentional (usability); flagged.
- **Section 6/7 depend on #6 + the new tls overlay** — #7 lands with/after #6 (PR #57).
- **`docs/` is changelog-exempt** — no CHANGELOG entry (CLAUDE.md §2); called out in
  the PR.

## Implementation plan

1. `deploy/Caddyfile.example` + `deploy/docker-compose.tls.yml` (caddy service);
   live-verify `-f … -f docker-compose.tls.yml config` + a real `up` against a test
   domain or `localhost` (self-signed/internal).
2. `docs/private-deploy.md` — write all 10 sections; **run every command** and paste
   real output.
3. Slim `deploy/README.md` (quickstart + links); add the root `README.md` link.
4. Extend the CI `compose-config` job to also validate the tls overlay merge.
5. Verify (below). PR `docs(deploy): …`, **`Closes #7`** (changelog-exempt).

## Verification

- **`scripts/smoke-deploy.sh`** — the executable form of §4–5 + §9 — builds attune
  from source, brings the stack up with a mock LLM, and asserts
  healthz → tenant → key → ingest → enrich (`status=done`) → backup. Wired into CI
  as the `deploy-smoke` job (gated on `deploy/**` or `go` changes) — the doc-rot
  guard. **Verified green locally end-to-end** (all 6 steps).
- Caddy TLS overlay: `config -q` parses; Caddy comes up and reverse-proxies attune
  (`via: 1.1 Caddy`). Public-domain ACME issuance is environment-dependent —
  documented, not CI-tested.
- Root README links `docs/private-deploy.md`; 6 troubleshooting entries, each a
  **real captured error** (LLM 401, port-in-use, DSN parse, …).
- Every command/output in the guide was run on a real cold deployment (incl. the
  corrected findings: first-boot timing, the `:v0.2.0` pin gap, and the api-key
  "bug" that was a harness artifact).

## References

- #5 deploy kit · #6 obs overlay (PR #57) · #10 module config.
- Benchmark: Sentry self-hosted, Plausible CE, Immich, Supabase self-hosting docs
  (README-thin + canonical-guide; Caddy/Traefik auto-TLS).
- Live run: healthz/boot-log/tenant+key/ingest/metrics/backup all captured during #6.
