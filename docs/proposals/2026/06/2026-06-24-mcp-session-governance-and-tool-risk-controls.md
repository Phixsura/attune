# MCP session governance and per-tool risk controls

| Field | Value |
| --- | --- |
| **Issue** | [#153](https://github.com/Phixsura/attune/issues/153) |
| **Status** | Implemented (P0 core, P1 protocol compatibility hardening, P1 connection workspace) |
| **Started** | 2026-06-24 |
| **Related** | [#93](https://github.com/Phixsura/attune/issues/93) (MCP server + OAuth 2.1 base), [#39](https://github.com/Phixsura/attune/issues/39) (audit log foundation), [#41](https://github.com/Phixsura/attune/issues/41) (scopes / auth posture), [#149](https://github.com/Phixsura/attune/issues/149) (system readiness surface), [2026-06-20-mcp-server.md](./2026-06-20-mcp-server.md) |

## Problem

attune already ships a functional MCP server with OAuth 2.1, PKCE, per-client
registration, refresh tokens, session IDs, scope checks, and audit events for
tool execution. That is enough for protocol compatibility. It is not yet enough
for enterprise governance.

The issue is not missing MCP capability; it is missing the control plane around
that capability.

Concretely, the current implementation has four gaps:

- **Visibility is client-level only.** Console admins can list and revoke MCP
  clients, but cannot see active sessions, refresh-token grants, or recent
  activity per session. The runtime already persists `mcp_sessions` and
  `mcp_oauth_refresh_tokens`, but operators cannot inspect or act on them.
- **Revocation is too coarse.** Today the supported administrative action is
  "revoke the whole client", which also revokes all tokens and closes all
  sessions. That is safe, but operationally expensive. A single suspicious
  session should be terminable without deleting the entire agent registration.
- **Authorization is too coarse.** Runtime enforcement is currently scope-only:
  `mcp:read`, `mcp:write`, `mcp:ingest`. That means a client granted
  `mcp:write` can call every write tool. There is no per-tool allowlist, no
  deny rule, no risk metadata, and no way to express "this client may update
  workflow state but must not modify tags".
- **Audit context is incomplete for incident response.** Tool calls are audited,
  but authorization denials, session-level last activity, per-tool policy
  decisions, and rate-limit denials are not modeled as first-class governance
  events. A responder cannot reconstruct "which session tried to do what, under
  which policy, and why it was allowed or denied" from one place.

One current implementation detail makes the visibility gap worse:
`mcp_sessions.last_active_at` is refreshed on refresh-token rotation, not on
every authenticated tool invocation. The field is therefore closer to "last
grant refresh" than to "last observed MCP activity". That is insufficient for
the issue acceptance criterion "Console shows enough context to investigate
agent activity."

This is exactly the point where protocol-complete systems diverge from
production-ready systems. Industry-leading agent and app platforms do not stop
at OAuth:

- GitHub Apps layer repository/account permissions, installation scoping,
  approval on privilege increase, and searchable audit logs on top of OAuth and
  tokens.
- Stripe restricted keys layer per-resource permissions and network
  restrictions on top of API credentials.
- Google Cloud's remote MCP guidance layers IAM, audit logging, and toolset
  governance on top of MCP connectivity.
- Cloudflare and Okta distinguish between client registration, token lifecycle,
  session lifecycle, and audit/forensics.

Issue #153 is the point where attune needs the same split: **transport and
token issuance stay in the MCP layer; operational governance becomes its own
control plane.**

## Goals

- Add an **admin-visible MCP access plane** with enough context to answer:
  which clients are active, which sessions are open, which grants are valid,
  what each session last did, and what policy currently applies.
- Support **single-session termination** without revoking the entire client.
- Add **per-client tool authorization** with default allow-all migration
  behavior so existing clients continue to work after rollout.
- Add **per-tool risk metadata** in a single runtime registry that is used for
  authorization decisions, rate-limit defaults, UI labeling, and audit facets.
- Add **per-client and per-tool rate-limit controls** without changing the
  transport contract exposed to MCP consumers.
- Make **authorization denials and rate-limit denials auditable**, with enough
  structured detail for incident response.
- Make session activity **accurate to actual MCP requests**, not just OAuth
  refresh operations.
- Add a **policy rollout path** that supports observe-only / dry-run evaluation
  before enforcement so administrators can harden existing clients without
  guessing.
- Add a **server-side approval / step-up model hook** for future high-risk tools
  so attune does not paint itself into a scope-only corner.
- Add an **audit export surface** suitable for SIEM ingestion or long-retention
  governance evidence, even if the first sink is simple.
- Add **tenant-isolated unit and integration coverage** for policy evaluation,
  session revocation, allowlist enforcement, rate limits, and Console/admin API
  behavior.

## Non-goals

- **Replacing the existing OAuth 2.1 Authorization Server.** The token-issuing
  path from #93 remains the foundation. This issue governs what an authorized
  client can do after token validation.
- **Redesigning the current tool catalogue.** The current tools remain the same
  in behavior and naming. This issue adds metadata and enforcement around them.
- **Adding a host-side approval UX.** Research shows host approvals and step-up
  flows are part of world-class agent governance, but issue #153 does not
  require a Console-mediated challenge flow. The design keeps space for it
  without making it a dependency.
- **Introducing a new distributed rate-limit backend.** The design keeps the
  limiter pluggable, but the first implementation remains an in-process
  limiter consistent with the repository's existing operational model.
- **Adding MCP Resources, Prompts, or non-HTTP transports.** This proposal is
  strictly about the existing Streamable HTTP tool surface.
- **Supporting headless OAuth service-client flows such as
  `client_credentials`.** The connection workspace added in this issue targets
  fixed-client, browser-mediated hosts such as Claude Code, Cursor, and VS
  Code. Service-token style remote hosts remain explicit future work.

## Prior art

The research focused on primary sources or vendor-authored documentation from
ecosystems that already operate at large scale with agent/app credentials.

| Source | Pattern adopted for attune |
| --- | --- |
| **MCP Authorization + Security Best Practices** | OAuth 2.1 belongs at the transport layer, but servers still need strict consent, exact redirect URI validation, minimum privilege, and server-side enforcement beyond token scopes. |
| **MCP Tools specification + tool annotations** | Tool metadata such as `readOnlyHint`, `destructiveHint`, and `openWorldHint` is advisory. Treat risk metadata as a runtime policy input and UI label, never as the only enforcement layer. |
| **IETF OAuth 2.0 Security BCP (RFC 9700)** | Refresh tokens should be issued and retained based on risk; public clients need rotation or equivalent replay protection; access rights should be narrowly bounded. |
| **IETF OAuth Step-Up (RFC 9470)** | High-risk operations should be modelable as challengeable, even if the first implementation does not yet prompt for recent auth. |
| **GitHub Apps permissions + audit logs** | Default to fine-grained permissions, separate registration from installation/use, require explicit approval for wider privilege, and expose searchable/streamable audit facts such as actor, token context, scopes, and request metadata. |
| **Stripe restricted API keys** | Per-credential allowlists work best when the server owns a single permission catalogue and the UI only selects from that catalogue. |
| **Okta and Slack token rotation** | Refresh-token rotation, replay protection, and single-grant revocation are distinct concerns from client revocation and should be modeled as such. |
| **Google Cloud remote MCP + audit logging** | Remote MCP governance should combine identity, tool-level authorization, and auditable data-access events. |
| **Cloudflare Access service tokens + session management** | Operators need separate surfaces for credentials, sessions, and audit logs; revocation and access-log review are first-class controls. |
| **OpenAI Codex / Claude Code permission systems** | Host-side approvals and sandboxing are complementary to server-side policy. The server must still emit a clear authorization decision and log it. |

The strongest cross-vendor pattern is consistent:

1. **Identity is layered**: client registration, session instance, token/grant.
2. **Authorization is layered**: coarse scopes plus fine-grained operation
   allowlists.
3. **Risk is explicit**: tools or endpoints carry sensitivity labels that drive
   UI and policy.
4. **Audit is decision-centric**: log not only successful mutations, but also
   denials, revocations, and operator changes.

## Proposal

### 1. Add an MCP access control plane, not just more fields on clients

The main product shape should be an admin-only MCP access plane with five
concepts:

- **Clients**: registered OAuth clients, their scopes, policy mode, default
  rate limits, and status.
- **Sessions**: active and recently closed session instances for each client.
- **Refresh grants**: refresh-token lineage / validity that can be revoked
  independently of the client.
- **Tool policies**: per-client per-tool authorization and rate-limit overrides.
- **Audit facets**: searchable governance events, especially denials and
  operator actions.

This means issue #153 should not be implemented as a few extra columns on the
existing `mcp_clients` page. It should be implemented as a cohesive
administrative surface that answers "what is allowed right now?" and "what just
happened?"

Because attune intentionally uses tenant-scoped pre-registered OAuth clients
instead of unauthenticated dynamic client registration, that same surface also
needs to expose the deployment's public MCP URLs and fixed-client host guidance
for real MCP consumers such as Claude Code, Cursor, and VS Code. Operators
should not have to reconstruct discovery URLs, callback expectations, or host
config snippets by hand from reverse-proxy config.

The same surface should be honest about current limits. The issue-complete
workspace can help operators wire interactive OAuth hosts, but it must not
imply that attune already supports headless service-client grants or remote
automation hosts that expect `client_credentials`.

### 2. Define a single runtime tool catalogue with risk metadata

Per-tool policy only works if the product has one authoritative list of tools
and their semantics. That list should live in code next to MCP registration,
not in the database.

Add a new registry, for example `internal/mcp/tools/catalog.go`:

```go
type RiskClass string

const (
    RiskRead        RiskClass = "read"
    RiskMutate      RiskClass = "mutate"
    RiskIngest      RiskClass = "ingest"
    RiskDestructive RiskClass = "destructive"
)

type DataClass string

const (
    DataMetadata   DataClass = "metadata"
    DataOperational DataClass = "operational"
    DataUserContent DataClass = "user_content"
    DataSecretAdjacent DataClass = "secret_adjacent"
)

type ApprovalMode string

const (
    ApprovalNone       ApprovalMode = "none"
    ApprovalRecentAuth ApprovalMode = "recent_auth"
    ApprovalHuman      ApprovalMode = "human"
)

type ToolMeta struct {
    Name               string
    RequiredScope      string
    Risk               RiskClass
    DataClass          DataClass
    ReadOnlyHint       bool
    DestructiveHint    bool
    OpenWorldHint      bool
    CanExfiltrate      bool
    WritesUserData     bool
    DefaultRPM         int
    DefaultBurst       int
    RequiresRecentAuth bool
    ApprovalMode       ApprovalMode
}
```

For the current tool set the catalogue is straightforward:

| Tool | Scope | Risk | Notes |
| --- | --- | --- | --- |
| `list_feedback`, `get_feedback`, `list_workflow_states`, `get_workflow_state`, `list_tags` | `mcp:read` | `read` | Read-only tools; default allowlist candidates. |
| `update_workflow_state`, `add_tag`, `remove_tag`, `set_urgent` | `mcp:write` | `mutate` | Mutations that change operational triage state. |
| `submit_feedback` | `mcp:ingest` | `ingest` | Creates new tenant data from an external agent. |

The important design choice is that **policy references tool names from this
catalogue**. The database never invents tool names. That keeps authorization,
Console labels, rate limits, and tests aligned.

The world-class extension is that this catalogue is **multidimensional**, not
just read/write. The additional dimensions are what let attune grow into
stronger controls later:

- `RiskClass` answers "how dangerous is the side effect?"
- `DataClass` answers "how sensitive is the data plane being touched?"
- `CanExfiltrate` answers "could this tool move tenant data out of attune?"
- `ApprovalMode` answers "should a valid token still require fresh proof or
  human involvement?"

That is the same conceptual split mature systems use between permission,
resource sensitivity, and approval requirements.

### 3. Move per-tool authorization in front of dispatcher execution

The current `jsonrpc.Dispatcher` only checks whether a method exists and then
invokes the registered tool function. Scope checks live inside each tool. That
is too late and too fragmented for policy governance.

Introduce an MCP policy gate before dispatch:

```
token validation
  -> client/session load
  -> tool metadata load
  -> per-client tool policy evaluation
  -> per-client/per-tool rate-limit evaluation
  -> audit authorization decision
  -> dispatch tool
  -> touch session activity + audit result
```

A new service such as `internal/mcp/policy.Service` should return a structured
decision:

```go
type Decision struct {
    Allowed        bool
    Reason         string
    ClientID       uuid.UUID
    SessionID      uuid.UUID
    ToolName       string
    Risk           RiskClass
    DataClass      DataClass
    AppliedScope   string
    AppliedRPM     int
    AppliedBurst   int
    ApprovalMode   ApprovalMode
    Simulated      bool
}
```

This service is where attune combines:

- the access token claims,
- the client registration,
- the session state,
- the tool catalogue entry,
- the client's tool policy mode and overrides,
- the applicable rate-limit bucket.

This design gives one enforcement path for:

- "client does not have the required scope"
- "tool is denied by allowlist"
- "session is closed"
- "tool is rate limited"

and makes each of those states separately auditable.

### 4. Add explicit tool-policy mode and rollout stages

Issue #153 explicitly requires that existing clients continue to work after
migration with default allow-all behavior. The cleanest design is:

- add `tool_policy_mode` to `mcp_oauth_clients`
- supported values: `legacy_allow_all`, `allow_list`
- default value for existing rows: `legacy_allow_all`
- default value for newly created clients should be feature-flagged so attune
  can graduate from compatibility mode to secure-by-default mode later

Then add a new table for overrides:

```sql
CREATE TABLE mcp_client_tool_policies (
    client_id        UUID        NOT NULL REFERENCES mcp_oauth_clients(id) ON DELETE CASCADE,
    tool_name        TEXT        NOT NULL,
    effect           TEXT        NOT NULL, -- allow | deny
    rate_limit_rpm   INT,
    rate_limit_burst INT,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (client_id, tool_name)
);
```

Policy evaluation rules:

- `legacy_allow_all`: all registered tools remain callable if scope checks pass,
  except explicit `deny` rows.
- `allow_list`: only tools with explicit `allow` rows are callable.
- policy rows may override default rate limits on a per-tool basis.

This gives the desired migration behavior:

- old clients keep working with zero admin action,
- new or hardened clients can switch to `allow_list`,
- individual exceptions can be modeled without duplicating tool metadata in the
  database.

To make policy hardening safe in production, add one more layer:

```sql
ALTER TABLE mcp_oauth_clients
    ADD COLUMN policy_enforcement_mode TEXT NOT NULL DEFAULT 'enforce';
```

Supported values:

- `enforce`: denials block the tool call
- `dry_run`: denials are logged as if they would have happened, but the tool is
  still allowed

`dry_run` is how attune gets from "everyone works today" to "this client is
truly least-privilege" without surprising operators. This is the governance
equivalent of a progressive rollout and is a hallmark of mature security
control planes.

### 5. Make sessions first-class operational records

The current `mcp_sessions` table tracks only client, tenant, scopes, timestamps,
and closure state. That is enough for token validation. It is not enough for
investigation.

Extend sessions with operator-meaningful activity fields:

```sql
ALTER TABLE mcp_sessions
    ADD COLUMN user_id TEXT DEFAULT '',
    ADD COLUMN last_tool_name TEXT DEFAULT '',
    ADD COLUMN last_decision TEXT DEFAULT '',
    ADD COLUMN last_ip TEXT DEFAULT '',
    ADD COLUMN last_user_agent TEXT DEFAULT '',
    ADD COLUMN closed_reason TEXT DEFAULT '',
    ADD COLUMN closed_by TEXT DEFAULT '';
```

Behavior changes:

- create sessions with `user_id` from the OAuth approval principal
- touch `last_active_at` on every authenticated MCP request that passes session
  validation
- update `last_tool_name`, `last_decision`, `last_ip`, and `last_user_agent`
  after each policy decision
- when a session is terminated by admin action, record `closed_reason` and
  `closed_by`

This keeps the existing `session_id`-based access-token invalidation model,
while making the record useful to operators.

### 6. Model refresh grants independently from clients

Refresh tokens already carry `session_id`, which is a strong start. The missing
operational ability is to inspect and revoke them as grants rather than as raw
credentials.

The first implementation does not need to expose secret token material, but it
should expose:

- token/grant ID
- client ID
- session ID
- scopes
- created time
- expiry
- revoked time

If scope permits a small schema improvement, add lineage fields:

```sql
ALTER TABLE mcp_oauth_refresh_tokens
    ADD COLUMN family_id UUID,
    ADD COLUMN rotated_from_id UUID REFERENCES mcp_oauth_refresh_tokens(id) ON DELETE SET NULL,
    ADD COLUMN revoke_reason TEXT DEFAULT '';
```

That aligns with the rotation/replay patterns used by Okta and Slack, without
changing the external token flow visible to MCP clients.

### 7. Add per-client and per-tool rate-limit controls

The current MCP runtime uses a single in-memory sliding limiter keyed by
`client_id`. That should become a two-level policy:

- **client default**: the maximum combined request rate for the client
- **tool override**: a narrower limit for a specific tool

Recommended keys:

- client bucket: `client:<client_id>`
- tool bucket: `client:<client_id>:tool:<tool_name>`

Evaluation:

1. Check client bucket.
2. Check tool bucket if an override or default tool rate exists.
3. If either fails, deny with a structured authorization/rate-limit error and
   audit event.

The data model can use client-level defaults on `mcp_oauth_clients`:

```sql
ALTER TABLE mcp_oauth_clients
    ADD COLUMN rate_limit_rpm INT,
    ADD COLUMN rate_limit_burst INT;
```

The implementation should keep the limiter behind an interface so a future
distributed backend can replace the in-memory limiter without changing policy
evaluation or Console shapes.

### 8. Promote authorization denials to first-class audit events

attune already audits successful MCP tool actions. Governance requires auditing
the *decision* as well, especially when the tool never ran.

Add audit actions such as:

- `mcp_session.revoke`
- `mcp_session.list`
- `mcp_refresh_grant.revoke`
- `mcp_client.policy_update`
- `mcp_client.policy_simulated_deny`
- `mcp_tool.authorize_denied`
- `mcp_tool.rate_limited`

Common facets should include:

- `tenant_id`
- `client_id`
- `session_id`
- `tool_name`
- `tool_risk`
- `policy_mode`
- `decision_reason`
- `scopes`
- `request_ip`
- `user_agent`
- `trace_id`
- `policy_enforcement_mode`
- `approval_mode`

This keeps the existing per-tool mutation/read audit trail, while adding the
operator-facing governance trail needed for incident response.

### 9. Add a proto-backed admin API and expand the Console surface

The repository contract prefers proto-defined HTTP shapes for new endpoints. For
issue #153, the clean design is a small admin contract, for example
`proto/attune/v1/mcp_admin.proto`, generated into Go and TypeScript.

Suggested capabilities:

- `ListMCPClients`
- `GetMCPClient`
- `ListMCPSessions`
- `RevokeMCPSession`
- `ListMCPRefreshGrants`
- `RevokeMCPRefreshGrant`
- `GetMCPToolCatalog`
- `UpdateMCPClientToolPolicy`

Console shape:

- keep the existing `/mcp-clients` entry point
- expand it into an MCP access page with:
  - client table
  - client detail drawer or page
  - active sessions list
  - refresh grants list
  - tool policy editor
  - last activity context

The page should expose enough detail to investigate without leaking secrets:

- show IDs, times, scopes, tool names, decision status
- show IP / user agent in truncated or hashed form if needed
- never show raw refresh token values

### 10. Keep step-up capacity in the model without blocking the issue

World-class systems frequently apply recent-auth or human-approval checks to
higher-risk operations. The current attune MCP tool set does not yet have a
clear "delete data / export private corpus / rotate credential" operation that
forces a full challenge flow.

The design should no longer stop at a boolean hook. It should define the server
model now, even if every current tool starts with `ApprovalNone`.

```go
type ApprovalMode string

const (
    ApprovalNone       ApprovalMode = "none"
    ApprovalRecentAuth ApprovalMode = "recent_auth"
    ApprovalHuman      ApprovalMode = "human"
)
```

The minimum server-side structure is:

```sql
CREATE TABLE mcp_approval_challenges (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       TEXT        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    client_id       UUID        NOT NULL REFERENCES mcp_oauth_clients(id) ON DELETE CASCADE,
    session_id      UUID        NOT NULL REFERENCES mcp_sessions(id) ON DELETE CASCADE,
    tool_name       TEXT        NOT NULL,
    status          TEXT        NOT NULL, -- pending | approved | expired | denied
    requested_by    TEXT        NOT NULL,
    approved_by     TEXT        NOT NULL DEFAULT '',
    expires_at      TIMESTAMPTZ NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

Issue #153 does not have to expose a full approval UI, but the runtime should
be able to return a structured "challenge required" denial for future tools. If
attune later adds export, destructive delete, or credential-rotation tools, the
schema and policy layer are already ready.

### 11. Add policy simulation, break-glass, and blast-radius controls

The difference between "feature-rich" and "world-class" is often whether the
operator can change policy safely under pressure.

Add three controls:

- **policy simulation**: `dry_run` denials are evaluated and audited but do not
  block traffic
- **tenant/client kill switch**: a single flag can suspend MCP execution for a
  tenant or one client without deleting registrations
- **break-glass override**: a bounded administrative override, separately
  audited, can temporarily force `legacy_allow_all` for incident recovery

Suggested client-level fields:

```sql
ALTER TABLE mcp_oauth_clients
    ADD COLUMN suspended_at TIMESTAMPTZ,
    ADD COLUMN suspended_reason TEXT DEFAULT '',
    ADD COLUMN break_glass_until TIMESTAMPTZ;
```

These controls are what make a policy system operable during rollouts and
incidents instead of becoming a source of outages.

### 12. Add audit export and durable governance evidence

Searchable in-product audit is necessary. It is not sufficient for mature
security operations. World-class deployments also need export.

attune should add an audit-export abstraction such as:

```go
type GovernanceSink interface {
    Publish(ctx context.Context, evt auditlog.Event) error
}
```

Recommended first sinks:

- JSONL file rotation for self-hosted operators
- webhook / HTTP sink for SIEM forwarders
- future cloud bucket or queue sink if the product grows into managed
  distribution

Export must include authorization denials, session revocations, policy changes,
and rate-limit denials, not only successful tool business actions.

This is the piece that lets a security team correlate attune with broader
identity, EDR, and network telemetry, which is a standard expectation in
enterprise environments.

### 13. Design for scale: distributed limits, anomaly signals, and risk scoring

The first implementation can stay instance-local. The design should still be
honest about its target-state ceiling.

The target-state control loop is:

```
tool request
  -> policy decision
  -> local/distributed rate limit
  -> activity + audit event
  -> anomaly counter / risk score update
  -> optional automated suspension or operator alert
```

Useful future-facing signals:

- unusually high denial rate for one client
- one client invoking many tools it was never meant to use
- a burst of session creation followed by denials
- repeated refresh-token replay / family invalidation once lineage is added

The proposal does not require automated response in v1, but it should keep the
data model and event shape suitable for it.

## Alternatives considered

### Keep scope-only enforcement and rely on client proliferation

Rejected. It scales poorly operationally and recreates the classic
"one credential per permission slice" sprawl that GitHub Apps and restricted
keys were designed to avoid.

### Store the tool catalogue in the database

Rejected. Tool definitions are runtime code, not tenant content. Duplicating the
catalogue in SQL invites drift between dispatcher registration, tests, UI
labels, and policy evaluation.

### Revoke suspicious sessions by revoking refresh tokens only

Rejected. Access tokens are bearer tokens and remain valid until expiry. The
current session check in token validation is already the right hard-stop point;
single-session revocation should close the session and invalidate subsequent
authenticated MCP requests immediately.

### Create a separate MCP governance audit table

Rejected for the first implementation. `audit_log` already exists, is already a
compliance surface, and already captures actor/target semantics. New actions and
facets are sufficient unless query volume proves otherwise.

### Require distributed rate limiting now

Rejected for the first implementation. A distributed limiter is attractive, but
introduces new operational dependencies and is not required to satisfy issue
acceptance. The policy model should be backend-agnostic, and the proposal now
explicitly reserves a path to a stronger backend later.

## Risks and tradeoffs

- **Console/API scope growth**: the clean administrative surface is larger than
  the current `mcp_clients` page. This is the main reason the issue spans
  backend, Console, audit, and tests rather than being a small handler patch.
- **Schema growth in security-sensitive tables**: session and token tables will
  gain operator-facing fields. The migration must preserve existing runtime
  semantics and avoid introducing nullable edge cases in token validation.
- **Potential double-auditing**: successful tool calls already emit action
  audits. Adding decision audits must avoid creating noisy duplicates that
  obscure signal. The intended split is "decision event" for denial / throttle /
  admin control changes, "action event" for successful business operation.
- **Rate limits remain instance-local in v1**: this is acceptable if documented,
  but it is not a global quota. The design should not pretend otherwise.
- **Policy mistakes can break active clients**: defaulting migrated clients to
  `legacy_allow_all` reduces rollout risk, but operator UX must make policy mode
  obvious so hardening changes are intentional.
- **Approval modeling adds design weight before it is exercised**: this is a
  deliberate trade. The payoff is that future destructive or export-oriented
  MCP features will not require a second schema redesign.
- **Audit export increases operational surface area**: file/webhook exporters
  can fail or back up. Export must therefore be additive and non-blocking to the
  primary request path.

## Implementation plan

The delivery plan should be explicitly phased so issue #153 can land usefully
without losing sight of the world-class target state.

This change set implements **Phase P0**. Phase P1 and P2 remain explicit future
hardening work rather than implied scope creep inside #153.

### Phase P0: issue-complete core

1. **Write schema and contract changes**
   - Add client policy mode and default rate-limit fields.
   - Add `mcp_client_tool_policies`.
   - Extend `mcp_sessions` with activity / revocation context.
   - Extend refresh-token rows with grant-facing metadata if included.
   - Add proto definitions for admin list/revoke/update endpoints.

2. **Add runtime tool catalogue and policy service**
   - Introduce `ToolMeta` registry.
   - Introduce `internal/mcp/policy`.
   - Move authorization decisions in front of dispatcher execution.
   - Add structured policy-denial error mapping and HTTP 403 handling.

3. **Add session and grant repositories**
   - List sessions by client / tenant.
   - Revoke single session.
   - List refresh grants by client / tenant.
   - Revoke single grant.
   - Update session activity on live MCP requests.

4. **Expand Console/admin handlers**
   - Add admin API handlers and proto/OpenAPI contracts for the admin surface.
   - Expand the existing MCP Console page into an access control view.
   - Add revoke-session, revoke-grant, and tool-policy mutation flows.

5. **Add audit and rate-limit integration**
   - Add new audit actions and event facets.
   - Apply per-client and per-tool rate limits.
   - Audit denials and rate-limit decisions.

6. **Add tests**
   - Unit tests for policy evaluation rules.
   - Repo tests for session/grant listing and revocation.
   - HTTP tests for admin endpoints.
   - Integration tests for:
     - single-session revoke invalidates access token use
     - allowlisted client can call allowed tool
     - denied tool returns clear authorization error and audit event
     - cross-tenant session/grant access is rejected
     - migrated legacy client continues to work in `legacy_allow_all`

### Phase P1: enterprise+ hardening

1. **Add rollout safety**
   - Add `policy_enforcement_mode=dry_run`.
   - Add Console visualization for simulated denials.
   - Add client suspension and break-glass fields.

2. **Add governance export**
   - Implement a non-blocking governance sink interface.
   - Ship at least one durable export path.

3. **Add richer risk metadata**
   - Populate `DataClass`, `CanExfiltrate`, and `ApprovalMode`.
   - Label current tools accordingly in Console and audit.

### Phase P2: world-class posture

1. **Add approval challenges**
   - Persist challenge records.
   - Return challenge-required responses for high-risk future tools.

2. **Add refresh-token family controls**
   - Enforce family lineage and replay invalidation semantics.

3. **Add distributed rate-limit backend option**
   - Keep the limiter interface stable; add a shared backend implementation.

4. **Add anomaly and automated response hooks**
   - Emit counters suitable for alerting or automated suspension.

## Verification

Implementation should not be considered complete without the following evidence:

- `go test ./internal/mcp/... ./internal/handlers/console/... ./internal/repo/mcp/...`
- `go test ./cmd/attune ./internal/handlers/console ./internal/infra/database`
- `pnpm --dir console vitest run src/features/mcp-clients`
- `pnpm --dir console tsc -b --noEmit`
- `pnpm --dir console vite build`
- `buf lint`
- relevant lint / gate subset from `make ci-check`

Behavioral verification should specifically demonstrate:

- a session revoked by admin begins failing authenticated MCP requests without
  revoking sibling sessions on the same client
- a disallowed tool fails with a stable, explicit authorization error
- insufficient-scope failures return a machine-readable Bearer challenge with
  the required scope
- the denial is recorded in audit with client/session/tool context
- `last_active_at` and `last_tool_name` reflect real MCP invocations
- existing clients created before the migration still function under
  `legacy_allow_all`
- standards-compliant well-known discovery works for both the protected
  resource and the authorization server even when MCP and Console use
  different public origins
- a compatibility regression matrix covers at least 10 distinct MCP host
  discovery/auth behaviors so split-origin, legacy-path, and scope-challenge
  regressions fail fast in CI
- the connection workspace clearly distinguishes supported interactive OAuth
  hosts from still-unsupported headless/service-client flows so operators do
  not misconfigure CI or daemon-style MCP consumers

For the P1/P2 path, additional evidence should eventually demonstrate:

- governance events can be exported to an external sink without breaking the
  request path
- challenge-required tools produce a stable, machine-readable denial contract
- rate-limit behavior remains correct after swapping the limiter backend

## References

- Model Context Protocol: Authorization
  [https://modelcontextprotocol.io/specification/draft/basic/authorization](https://modelcontextprotocol.io/specification/draft/basic/authorization)
- Model Context Protocol: Tools
  [https://modelcontextprotocol.io/specification/2025-06-18/server/tools](https://modelcontextprotocol.io/specification/2025-06-18/server/tools)
- Model Context Protocol: Security best practices
  [https://modelcontextprotocol.io/docs/tutorials/security/security_best_practices](https://modelcontextprotocol.io/docs/tutorials/security/security_best_practices)
- Model Context Protocol blog: Tool annotations
  [https://blog.modelcontextprotocol.io/posts/2026-03-16-tool-annotations/](https://blog.modelcontextprotocol.io/posts/2026-03-16-tool-annotations/)
- IETF RFC 9700: Best Current Practice for OAuth 2.0 Security
  [https://datatracker.ietf.org/doc/rfc9700/](https://datatracker.ietf.org/doc/rfc9700/)
- IETF RFC 9470: OAuth 2.0 Step Up Authentication Challenge Protocol
  [https://datatracker.ietf.org/doc/rfc9470/](https://datatracker.ietf.org/doc/rfc9470/)
- GitHub Docs: Choosing permissions for a GitHub App
  [https://docs.github.com/en/apps/creating-github-apps/registering-a-github-app/choosing-permissions-for-a-github-app](https://docs.github.com/en/apps/creating-github-apps/registering-a-github-app/choosing-permissions-for-a-github-app)
- GitHub Docs: Permissions required for GitHub Apps
  [https://docs.github.com/en/rest/authentication/permissions-required-for-github-apps](https://docs.github.com/en/rest/authentication/permissions-required-for-github-apps)
- GitHub Docs: Using the audit log for your enterprise
  [https://docs.github.com/enterprise-cloud%40latest/enterprise-onboarding/govern-people-and-repositories/using-the-audit-log-for-your-enterprise](https://docs.github.com/enterprise-cloud%40latest/enterprise-onboarding/govern-people-and-repositories/using-the-audit-log-for-your-enterprise)
- GitHub Docs: Audit log events for your organization
  [https://docs.github.com/en/organizations/keeping-your-organization-secure/managing-security-settings-for-your-organization/audit-log-events-for-your-organization](https://docs.github.com/en/organizations/keeping-your-organization-secure/managing-security-settings-for-your-organization/audit-log-events-for-your-organization)
- GitHub Docs: Streaming the audit log for your enterprise
  [https://docs.github.com/enterprise-cloud%40latest/admin/monitoring-activity-in-your-enterprise/reviewing-audit-logs-for-your-enterprise/streaming-the-audit-log-for-your-enterprise](https://docs.github.com/enterprise-cloud%40latest/admin/monitoring-activity-in-your-enterprise/reviewing-audit-logs-for-your-enterprise/streaming-the-audit-log-for-your-enterprise)
- Stripe Docs: API keys and restricted keys
  [https://docs.stripe.com/keys](https://docs.stripe.com/keys)
- Okta Docs: Refresh access tokens and rotate refresh tokens
  [https://developer.okta.com/docs/guides/refresh-tokens/main/](https://developer.okta.com/docs/guides/refresh-tokens/main/)
- Slack Docs: Using token rotation
  [https://docs.slack.dev/authentication/using-token-rotation/](https://docs.slack.dev/authentication/using-token-rotation/)
- Google Cloud Docs: MCP overview
  [https://docs.cloud.google.com/mcp/overview](https://docs.cloud.google.com/mcp/overview)
- Google Cloud Docs: Audit logging for MCP
  [https://docs.cloud.google.com/mcp/audit-logging](https://docs.cloud.google.com/mcp/audit-logging)
- Google Cloud Docs: Secure agent interactions with MCP
  [https://docs.cloud.google.com/bigtable/docs/secure-agent-interactions-mcp](https://docs.cloud.google.com/bigtable/docs/secure-agent-interactions-mcp)
- Cloudflare Docs: Service tokens
  [https://developers.cloudflare.com/cloudflare-one/access-controls/service-credentials/service-tokens/](https://developers.cloudflare.com/cloudflare-one/access-controls/service-credentials/service-tokens/)
- Cloudflare Docs: Session management
  [https://developers.cloudflare.com/cloudflare-one/access-controls/access-settings/session-management/](https://developers.cloudflare.com/cloudflare-one/access-controls/access-settings/session-management/)
- OpenAI Codex: Permissions
  [https://developers.openai.com/codex/permissions](https://developers.openai.com/codex/permissions)
- OpenAI Codex: Agent approvals and security
  [https://developers.openai.com/codex/agent-approvals-security](https://developers.openai.com/codex/agent-approvals-security)
- Anthropic Docs: Claude Code security
  [https://docs.anthropic.com/en/docs/claude-code/security](https://docs.anthropic.com/en/docs/claude-code/security)
