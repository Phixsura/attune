# Security & Reliability Hardening — Operator & Integrator Guide

This guide covers the production hardening shipped in the P0/P1 batch
([#84](https://github.com/Phixsura/attune/issues/84),
[#81](https://github.com/Phixsura/attune/issues/81)): outbound SSRF protection,
trusted-proxy client-IP resolution, worker panic supervision, request/query
timeouts, at-least-once webhook delivery with consumer dedup, and the new
observability signals.

It is written for three audiences:

- **Operators / SRE** deploying attune (the `config.yaml` knobs, what to watch).
- **Security / compliance** (the threat model these controls address).
- **Integrators** who consume attune's outbound webhooks.

---

## 1. Outbound egress / SSRF protection

attune makes outbound HTTP calls to **tenant-controlled** destinations: customer
webhooks, LLM provider base URLs, and IMAP servers. Without a guard, a tenant
could point any of these at the cloud metadata endpoint
(`169.254.169.254`) or an internal service and exfiltrate credentials or pivot
inside your network. attune now enforces an egress policy **at dial time** —
*after* DNS resolution, so it also defeats DNS rebinding.

### What is always blocked (not configurable)

Regardless of config, these destinations are refused on every outbound dial:

- **Cloud metadata** — `169.254.169.254`, `169.254.170.2`, `100.100.100.200`
  (Alibaba), `metadata.google.internal`, etc.
- **Link-local** — `169.254.0.0/16`, `fe80::/10`.
- **Unspecified / multicast** — `0.0.0.0`, `::`, `224.0.0.0/4`.
- **IPv6 transition wrappers** of any of the above — 6to4 (`2002::/16`), NAT64
  (`64:ff9b::/96`), Teredo (`2001:0000::/32`), and IPv4-compatible
  (`::a.b.c.d`). The embedded IPv4 is unwrapped and re-checked.

### What you can opt into (`security:` block)

```yaml
security:
  # Allow outbound dials to loopback (127.0.0.0/8, ::1, localhost).
  # Off by default. Turn on for local dev, or when a loopback reverse-proxy
  # fronts your LLM gateway. Never enable in a public-facing deploy unless you
  # understand it re-permits SSRF-to-loopback.
  allow_loopback_egress: false

  # Allow outbound dials to RFC1918 / unique-local (10/8, 172.16/12,
  # 192.168/16, fc00::/7). Turn on for on-prem deployments where attune is
  # co-located with an internal IMAP server or self-hosted LLM gateway.
  allow_private_egress: false

  # See §2 (trusted proxies).
  trusted_proxy_hops: 0
```

Defaults are **fail-closed**: an absent `security:` block blocks loopback *and*
private networks. Existing deployments that dial only public HTTPS endpoints are
unaffected; deployments that rely on an internal LLM/IMAP host must set
`allow_private_egress: true`.

### Where the guard applies

| Path | Guard |
|---|---|
| Webhook delivery (outbox) | dial-time, per `security.*` |
| Console **"Test webhook"** button | dial-time, per `security.*` |
| LLM provider calls (all backends) | dial-time, per `security.*` |
| LLM `base_url` (create/update channel) | **config-time** literal-IP check (metadata/link-local always rejected; loopback/private allowed at config time and governed by the dial-time policy at runtime) |
| Inbound email IMAP | dial-time; loopback + RFC1918 always allowed here (on-prem IMAP), metadata/link-local always blocked |

### Caveats

- **HTTP(S)_PROXY is intentionally ignored** on these egress paths. A forward
  proxy would make the dialer connect to the *proxy* IP, so the guard would only
  ever see the proxy and never the real target — silently defeating SSRF
  protection. If you require an egress proxy, it must do its own SSRF filtering.
- The config-time `base_url` check is best-effort early feedback; the dial-time
  guard is the authoritative, rebinding-proof enforcement.

### What you'll see

A blocked dial fails with, e.g.:

```
nethardening: refused outbound dial to 169.254.169.254 (link-local (cloud metadata range))
```

The Console "Test webhook" button surfaces this as a `502` with the reason.

---

## 2. Trusted proxies & client IP (`trusted_proxy_hops`)

attune resolves the **client IP** for two purposes: the per-API-key IP allowlist
and the audit-log `actor_ip`. Both now use a single trusted-proxy model.

```yaml
security:
  # Number of reverse proxies in front of attune that append to
  # X-Forwarded-For. The client IP is read that many hops from the right.
  #   0 (default) → ignore X-Forwarded-For entirely, use the direct TCP peer.
  #   N           → trust the rightmost N hops; the client is entry (len - N).
  trusted_proxy_hops: 0
```

- **Direct exposure (no proxy):** leave it `0`. `X-Forwarded-For` is ignored, so
  a client on a direct connection **cannot forge an allowlisted source IP**.
- **Behind one reverse proxy** (nginx, a single k8s ingress): set `1`.
- **Behind two** (e.g. CDN → ingress): set `2`. An attacker prepending extra
  `X-Forwarded-For` entries can't shift the result — only the rightmost N hops
  (added by infrastructure you control) are trusted.

Notes:

- Only `X-Forwarded-For` is consulted (single-header model). If your proxy emits
  only `X-Real-IP`, configure it to also append `X-Forwarded-For`.
- chi's `middleware.RealIP` is intentionally **not** used (it would
  unconditionally trust the header and reopen the spoof).
- `audit_log.actor_ip` records the resolved client IP, so set
  `trusted_proxy_hops` correctly if you rely on audit attribution behind a proxy
  — otherwise it records the proxy's IP.

---

## 3. Consuming attune webhooks (integrators)

attune delivers enriched feedback and daily digests to your HTTPS endpoint
**at least once** (it retries until your endpoint 2xx's or the row dead-letters).

### Dedup with `X-Attune-Delivery-Id`

Every raw-webhook delivery carries a stable `X-Attune-Delivery-Id` header (the
outbox row id). It is **constant across retries** of the same delivery, so a
delivery that succeeds on your side but whose ack is lost (and is therefore
retried) arrives with the same id. **Dedup on it** to make your receiver
idempotent:

```
POST /your/hook
X-Attune-Signature: sha256=<hmac>
X-Attune-Delivery-Id: 4213
Content-Type: application/json
```

### Verify the signature

`X-Attune-Signature` is an HMAC-SHA256 over the body (or content-hash, per the
target's `signature_version`) using the shared secret you configured. Verify it
before trusting the payload.

### Operator log hygiene

attune logs outbound deliveries with the URL **redacted to `scheme://host`** and
the body as a byte count only — webhook URLs routinely carry secret tokens in
the path/query, and bodies carry user content. Don't expect full URLs or bodies
in attune's logs.

---

## 4. Reliability

### Worker panic supervision

Every background worker (outbox, enrichment, embedding, reply-draft, digest,
GDPR export, audit pruner, queue/lag refreshers) runs under a supervisor that
**recovers panics**, counts them in `attune_worker_panics_total{worker}`, and
restarts the worker with capped backoff. A single panic — e.g. a malformed LLM
response — no longer crashes the whole process. The enrichment path additionally
recovers per-job so one bad row can't stop the processor.

### Timeouts

- **DB pool** (`database.NewPool`): `MaxConns=20`, `connect_timeout=10s`,
  `statement_timeout=30s` — applied unless your `DATABASE_URL` already sets them
  (URL params win). A stuck query can't pin a connection indefinitely.
  Migrations are exempt (they run with `statement_timeout=0`); a long ad-hoc
  query through this pool will be cancelled at 30s — override via the URL param
  if you need longer.
- **HTTP server**: `ReadHeaderTimeout=10s`, `ReadTimeout=60s`,
  `WriteTimeout=315s` (above the 305s in-handler timeout), `IdleTimeout=120s` —
  closes slow-loris and slow-reader exposure.

### Outbox under multiple replicas

The outbox worker is safe to run as multiple replicas: `ClaimBatch` excludes
rows claimed within a 10-minute window, the active worker renews its claims
(owner-scoped lease heartbeat via the `claimed_by` column) while draining a
batch, and a crashed worker's rows are retried after the window. Combined with
`X-Attune-Delivery-Id` (consumer dedup), horizontal scaling won't double-deliver.

---

## 5. Observability

New signals (all under the `attune_` Prometheus namespace, documented in
[`observability/README.md`](../observability/README.md)):

| Metric | Meaning |
|---|---|
| `attune_enrichment_terminal_failures_total{tenant}` | feedback rows that exhausted enrichment retries and stopped in `failed` (#81) |
| `attune_worker_panics_total{worker}` | recovered panics in supervised workers |

Alerts (with runbooks in [`observability/runbooks.md`](../observability/runbooks.md)):

- **`AttuneEnrichmentTerminalFailures`** — rows are silently failing enrichment;
  sample their `enrichment_error`, fix the provider/prompt/parse cause, re-enqueue.
- **`AttuneWorkerPanics`** — a worker is panic-looping; grep the worker label for
  the recovered-panic stack and fix the underlying bug.

Both metrics appear on the bundled Grafana dashboards (AI Pipeline / Operations).

---

## See also

- [`docs/private-deploy.md`](private-deploy.md) — full private-deployment quickstart.
- [`config.example.yaml`](../config.example.yaml) — annotated config with the `security:` block.
- [`observability/README.md`](../observability/README.md) — full metrics catalog.
