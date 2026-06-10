# Guardrails

Attune treats guardrails as a policy layer around AI and outbound boundaries.
PII redaction is the first built-in guard, but the model is intentionally
broader: future guards can handle secrets, prompt injection, output leakage,
tool calls, and outbound delivery.

## Principles

- Raw feedback stays in Postgres. Guards transform or block what leaves attune,
  not the canonical `user_feedback.content` record.
- Policies are managed in the database and, over time, in the Console. YAML may
  seed bootstrap defaults, but it is not the long-term source of truth.
- Matching is source-aware. A support mailbox, public webhook, RSS feed, and
  agent/MCP source can resolve different effective policies.
- Findings are safe summaries. Logs, metrics, Console views, webhooks, and
  outbox payloads must not include raw matched PII.

## Policy Model

Guard policies are rulesets. At runtime attune collects every enabled policy
matching the LLM request metadata and resolves an effective plan:

```text
system default
  -> tenant default
    -> channel default
      -> source override
```

That inheritance ladder is the user-facing mental model. The implementation is
ruleset-based so multiple policies can match one request.

For `default` and `override` policies, narrower matching targets win over
broader targets. This lets a tenant default relax the system default, and a
source override relax a channel or tenant default. `baseline` policies are the
mandatory floor: if a baseline is stricter than the selected default/override,
the baseline wins.

Policy kinds:

| Kind | Meaning |
|---|---|
| `baseline` | Mandatory floor. Narrower overrides cannot relax it. |
| `default` | Recommended behavior. Narrower overrides may relax it. |
| `override` | Explicit exception for a narrower target, usually a source. |

Actions, from most restrictive to least restrictive:

```text
block > redact > hash > tokenize > audit > off
```

The current implementation supports `off`, `audit`, `redact`, and `block`.
`hash` and `tokenize` are reserved for future work.

`off` is preserved in effective-policy previews so the Console can distinguish
"explicitly disabled here" from "no policy matched." Runtime guards treat `off`
as a no-op.

`priority` is a tie-breaker only after target specificity. It is not a general
policy language and should not be presented as arbitrary rule ordering in the
Console.

## Stages

| Stage | Purpose |
|---|---|
| `llm_input` | Before sending prompt text to an LLM provider. |
| `llm_output` | After the LLM responds. |
| `outbound` | Before sending content to an external destination. |
| `tool_call` | Before an agent/tool action executes. |

The first shipped guard applies PII policy at `llm_input`.

## Built-in PII Guard

The built-in PII guard detects:

- `email`
- `phone`
- `cn_mobile`
- `cn_id`
- `credit_card`

Credit-card detection uses Luhn validation. Regex detectors are intentionally
documented as a conservative local baseline, not a perfect compliance scanner.
Future policy providers may add external detectors such as Presidio-compatible
services without making them required dependencies.

The migration seeds a system default policy that redacts these entities for
`purpose=enrich` and `stage=llm_input`. It is a `default`, not a `baseline`, so
tenants or sources can explicitly turn it off when they run a fully trusted local
LLM path.

## Console API

The Console backend exposes tenant-scoped policy management under
`/fb/v1/console/guard-policies`:

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/guard-policies` | List matching system and tenant policies. |
| `POST` | `/guard-policies` | Create one tenant policy. |
| `PATCH` | `/guard-policies/{id}` | Replace one tenant policy by ID. |
| `DELETE` | `/guard-policies/{id}` | Delete one tenant policy by ID. |
| `PUT` | `/guard-policies` | Replace the current tenant's policy rows. System policies are read-only. |
| `POST` | `/guard-policies/effective` | Preview the effective rules for tenant, channel, source, tags, purpose, and environment metadata. |

The v1 Console API accepts only the built-in `pii` guard and the implemented
`off`, `audit`, `redact`, and `block` actions. `hash` and `tokenize` remain
reserved until their execution semantics exist.

Inbound source identity is server-asserted. Public API-key ingest cannot set
`source_meta.inbound_source_id`; reserved inbound-source metadata is stripped
before persistence unless the row comes from a trusted server-side adapter path.
`sourceTags` matching uses AND semantics: all tags in a policy target must be
present on the request/source.

## Operational Notes

- Guard action metrics are exported as
  `attune_guard_actions_total{tenant,stage,guard,entity,action}`.
- Block decisions are exported as
  `attune_guard_blocked_total{tenant,stage,guard,reason}`.
- Labels are bounded. Policy names, source names, and raw findings are not metric
  labels.
- A blocked `llm_input` guard prevents the provider call. The enricher marks the
  row failed through its existing failure path.

## Roadmap

- Console-managed Guard Policies page that consumes the existing API.
- Inbound Source "Guards" tab showing effective inherited policy.
- Source tags for policy targeting, such as `public`, `support`,
  `regulated`, or `agent`.
- Audit-log entries for policy create/update/delete and safe policy-match
  summaries.
- Additional guards: secrets, prompt injection, output leakage, and tool-call
  policy.

See the design proposal:
[`2026-06-10-llm-guard-policies.md`](proposals/2026/06/2026-06-10-llm-guard-policies.md).
