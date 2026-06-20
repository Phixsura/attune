# MCP Server: Agent-Accessible Tool Surface

| | |
|---|---|
| **Issue** | #93 |
| **Status** | Proposed |
| **Started** | 2026-06-20 |
| **Related** | #66 (channel-agnostic inbound — symmetric counterpart), #41 (API key scopes — extended for MCP), #39 (audit log — reused for MCP actions), #40 (OIDC SSO — OAuth AS reuses OIDC login), #34 (outbound notify — parallel adapter pattern) |

---

## Problem

attune is evolving into a bidirectional product intelligence hub (#66): the inbound framework normalizes external signals into one event stream, and the LLM enrichment pipeline classifies, dedupes, and produces insight.

What's missing is a first-class surface for **AI agents** (Claude, Cursor, Windsurf, IDE extensions, custom automations) to *operate on* attune content — not as anonymous REST callers, but as authenticated principals with attribution and audit.

### Current state

1. **No agent-native protocol** — agents must reverse-engineer REST endpoints, handle pagination, parse errors, and manage auth manually. No tool catalogue, no structured input/output schemas.

2. **No agent identity** — an API key authenticates a tenant, not an individual agent instance. If three Cursor sessions use the same key, audit log cannot distinguish them.

3. **No agent-specific audit** — existing `audit_log` (#39) tracks Console users, not agent-initiated actions. Compliance teams cannot answer "what did the AI agent do in the last 24h?"

4. **No standard auth flow for agents** — Claude Desktop / Cursor expect OAuth 2.1 with `/.well-known/oauth-protected-resource` discovery. attune has no Authorization Server; agents cannot self-register or request scoped tokens.

### Impact

- **Developer friction** — building agent integrations requires custom REST wrappers, not `mcp.json` config.
- **Enterprise blocker** — SOC 2 / EU AI Act require auditable agent trails; current model cannot provide them.
- **Missed opportunity** — MCP is becoming the standard; first-class support differentiates attune in the AI-native feedback space.

---

## Goals

| Category | Goal |
|----------|------|
| **Protocol** | Expose attune via Model Context Protocol (MCP) over Streamable HTTP transport. |
| | Publish `/.well-known/oauth-protected-resource` for client discovery. |
| **Auth** | Built-in OAuth 2.1 Authorization Server with PKCE; Console OIDC login as resource owner authorization. |
| | Per-agent access tokens with `mcp:*` scopes; immediate revocation via Console. |
| | Agent identity persisted as `principal=mcp:<client_id>:<session_id>` for audit. |
| **Tool catalogue** | Full coverage: read (search, query, list), write (triage, classify, tag), ingest (record signals). |
| | `snake_case` `verb_noun` naming: `list_feedback`, `search_feedback`, `update_status`, `record_signal`. |
| | JSON Schema for all inputs; optional `outputSchema` for structured results. |
| **Audit** | Reuse `audit_log` table (#39) with `actor_type='mcp_agent'`. |
| | Capture: `tool_name`, `arguments_hash`, `result_status`, `latency_ms`, `tokens_in`, `tokens_out`. |
| | Admin-only Console page for agent activity timeline. |
| **Scope model** | Extend #41 API key scopes with `mcp:*` category. |
| | Per-tool scope mapping; `mcp:read` for read tools, `mcp:write` for mutations, `mcp:ingest` for record_signal. |
| **Observability** | Metrics: `attune_mcp_tool_calls_total{tool, result}`, `attune_mcp_tool_latency_seconds{tool}`. |
| | Logs via `logext` with `trace_id` / `span_id` injection. |

---

## Non-goals

| Scope | Rationale |
|-------|-----------|
| **stdio transport** | Local-only; attune is a remote SaaS. Agents use Streamable HTTP. |
| **MCP Resources / Prompts** | Tools are the primary surface; Resources (data read) and Prompts (templates) can be added later. |
| **Third-party IdP delegation** | v0.6 ships built-in AS; external IdP (Auth0, Keycloak) is a follow-up. |
| **Agent-to-agent delegation** | Token Exchange (RFC 8693) for chained agents is out of scope. |
| **Real-time streaming results** | Tools return complete responses; SSE streaming for long-running ops is a follow-up. |
| **MCP gateway proxy pattern** | Direct service-layer calls, not REST-to-REST forwarding. |

---

## Proposal

### 1. Package layout (Integrated approach)

MCP server is integrated into the existing `cmd/attune` binary as a new route mount:

```
cmd/attune/
├── router.go              # add r.Mount("/mcp", mcpHandler.Routes())
├── main.go                # initialize MCP server, OAuth AS

internal/
├── mcp/
│   ├── server.go          # MCP JSON-RPC handler (Streamable HTTP)
│   ├── session.go         # session management (Mcp-Session-Id)
│   ├── auth.go            # OAuth token validation middleware
│   ├── tools/
│   │   ├── registry.go    # tool registration + discovery
│   │   ├── list_feedback.go
│   │   ├── search_feedback.go
│   │   ├── get_feedback.go
│   │   ├── update_status.go
│   │   ├── update_tags.go
│   │   ├── reclassify.go
│   │   ├── record_signal.go
│   │   ├── list_dimensions.go
│   │   ├── get_digest.go
│   │   └── ...
│   └── oauth/
│       ├── server.go      # Authorization Server core
│       ├── authorize.go   # GET /mcp/oauth/authorize
│       ├── token.go       # POST /mcp/oauth/token
│       ├── revoke.go      # POST /mcp/oauth/revoke
│       ├── discovery.go   # GET /.well-known/oauth-protected-resource
│       └── client.go      # OAuth client (agent) registration

internal/handlers/console/
├── mcp/
│   ├── agents.go          # list/revoke registered agents
│   ├── authorize_ui.go    # OAuth consent screen
│   └── activity.go        # agent activity timeline
```

### 2. OAuth 2.1 Authorization Server

#### 2.1 Client registration

Agents register via Console UI or `POST /mcp/oauth/clients`:

```go
// internal/mcp/oauth/client.go

type OAuthClient struct {
    ID           uuid.UUID  `json:"id"`
    TenantID     string     `json:"tenant_id"`
    Name         string     `json:"name"`           // e.g., "Cursor IDE"
    RedirectURIs []string   `json:"redirect_uris"`  // e.g., ["http://127.0.0.1:*"]
    Scopes       []string   `json:"scopes"`         // requested scopes
    CreatedAt    time.Time  `json:"created_at"`
    CreatedBy    string     `json:"created_by"`     // user who registered
}
```

#### 2.2 Authorization flow (PKCE required)

```
Agent                       attune AS                    Console UI
  │                              │                            │
  │─ GET /mcp/oauth/authorize ──▶│                            │
  │   ?client_id=...             │                            │
  │   &redirect_uri=...          │                            │
  │   &code_challenge=...        │                            │
  │   &code_challenge_method=S256│                            │
  │   &scope=mcp:read+mcp:write  │                            │
  │   &state=...                 │                            │
  │                              │                            │
  │                              │─ redirect to Console ─────▶│
  │                              │   /fb/v1/console/mcp/auth  │
  │                              │                            │
  │                              │           [user logs in    │
  │                              │            via OIDC #40]   │
  │                              │                            │
  │                              │◀─ POST /mcp/oauth/approve ─│
  │                              │   (auth code generated)    │
  │                              │                            │
  │◀─ redirect with code ────────│                            │
  │   ?code=...&state=...        │                            │
  │                              │                            │
  │─ POST /mcp/oauth/token ─────▶│                            │
  │   grant_type=authorization_code                           │
  │   code=...                   │                            │
  │   code_verifier=...          │                            │
  │                              │                            │
  │◀─ { access_token, ... } ─────│                            │
```

#### 2.3 Token structure

```go
type AccessToken struct {
    TenantID  string    `json:"tenant_id"`
    ClientID  uuid.UUID `json:"client_id"`
    SessionID uuid.UUID `json:"session_id"`  // unique per token issuance
    Scopes    []string  `json:"scopes"`
    ExpiresAt time.Time `json:"exp"`
    IssuedAt  time.Time `json:"iat"`
}
```

Tokens are JWTs signed with tenant-specific key (from `secrets.tink_keyset`). Short-lived (1h default), refresh tokens optional.

#### 2.4 Discovery endpoint

`GET /.well-known/oauth-protected-resource`:

```json
{
  "resource": "https://attune.example.com/mcp/v1",
  "authorization_servers": ["https://attune.example.com/mcp/oauth"],
  "scopes_supported": ["mcp:read", "mcp:write", "mcp:ingest"],
  "bearer_methods_supported": ["header"]
}
```

### 3. MCP Streamable HTTP transport

#### 3.1 Endpoints

| Endpoint | Method | Purpose |
|----------|--------|---------|
| `POST /mcp/v1` | POST | JSON-RPC 2.0 messages (tool calls, list tools) |
| `GET /mcp/v1/sse` | GET | Server-Sent Events for async notifications (optional) |

#### 3.2 Session management

- Server generates `Mcp-Session-Id` on first request (UUID).
- Client includes header on all subsequent requests.
- Session stores: tenant context, client identity, rate limit state.
- Sessions expire after 24h idle; explicit `DELETE /mcp/v1/session` to close.

#### 3.3 JSON-RPC handler

```go
// internal/mcp/server.go

type MCPServer struct {
    tools    *tools.Registry
    sessions *SessionStore
    oauth    *oauth.Validator
    audit    *AuditRecorder
    svc      *service.FeedbackService  // inject service layer
}

func (s *MCPServer) HandleRPC(w http.ResponseWriter, r *http.Request) {
    // 1. Validate Bearer token
    token, err := s.oauth.Validate(r)
    if err != nil {
        writeJSONRPCError(w, -32001, "unauthorized")
        return
    }

    // 2. Get or create session
    sess := s.sessions.GetOrCreate(r.Header.Get("Mcp-Session-Id"), token)
    w.Header().Set("Mcp-Session-Id", sess.ID.String())

    // 3. Parse JSON-RPC request
    var req jsonrpc.Request
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        writeJSONRPCError(w, -32700, "parse error")
        return
    }

    // 4. Dispatch method
    switch req.Method {
    case "tools/list":
        s.handleToolsList(w, sess, req)
    case "tools/call":
        s.handleToolCall(w, sess, req)
    default:
        writeJSONRPCError(w, -32601, "method not found")
    }
}
```

### 4. Tool catalogue

#### 4.1 Scope mapping

| Scope | Tools |
|-------|-------|
| `mcp:read` | `list_feedback`, `search_feedback`, `get_feedback`, `list_dimensions`, `list_tags`, `get_digest`, `list_clusters`, `get_cluster` |
| `mcp:write` | `update_status`, `update_tags`, `reclassify`, `link_issue`, `mark_duplicate`, `batch_update_status` |
| `mcp:ingest` | `record_signal` |

#### 4.2 Tool definitions

```go
// internal/mcp/tools/search_feedback.go

var SearchFeedback = tools.Definition{
    Name:        "search_feedback",
    Description: "Search feedback by semantic query, filters, and time range. Use this when you need to find feedback matching specific criteria.",
    InputSchema: jsonschema.Object{
        Properties: map[string]jsonschema.Schema{
            "query": {
                Type:        "string",
                Description: "Semantic search query (natural language)",
            },
            "filters": {
                Type: "object",
                Properties: map[string]jsonschema.Schema{
                    "status":     {Type: "string", Enum: []string{"new", "triaged", "in_progress", "resolved", "closed"}},
                    "dimensions": {Type: "array", Items: &jsonschema.Schema{Type: "string"}},
                    "tags":       {Type: "array", Items: &jsonschema.Schema{Type: "string"}},
                    "source":     {Type: "string"},
                    "sentiment":  {Type: "string", Enum: []string{"positive", "neutral", "negative"}},
                },
            },
            "time_range": {
                Type: "object",
                Properties: map[string]jsonschema.Schema{
                    "start": {Type: "string", Format: "date-time"},
                    "end":   {Type: "string", Format: "date-time"},
                },
            },
            "limit": {
                Type:        "integer",
                Description: "Max results (default 20, max 100)",
                Default:     20,
            },
            "cursor": {
                Type:        "string",
                Description: "Pagination cursor from previous response",
            },
        },
        Required: []string{},
    },
    OutputSchema: jsonschema.Object{
        Properties: map[string]jsonschema.Schema{
            "items": {
                Type: "array",
                Items: &jsonschema.Schema{
                    Type: "object",
                    Properties: map[string]jsonschema.Schema{
                        "id":         {Type: "string"},
                        "content":    {Type: "string"},
                        "status":     {Type: "string"},
                        "dimensions": {Type: "array"},
                        "tags":       {Type: "array"},
                        "sentiment":  {Type: "string"},
                        "created_at": {Type: "string", Format: "date-time"},
                    },
                },
            },
            "next_cursor": {Type: "string"},
            "total_count": {Type: "integer"},
        },
    },
    RequiredScope: domain.ScopeMCPRead,
}

func (t *SearchFeedbackTool) Execute(ctx context.Context, sess *Session, params json.RawMessage) (*tools.Result, error) {
    var input SearchFeedbackInput
    if err := json.Unmarshal(params, &input); err != nil {
        return nil, tools.InvalidParams("invalid input: %v", err)
    }

    // Call service layer
    results, cursor, total, err := t.feedbackSvc.Search(ctx, sess.TenantID, service.SearchParams{
        Query:      input.Query,
        Filters:    input.Filters,
        TimeRange:  input.TimeRange,
        Limit:      min(input.Limit, 100),
        Cursor:     input.Cursor,
    })
    if err != nil {
        return nil, tools.InternalError("search failed: %v", err)
    }

    return &tools.Result{
        Content: []tools.Content{{
            Type: "text",
            Text: mustJSON(SearchFeedbackOutput{
                Items:      results,
                NextCursor: cursor,
                TotalCount: total,
            }),
        }},
    }, nil
}
```

#### 4.3 Full tool list (22 tools)

| Tool | Category | Description |
|------|----------|-------------|
| `list_feedback` | Read | List feedback with pagination and basic filters |
| `search_feedback` | Read | Semantic search with advanced filters |
| `get_feedback` | Read | Get single feedback by ID with full detail |
| `list_dimensions` | Read | List tenant's dimension taxonomy |
| `list_tags` | Read | List tenant's tag taxonomy |
| `list_clusters` | Read | List feedback clusters (themes) |
| `get_cluster` | Read | Get cluster details with member feedback |
| `get_digest` | Read | Get latest digest summary |
| `get_usage_stats` | Read | Get usage statistics |
| `update_status` | Write | Transition feedback workflow status |
| `update_tags` | Write | Add/remove tags on feedback |
| `reclassify` | Write | Re-run dimension classification |
| `link_issue` | Write | Link feedback to external issue (Linear/Jira/GitHub) |
| `mark_duplicate` | Write | Mark feedback as duplicate of another |
| `batch_update_status` | Write | Bulk status transition |
| `batch_update_tags` | Write | Bulk tag update |
| `record_signal` | Ingest | Record external observation into attune |
| `create_tag` | Write | Create new tag |
| `archive_tag` | Write | Archive existing tag |
| `trigger_digest` | Write | Manually trigger digest generation |
| `get_enrichment_status` | Read | Check enrichment queue status |
| `retry_enrichment` | Write | Retry failed enrichment for feedback |

### 5. Audit integration

Reuse `audit_log` table with MCP-specific fields in `after_json`:

```go
// internal/mcp/audit.go

type AuditRecorder struct {
    repo auditlog.Repo
}

func (a *AuditRecorder) RecordToolCall(ctx context.Context, sess *Session, tool string, params json.RawMessage, result *tools.Result, latency time.Duration) error {
    return a.repo.Insert(ctx, auditlog.Row{
        TenantID:       sess.TenantID,
        ActorType:      "mcp_agent",
        ActorID:        fmt.Sprintf("mcp:%s:%s", sess.ClientID, sess.SessionID),
        ActorEmail:     "",  // agents have no email
        ActorIP:        sess.ClientIP,
        ActorUserAgent: sess.UserAgent,
        Action:         fmt.Sprintf("mcp.%s", tool),
        TargetType:     "mcp_tool",
        TargetID:       tool,
        Summary:        fmt.Sprintf("Agent %s called %s", sess.ClientName, tool),
        BeforeJSON:     nil,
        AfterJSON: map[string]any{
            "arguments_hash": sha256Hash(params),
            "result_status":  resultStatus(result),
            "latency_ms":     latency.Milliseconds(),
            "session_id":     sess.SessionID.String(),
            "client_name":    sess.ClientName,
        },
    })
}
```

#### Action taxonomy extension

Add to `validActions` in Go and DB constraint:

```text
mcp.list_feedback
mcp.search_feedback
mcp.get_feedback
mcp.update_status
mcp.update_tags
mcp.reclassify
mcp.link_issue
mcp.mark_duplicate
mcp.batch_update_status
mcp.batch_update_tags
mcp.record_signal
mcp.create_tag
mcp.archive_tag
mcp.trigger_digest
mcp.retry_enrichment
... (all 22 tools prefixed with mcp.)
```

### 6. Scope model extension

Extend #41 API key scopes with MCP category:

```go
// internal/domain/scope.go (extended)

const (
    // ... existing scopes ...

    // MCP scopes
    ScopeMCPRead   Scope = "mcp:read"
    ScopeMCPWrite  Scope = "mcp:write"
    ScopeMCPIngest Scope = "mcp:ingest"
)

var scopeHierarchy = map[Scope][]Scope{
    // ... existing hierarchy ...
    ScopeMCPWrite:  {ScopeMCPRead},
    ScopeMCPIngest: {}, // standalone, does not imply read
}
```

OAuth tokens carry MCP scopes; middleware validates before tool execution.

### 7. Console UI

#### 7.1 Agent management page

`/fb/v1/console/mcp/agents`:

- List registered OAuth clients (agents)
- Show: name, created by, scopes, last active, total calls
- Actions: revoke (deletes all tokens), view activity

#### 7.2 OAuth consent screen

`/fb/v1/console/mcp/authorize`:

- Shows agent name, requested scopes
- User approves/denies
- Uses existing OIDC session for authentication

#### 7.3 Agent activity timeline

`/fb/v1/console/mcp/activity`:

- Filter by agent, tool, date range
- Shows audit log entries where `actor_type='mcp_agent'`
- CSV export for compliance

### 8. Configuration

```yaml
# config.yaml

mcp:
  enabled: true
  
  oauth:
    issuer: "https://attune.example.com/mcp/oauth"
    access_token_ttl: 1h
    refresh_token_ttl: 7d
    # signing key derived from secrets.tink_keyset
  
  rate_limit:
    requests_per_minute: 60
    burst: 10
  
  # allowed redirect URI patterns (supports localhost wildcards)
  allowed_redirect_patterns:
    - "http://127.0.0.1:*"
    - "http://localhost:*"
    - "https://*.cursor.sh/callback"
```

### 9. Database schema

```sql
-- Migration 058: MCP OAuth clients, tokens, and sessions

BEGIN;

-- OAuth clients (registered agents)
CREATE TABLE mcp_oauth_clients (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       TEXT        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name            TEXT        NOT NULL,
    redirect_uris   TEXT[]      NOT NULL,
    scopes          TEXT[]      NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by      TEXT        NOT NULL,  -- user ID who registered
    revoked_at      TIMESTAMPTZ,

    CONSTRAINT uq_mcp_client_name_tenant UNIQUE (tenant_id, name)
);

CREATE INDEX idx_mcp_oauth_clients_tenant ON mcp_oauth_clients(tenant_id);

-- OAuth authorization codes (short-lived)
CREATE TABLE mcp_oauth_codes (
    code            TEXT        PRIMARY KEY,
    client_id       UUID        NOT NULL REFERENCES mcp_oauth_clients(id) ON DELETE CASCADE,
    redirect_uri    TEXT        NOT NULL,
    scopes          TEXT[]      NOT NULL,
    code_challenge  TEXT        NOT NULL,
    user_id         TEXT        NOT NULL,  -- authorizing user
    expires_at      TIMESTAMPTZ NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- OAuth refresh tokens
CREATE TABLE mcp_oauth_refresh_tokens (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    client_id       UUID        NOT NULL REFERENCES mcp_oauth_clients(id) ON DELETE CASCADE,
    token_hash      TEXT        NOT NULL UNIQUE,
    scopes          TEXT[]      NOT NULL,
    user_id         TEXT        NOT NULL,
    expires_at      TIMESTAMPTZ NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    revoked_at      TIMESTAMPTZ
);

CREATE INDEX idx_mcp_refresh_tokens_client ON mcp_oauth_refresh_tokens(client_id);

-- MCP sessions (for Mcp-Session-Id tracking)
CREATE TABLE mcp_sessions (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    client_id       UUID        NOT NULL REFERENCES mcp_oauth_clients(id) ON DELETE CASCADE,
    tenant_id       TEXT        NOT NULL,
    scopes          TEXT[]      NOT NULL,
    last_active_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    closed_at       TIMESTAMPTZ
);

CREATE INDEX idx_mcp_sessions_client ON mcp_sessions(client_id);

COMMIT;
```

```sql
-- Migration 059: MCP audit actions

-- Extend audit_log action constraint with MCP tool calls and OAuth admin actions.
-- Keep the Go allow-list (internal/service/auditlog/actions.go) in lockstep.

ALTER TABLE audit_log DROP CONSTRAINT IF EXISTS chk_audit_action_value;
ALTER TABLE audit_log ADD CONSTRAINT chk_audit_action_value
    CHECK (action IN (
        -- Existing actions (from 057)
        'api_key.create',
        'api_key.revoke',
        'api_key.rotate',
        'digest_subscription.delete',
        'digest_subscription.upsert',
        'enrich_config.promote_suggested',
        'enrich_config.update',
        'enrichment_runtime.reset',
        'enrichment_runtime.rollback',
        'enrichment_runtime.update',
        'feedback.batch_delete',
        'feedback_job.cancel',
        'gdpr.delete',
        'gdpr.delete.cancelled',
        'gdpr.delete.requested',
        'gdpr.export',
        'gdpr.export.revoked',
        'guard_policy.create',
        'guard_policy.delete',
        'guard_policy.update',
        'inbound_source.create',
        'inbound_source.delete',
        'inbound_source.pause',
        'inbound_source.resume',
        'inbound_source.rotate_secret',
        'inbound_source.test_connection',
        'llm_ability.delete',
        'llm_ability.upsert',
        'llm_channel.create',
        'llm_channel.delete',
        'llm_channel.test',
        'llm_channel.update',
        'llm_route.delete',
        'llm_route.upsert',
        'member.invite',
        'member.remove',
        'member.update_role',
        'notify_target.create',
        'notify_target.delete',
        'notify_target.test',
        'notify_target.update',
        'outbox.retry',
        'tag.archive',
        'tag.create',
        'tag.update',
        'workflow_seed_defaults.run',
        'workflow_state.archive',
        'workflow_state.create',
        'workflow_state.update',
        'workflow_transition.replace',

        -- MCP tool calls (new)
        'mcp.list_feedback',
        'mcp.search_feedback',
        'mcp.get_feedback',
        'mcp.list_dimensions',
        'mcp.list_tags',
        'mcp.list_clusters',
        'mcp.get_cluster',
        'mcp.get_digest',
        'mcp.get_usage_stats',
        'mcp.update_status',
        'mcp.update_tags',
        'mcp.reclassify',
        'mcp.link_issue',
        'mcp.mark_duplicate',
        'mcp.batch_update_status',
        'mcp.batch_update_tags',
        'mcp.record_signal',
        'mcp.create_tag',
        'mcp.archive_tag',
        'mcp.trigger_digest',
        'mcp.get_enrichment_status',
        'mcp.retry_enrichment',

        -- MCP OAuth admin actions (new)
        'mcp_client.create',
        'mcp_client.revoke'
    ));
```

---

## Alternatives Considered

### A. Separate MCP binary

Pros:
- Independent scaling
- Fault isolation

Cons:
- Two binaries to deploy
- Config synchronization
- OAuth token verification requires shared DB

**Rejected**: attune is single-binary; agent traffic won't exceed main API.

### B. MCP gateway (proxy to REST)

Pros:
- Zero code duplication
- REST changes auto-reflect

Cons:
- Extra hop latency
- Error translation complexity
- Auth double-validation

**Rejected**: direct service-layer calls are cleaner and faster.

### C. Reuse API keys instead of OAuth

Pros:
- Simpler implementation
- Existing infra

Cons:
- No per-agent identity (shared keys)
- No standard discovery (agents expect OAuth)
- No user consent flow

**Rejected**: MCP spec recommends OAuth 2.1; per-agent identity is a hard requirement.

### D. stdio transport (in addition to HTTP)

Pros:
- Local agents (Claude Desktop) can use subprocess launch

Cons:
- attune is remote SaaS; stdio requires local binary
- Maintenance of two transports

**Rejected**: Streamable HTTP covers all use cases; agents can tunnel via mcp-proxy if needed.

---

## Risks / Tradeoffs

| Risk | Mitigation |
|------|------------|
| **OAuth AS complexity** | Lean implementation: PKCE only, no client credentials grant, no implicit. Use existing Tink keyset for signing. |
| **Audit log volume** | Read-only tools (`list_feedback`) logged at INFO, not audit table. Only mutations + `record_signal` audited. |
| **Token leakage** | Short-lived access tokens (1h), refresh optional. Console revocation deletes all client tokens. |
| **Rate limiting evasion** | Per-session + per-client limits. Shared across all sessions of same client. |
| **Breaking changes** | MCP tools are versioned via `tools/list` capabilities; deprecation via `deprecated: true` in tool metadata. |

---

## Implementation Plan

### Phase 1: OAuth AS + skeleton (3 days)

1. Database migration: `mcp_oauth_clients`, `mcp_oauth_codes`, `mcp_oauth_refresh_tokens`, `mcp_sessions`
2. `internal/mcp/oauth/`: client registration, authorize, token, revoke, discovery
3. Console UI: agent registration page, consent screen
4. Tests: OAuth flow e2e with test client

### Phase 2: MCP transport + read tools (3 days)

1. `internal/mcp/server.go`: JSON-RPC handler, session management
2. `internal/mcp/tools/registry.go`: tool registration
3. Read tools: `list_feedback`, `search_feedback`, `get_feedback`, `list_dimensions`, `list_tags`, `get_digest`
4. Wire to router: `r.Mount("/mcp", mcpHandler.Routes())`
5. Tests: tool execution with mocked service layer

### Phase 3: Write + ingest tools (2 days)

1. Write tools: `update_status`, `update_tags`, `reclassify`, `link_issue`, `mark_duplicate`, batch ops
2. Ingest tool: `record_signal`
3. Scope enforcement middleware
4. Tests: scope denial, successful mutations

### Phase 4: Audit + observability (2 days)

1. `internal/mcp/audit.go`: audit recorder integration
2. Extend `audit_log` constraint with MCP actions
3. Console UI: agent activity timeline
4. Metrics: `attune_mcp_*` counters and histograms
5. Tests: audit row verification

### Phase 5: Documentation + polish (1 day)

1. Update `docs/private-deploy.md` with MCP config
2. OpenAPI spec for OAuth endpoints
3. Example `mcp.json` for Claude Desktop / Cursor
4. CHANGELOG entry

**Total: ~11 days**

---

## Verification

### Unit tests

- OAuth flow: authorize → token → refresh → revoke
- Tool execution: each tool with valid/invalid inputs
- Scope enforcement: denied without scope, allowed with scope
- Audit recording: verify row structure

### Integration tests

- Full OAuth flow with test HTTP client
- Tool calls through JSON-RPC endpoint
- Session persistence across requests
- Rate limiting behavior

### E2e verification

- Register agent via Console
- Authorize via browser (OIDC login)
- Call tools from Claude Desktop with `mcp.json` config
- Verify audit log in Console
- Revoke agent, verify subsequent calls fail

### Compliance verification

- SOC 2 auditor can trace agent actions via CSV export
- EU AI Act: 6-month retention, agent identity preserved

---

## References

### MCP specification

- [Model Context Protocol](https://modelcontextprotocol.io/)
- [MCP Specification 2025-06-18](https://modelcontextprotocol.io/specification/2025-06-18)
- [Streamable HTTP Transport](https://modelcontextprotocol.io/specification/2025-06-18/basic/transports#streamable-http)

### OAuth 2.1

- [OAuth 2.1 Draft](https://datatracker.ietf.org/doc/html/draft-ietf-oauth-v2-1)
- [PKCE RFC 7636](https://datatracker.ietf.org/doc/html/rfc7636)

### Industry implementations

- [GitHub MCP Server](https://github.com/github/github-mcp-server) — 51 tools, remote OAuth
- [Stripe MCP](https://docs.stripe.com/mcp) — OAuth + local API key dual mode
- [Sentry MCP](https://mcp.sentry.dev/) — OAuth, AI integration
- [Linear API](https://developers.linear.app/) — GraphQL, cursor pagination

### Go MCP SDK

- [mark3labs/mcp-go](https://github.com/mark3labs/mcp-go) — Go MCP server SDK

### attune proposals

- [#41 API Key Scopes](2026-06-18-api-key-scopes.md)
- [#39 Audit Log](2026-06-16-audit-log-sensitive-console-actions.md)
- [#40 OIDC SSO](2026-06-15-oidc-sso.md)
- [#66 Channel-agnostic Inbound](2026-06-08-channel-agnostic-inbound.md)
