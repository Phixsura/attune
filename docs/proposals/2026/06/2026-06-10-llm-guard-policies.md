# Source-aware LLM guard policies, with PII redaction as the first guard

| Field | Value |
|---|---|
| Issue | #20 |
| Status | Implemented |
| Started | 2026-06-10T13:35:00+08:00 |
| Related | #66 (channel-agnostic inbound), #19 (proto contract), #10 (metadata-driven enrichment), #12 (PostgreSQL integration tier), #39 (future audit log), #94 (secret-store key rotation), #95 (source registry follow-up) |

## Problem

Issue #20 asks for PII redaction before sending feedback to the LLM. The core
risk is valid: feedback can contain email addresses, phone numbers, government
IDs, credit cards, API keys, contracts, or private customer context. Sending
that raw content to a third-party LLM is a compliance and procurement blocker
for private-deployment customers.

However, the issue text predates the current `main` architecture:

- The enricher now lives under `internal/service/enrich`, not
  `internal/service/pii_redactor.go`.
- The LLM boundary is provider-agnostic through `llmclient.LLMClient`, with
  OpenAI-compatible, OpenAI Responses, Anthropic, and Gemini backends.
- #66 made inbound source rows first-class. Future sources include email,
  webhook, RSS, scraper, social, MQ, Chinese platforms, and agent/MCP sources.
- Per-source risk differs. A support mailbox, public webhook, RSS feed, and MCP
  agent stream should not share one hard-coded redaction behavior.
- A YAML boolean such as `enricher_pii_redact` would give deployers a switch,
  but it would not give tenant admins or source owners a governance surface in
  the Console.

The right abstraction is not "PII redactor inside the enricher." It is a
policy-driven LLM guard layer, managed in the Console and resolved per tenant,
channel, source, purpose, and stage. PII redaction is the first built-in guard.

## Goals

- Add a reusable LLM guard framework at the `llmclient.LLMClient` boundary so
  every LLM backend receives the same protections.
- Preserve raw feedback in Postgres while sending guarded text to the LLM.
- Make PII behavior configurable and explicitly disableable by policy.
- Model guard configuration as Console-managed policies, not long-lived YAML.
- Support source-aware policy targeting across tenant, channel, source, and
  source tags.
- Support both mutable guards, such as redact or hash, and validating guards,
  such as block.
- Make policy resolution explainable: operators can see which policies matched
  and why an action was taken.
- Keep sensitive findings out of logs, metrics labels, and persistent audit
  payloads. Store counts and entity/action summaries only.
- Ship PII as the first guard without closing the door on secrets, prompt
  injection, output leakage, tool-call policy, or data-exfiltration guards.

## Non-goals

- Do not build a general-purpose policy language or embed OPA/Rego in v1.
- Do not add Presidio, a Python sidecar, or a heavyweight external detector as a
  required dependency.
- Do not replace the existing metadata-driven Dimensions classifier.
- Do not redact the canonical `user_feedback.content` column. The source of
  truth remains the original user-submitted content.
- Do not expose raw PII findings in the Console, logs, metrics, webhooks, or
  outbox payloads.
- Do not make every future source/channel policy UI complete in the first PR.
  The first implementation should establish the storage and resolution model.
- Do not rely on YAML as the long-term policy store. YAML may seed bootstrap
  defaults only.

## Design

### Borrowed governance model

This proposal borrows proven policy ideas instead of inventing a one-off guard
configuration model:

- GitHub Rulesets: named, enabled policies target resources and can be explained
  in the UI.
- Kubernetes Admission Control: mutating steps run before validating steps.
- AWS IAM policy evaluation: explicit restrictive rules win over permissive
  rules, and a baseline cannot be relaxed by a lower-level override.
- OPA's PDP/PEP split: business code enforces a resolved decision instead of
  scattering policy logic across call sites.

Attune should not copy any one product's field names. It should adopt these
structural principles.

### Policy model

The user-facing mental model is:

```text
system default
  -> tenant default
    -> channel default
      -> source override
```

The implementation should be a ruleset model:

```text
collect all enabled policies matching the LLM request
  -> resolve an effective guard plan
  -> execute guard stages
```

This avoids a brittle single-parent inheritance chain and supports multiple
policies matching the same source, such as a tenant privacy baseline plus an
email-channel default plus a support-mailbox override.

### Policy kinds

Policies have one of three kinds:

| Kind | Meaning | Can lower scopes relax it? |
|---|---|---|
| `baseline` | Mandatory floor, usually set by system or tenant security owner. | No |
| `default` | Recommended behavior for a scope. | Yes |
| `override` | Explicit exception for a narrower target, often a source. | N/A |

Example:

```text
tenant baseline: credit_card -> block
source override: credit_card -> off
effective:        credit_card -> block
```

But:

```text
tenant default: email -> redact
source override: email -> off
effective:       email -> off
```

### Rule actions

Actions are ordered from most restrictive to least restrictive:

```text
block > redact > hash > tokenize > audit > off
```

`hash` and `tokenize` are reserved for future work. The first implementation can
support `off`, `audit`, `redact`, and `block`.

### Guard stages

Guard stages are explicit:

| Stage | Purpose |
|---|---|
| `llm_input` | Before sending prompt text to an LLM provider. |
| `llm_output` | After the LLM responds, before parsing/persisting or using output. |
| `outbound` | Before sending enriched content to external destinations. |
| `tool_call` | Before an agent/MCP/tool action is allowed to execute. |

Issue #20 ships the `llm_input` PII guard first. The framework should model the
other stages now so future PRs do not have to rewrite the policy schema.

### Targets

Policies target request metadata:

```json
{
  "tenantId": "tenant-uuid",
  "channels": ["email"],
  "sourceIds": ["source-uuid"],
  "sourceTags": ["support", "regulated"],
  "purposes": ["enrich"],
  "stages": ["llm_input"],
  "environments": ["prod"]
}
```

All fields are optional. Empty target means "all requests in this policy's
tenant/system scope." `sourceTags` are important because future deployments may
have dozens of sources that should share a policy.

### Policy storage

Add a first-class policy table:

```sql
CREATE TABLE guard_policies (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   TEXT REFERENCES tenants(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    kind        TEXT NOT NULL,
    enabled     BOOLEAN NOT NULL DEFAULT TRUE,
    priority    INT NOT NULL DEFAULT 100,
    target      JSONB NOT NULL DEFAULT '{}'::jsonb,
    rules       JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_by  TEXT,
    updated_by  TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

`tenant_id IS NULL` is reserved for system policies. Tenant admins manage
tenant-scoped policies. System policies are seeded by migration or bootstrap and
managed by operators.

Add source tags separately or as a future migration:

```sql
ALTER TABLE inbound_sources
    ADD COLUMN IF NOT EXISTS tags TEXT[] NOT NULL DEFAULT '{}';
```

Long term, make source identity first-class on feedback rows:

```sql
ALTER TABLE user_feedback
    ADD COLUMN IF NOT EXISTS inbound_source_id UUID REFERENCES inbound_sources(id);
```

The first PR may continue reading `source_meta.inbound_source_id` if adding this
column would make the change too large, but the proposal recommends the column
because guard policy resolution should not depend on JSON parsing forever.

### Rule schema

Rules are typed JSON so the Console can render structured controls rather than
raw JSON:

```json
[
  {
    "guard": "pii",
    "stage": "llm_input",
    "entities": ["email", "phone"],
    "action": "redact",
    "replacement": "<PII:{entity}>"
  },
  {
    "guard": "pii",
    "stage": "llm_input",
    "entities": ["credit_card", "cn_id"],
    "action": "block"
  }
]
```

For the first PII guard, supported entities are:

- `email`
- `phone`
- `cn_mobile`
- `cn_id`
- `credit_card`

`credit_card` must use Luhn validation. Regex-only card detection is too noisy.

### Execution architecture

Add a guard package outside `service/enrich`:

```text
internal/infra/llmguard/
  client.go       -- LLMClient wrapper
  policy.go       -- policy shapes + resolver interface
  pii.go          -- built-in PII guard
```

The wrapper implements `llmclient.LLMClient`:

```go
type Client struct {
    next     llmclient.LLMClient
    resolver PolicyResolver
    guards   Registry
}

func (c *Client) Complete(ctx context.Context, req llmclient.CompletionRequest) (llmclient.CompletionResponse, error)
```

The wrapper resolves policy from request metadata, applies `llm_input` guards to
`req.Prompt`, calls the backend, then applies `llm_output` guards to the
response if configured.

Default and override conflicts are resolved by target specificity before
priority: tenant defaults can relax system defaults, channel defaults can relax
tenant defaults, and source overrides can relax broader defaults. Baselines are
mandatory floors and win whenever they are stricter than the selected
default/override.

`CompletionRequest` should gain optional metadata:

```go
type GuardMetadata struct {
    TenantID        string
    Channel         string
    SourceID        string
    SourceTags      []string
    Purpose         string // enrich, digest, reply_draft, eval, outbound
    Environment     string // dev, staging, prod
}
```

This is clearer than hiding policy context in `context.Context`.

### Enricher integration

`service/enrich` should pass metadata into the LLM request:

- `TenantID` from `ClassifyConfig`.
- `Purpose = "enrich"`.
- `Channel` and `SourceID` from the feedback row when available.

The raw row content still enters `renderPrompt`. The guard wrapper modifies the
final prompt immediately before the provider call. This ensures custom prompt
templates are guarded too.

Inbound source identity is trusted only when asserted by server-side adapters.
Public API-key ingest strips reserved inbound-source metadata before persistence
so clients cannot attach themselves to a source-specific override.

### Explainability and audit

Each guarded call produces a safe summary:

```json
{
  "stage": "llm_input",
  "matchedPolicies": [
    "System privacy baseline",
    "Email channel default",
    "Support mailbox strict"
  ],
  "actions": [
    {"guard": "pii", "entity": "email", "action": "redact", "count": 2},
    {"guard": "pii", "entity": "credit_card", "action": "block", "count": 1}
  ],
  "finalDecision": "block"
}
```

The summary may be logged or audited. It must not include matched substrings or
raw redacted values. A future #39 audit log can persist these summaries with
actor/request metadata.

### Metrics

Add bounded-label metrics:

```text
attune_guard_actions_total{tenant,stage,guard,entity,action}
attune_guard_blocked_total{tenant,stage,guard,reason}
```

Labels must stay finite. Do not include source slug, source name, policy name,
or raw error text unless a separate cardinality review approves it.

### Console experience

The Console should eventually expose:

1. Guard Policies page
   - Named policies.
   - Kind: baseline/default/override.
   - Target selectors.
   - Rule controls.
   - Enabled toggle.
   - Last matched / blocked counts.

2. Inbound Source Guards tab
   - Effective policy view for that source.
   - "Inherited from tenant default" / "Inherited from channel default" labels.
   - Source-local override controls.
   - Test text box that shows safe findings and transformed text.

3. Tenant default policy
   - A simple "Privacy mode" preset for non-expert admins:
     - Off
     - Audit
     - Redact PII
     - Strict regulated

The first PR may expose only a minimal tenant/source PII mode control if the full
policy UI is too large, but the storage and resolver should already match this
model.

### YAML role

YAML is only for bootstrap:

```yaml
guard_bootstrap:
  system_default: privacy
```

or:

```yaml
guard_bootstrap:
  seed_builtin_policies: true
```

After bootstrap, the DB and Console are the source of truth. Operators can still
disable all guards via system policy if they want a fully local/trusted LLM path.

## Alternatives considered

### `enricher_pii_redact: bool`

Rejected. It solves only one issue in one business path. It cannot express
audit/block, output guards, source-specific behavior, or tenant-admin control
through the Console. It would be rewritten as soon as webhook/email/RSS/MCP
source policies matter.

### Per-tenant `pii_redact_enabled` column only

Rejected as the primary model. It is better than a YAML-only switch, but it
still cannot distinguish public webhooks from support mailboxes, market-intel
feeds, and agent sources. It also cannot express future guards.

### Store guard config inside `inbound_sources.config`

Rejected. That column is channel-adapter configuration and encrypted because it
contains secrets/credentials. Guard policy is cross-channel governance and
should be queryable, explainable, and managed independently from adapter secrets.

### Embed OPA/Rego

Deferred. OPA's policy-decision model is useful, but Rego is too much surface
area for the first guardrail version and hard to make friendly in the Console.
Typed rules cover the current product needs while leaving room for a future
advanced-policy backend.

### Delegate all PII detection to LiteLLM, Portkey, or another gateway

Rejected as the only implementation. Attune should work with any LLM backend,
including direct SDK clients and local deployments. External gateways remain a
valid operator choice, but Attune still needs first-party controls for
self-hosted trust.

### Redact before storing feedback

Rejected. The acceptance requirement is DB original, LLM guarded. Operators need
the original record for support, audit, legal export, and dedup/debug flows.

## Risks / tradeoffs

- Regex PII detection has false positives and false negatives. Mitigate with a
  curated positive/negative corpus, Luhn validation for cards, conservative
  phone boundaries, and audit mode.
- Source-aware resolution needs source metadata in the LLM request. If
  `inbound_source_id` stays only in `source_meta`, the first implementation is
  less clean. A first-class column is preferred.
- A full policy UI can grow large. The first PR should keep UI minimal while
  preserving the backend model.
- Blocking guards can stop enrichment. The row should mark failed with a stable
  guard error code rather than retrying forever.
- Policies are security-sensitive configuration. Future #39 audit logging should
  record create/update/delete operations and policy-match summaries.
- Applying output guards after LLM response can break strict JSON parsing if a
  mutating guard changes structure. For v1, output guard may be audit/block only
  unless the implementation can safely mutate text before parse.

## Implementation plan

1. Add this proposal and align issue #20 around guard policies instead of a
   single redaction boolean.
2. Add DB schema for `guard_policies`; optionally add `inbound_sources.tags` and
   `user_feedback.inbound_source_id` if the scope stays manageable.
3. Add domain/config structs for policy target, rule, action, kind, and stage.
4. Add a policy resolver that collects matching policies and produces an
   effective per-stage guard plan.
5. Add `internal/infra/llmguard` with a `llmclient.LLMClient` wrapper.
6. Add the built-in PII guard:
   - email regex,
   - Chinese mobile,
   - international phone with conservative boundaries,
   - Chinese ID,
   - credit card with Luhn validation.
7. Wire the guarded client in `cmd/attune` so every LLM backend is protected by
   the same wrapper.
8. Pass guard metadata from `service/enrich` into `CompletionRequest`.
9. Add Console API support for listing/replacing tenant policies and previewing
   effective policies. Defer the full policy editor UI to a follow-up while
   preserving the same storage and resolver model.
10. Add metrics and safe summaries.
11. Update docs and `CHANGELOG.md` under `### Added` and `### Security`.
12. Harden the management path:
   - Console list shows disabled policies, while runtime resolution only uses
     enabled policies.
   - Policy validation limits target fields and replacement templates.
   - `off` remains visible in effective-policy previews.

## Verification

- Unit tests for policy matching and resolution:
  - system baseline plus source override,
  - default relaxed by source override,
  - baseline not relaxed by override,
  - action precedence.
- Unit tests for PII detection:
  - positive samples for each entity,
  - negative samples for SKUs, order numbers, dates, IPv4-like strings, and
    ordinary 10-19 digit non-card values,
  - Luhn card detection.
- Unit tests for the LLM guard wrapper:
  - redacts prompt before calling the backend,
  - audit mode does not mutate prompt,
  - block mode does not call the backend,
  - output guard path is invoked when configured.
- PostgreSQL integration test:
  - ingest source content with PII,
  - run `Enricher.EnrichOne` with fake LLM,
  - assert fake LLM sees redacted prompt,
  - assert `user_feedback.content` remains original.
- Benchmark:
  - PII redaction throughput is at least 10 MB/s on one core for typical
    feedback-sized text.
- Safety checks:
  - no raw PII in logs,
  - metrics labels are bounded,
  - `go test ./...`, `go vet ./...`, and `make test-integration` relevant suites
    pass.

## References

- LiteLLM guardrails and PII masking:
  <https://docs.litellm.ai/docs/proxy/guardrails/quick_start>,
  <https://docs.litellm.ai/docs/proxy/guardrails/pii_masking_v2>
- Portkey guardrails and PII redaction:
  <https://docs.portkey.ai/docs/product/guardrails>,
  <https://docs.portkey.ai/docs/product/guardrails/pii-redaction>
- Amazon Bedrock Guardrails sensitive information filters:
  <https://docs.aws.amazon.com/bedrock/latest/userguide/guardrails-sensitive-filters.html>
- Google Model Armor:
  <https://cloud.google.com/security/products/model-armor>
- LangChain guardrails middleware:
  <https://docs.langchain.com/oss/javascript/langchain/guardrails>
- OpenAI Guardrails PII checks:
  <https://openai.github.io/openai-guardrails-python/ref/checks/pii/>
- Microsoft Presidio:
  <https://microsoft.github.io/presidio/supported_entities/>
- Kubernetes admission controllers:
  <https://kubernetes.io/docs/reference/access-authn-authz/admission-controllers/>
- AWS IAM policy evaluation:
  <https://docs.aws.amazon.com/IAM/latest/UserGuide/reference_policies_evaluation-logic.html>
- GitHub Rulesets:
  <https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/managing-rulesets/about-rulesets>
- Open Policy Agent:
  <https://www.openpolicyagent.org/docs/latest/>
