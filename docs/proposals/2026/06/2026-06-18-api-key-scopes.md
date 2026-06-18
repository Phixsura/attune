# API Key Scopes: Fine-grained Permission Control

| | |
|---|---|
| **Issue** | #41 |
| **Status** | Proposed |
| **Started** | 2026-06-18 |
| **Related** | #38 (RBAC — similar permission model for sessions), #39 (audit log — scope changes logged) |

---

## Problem

Today every API key has full write access to its tenant. The middleware validates key existence but grants unrestricted access to all endpoints.

**Evidence from codebase:**

1. **No scope concept** — `apikey.Middleware` (middleware.go:59-98) only stores `TenantID` and `KeyID` in context. No scope field exists.

2. **Flat authorization** — All API-key-authenticated requests pass through with identical privileges. A key intended for ingest can also revoke other keys.

3. **Schema lacks scopes** — `external_api_keys` table (001_init.sql:104-114) has no scopes column.

4. **Console API inaccessible to API keys** — Currently Console endpoints only accept session auth. Operators cannot automate Console workflows via API key.

**Impact:**

- **Security risk** — A leaked ingest key grants full tenant access
- **Least-privilege violation** — No way to create read-only keys for monitoring dashboards
- **Automation gap** — Cannot use API keys for Console automation (e.g., configuring LLM channels programmatically)
- **Enterprise blocker** — Fine-grained API permissions are table-stakes for enterprise security audits

---

## Industry Research Summary

Surveyed 10 top projects for API key scope design:

| Project | Model | Key Insight |
|---------|-------|-------------|
| **GitHub** | Fine-grained PATs | `resource:action` format; tiered levels (read < write < admin) |
| **GitLab** | PAT scopes | `read_*` / `write_*` prefix pattern |
| **Stripe** | Restricted keys | Per-resource permission toggles |
| **AWS IAM** | Ultra-fine policies | Action/Resource/Effect; explicit deny wins |
| **GCP IAM** | Dot-notation | Hierarchical inheritance via roles |
| **Slack** | Colon-notation | `channels:read`, `chat:write` pattern |
| **Linear** | OAuth scopes | Additive scope combination |
| **Discord** | Bitmask | `ADMINISTRATOR` flag overrides all |
| **Twilio** | Path-based | Restricted keys use path patterns |
| **OpenAI** | Resource.action | Per-resource None/Read/Write/Full assignment |

**Best fit for attune:**
- **Slack's colon notation** — `resource:action` is human-readable and extensible
- **GitHub's tiered levels** — `write` implies `read`, reducing grant redundancy
- **Linear's additive combination** — Simple union, no explicit deny complexity
- **Separate admin scopes** — Operational vs management access isolation

---

## Goals

| Category | Goal |
|----------|------|
| **Scope Model** | 22 fine-grained scopes covering all resources |
| | `resource:action` string format (e.g., `feedback:read`) |
| | Hierarchy: `write` implies `read`; `admin` implies both |
| | Additive combination: union of granted scopes |
| **Data Model** | Normalized `api_key_scopes(key_id, scope)` join table |
| | Immutable after creation: revoke + recreate to change scopes |
| | Migration: existing keys get all 22 scopes (zero disruption) |
| **Backend** | `RequireScope(scope)` middleware for endpoint protection |
| | Dual auth: Console endpoints accept both session AND API key |
| | Session users bypass scope checks (handled by RBAC) |
| **Frontend** | Preset templates: Ingest Only, Read Only, Integration, Full Access |
| | Custom scope selection via grouped checkboxes |
| | Scopes displayed on key list page |
| **Observability** | Scope-denied requests logged with key/scope context |
| | Metrics: `attune_apikey_denied_total{scope}` |
| **Testing** | Table-driven: every endpoint × scope combination |
| | Integration: migration seeds existing keys correctly |
| | E2E: read-only key can list but not ingest |

---

## Non-Goals

| Scope | Rationale |
|-------|-----------|
| **Resource-instance scopes** | No `feedback:read:123` — scopes are resource-level, not row-level |
| **Dynamic scope modification** | Keys are immutable post-creation; prevents privilege escalation |
| **Scope hierarchy at storage** | Store explicit scopes; resolve hierarchy at runtime |
| **Cross-tenant scopes** | All scopes are tenant-bound; no multi-tenant keys |

---

## Proposal

### Scope Definitions (22 total)

#### Core Resources
| Scope | Purpose | Endpoints |
|-------|---------|-----------|
| `ingest:write` | Submit feedback | `POST /v1/feedback/ingest` |
| `feedback:read` | View feedback, clusters, stats | `GET /feedback/*`, `GET /clusters/*` |
| `feedback:write` | Modify state, tags, batch ops | `POST /feedback/*/transition`, `POST /feedback/batch/*` |
| `usage:read` | View usage statistics | `GET /usage`, `GET /llm-usage` |
| `audit:read` | View audit log | `GET /audit-log/*` |

#### AI Configuration
| Scope | Purpose | Endpoints |
|-------|---------|-----------|
| `llm:read` | View LLM channels, abilities, routes | `GET /llm/*` |
| `llm:write` | Configure LLM settings | `POST/PUT/PATCH/DELETE /llm/*` |
| `enrich:read` | View enrichment config | `GET /enrich-config/*`, `GET /enrichment-runtime/*` |
| `enrich:write` | Modify enrichment settings | `PUT /enrich-config/*`, `POST /enrichment-runtime/*` |
| `guard:read` | View guard policies | `GET /guard-policies/*` |
| `guard:write` | Configure guard policies | `POST/PUT/PATCH/DELETE /guard-policies/*` |

#### Notifications & Integrations
| Scope | Purpose | Endpoints |
|-------|---------|-----------|
| `notify:read` | View notify targets | `GET /notify-targets/*` |
| `notify:write` | Configure notify targets | `POST/PATCH/DELETE /notify-targets/*` |
| `inbound:read` | View inbound sources | `GET /inbound/sources/*` |
| `inbound:write` | Configure inbound sources | `POST/DELETE /inbound/sources/*` |
| `digest:read` | View digest subscription | `GET /digest-subscription` |
| `digest:write` | Configure digest subscription | `PUT/DELETE /digest-subscription` |

#### Workflow & Tags
| Scope | Purpose | Endpoints |
|-------|---------|-----------|
| `tags:read` | View tags | `GET /tags/*` |
| `tags:write` | Configure tags | `POST/PATCH/DELETE /tags/*` |
| `workflow:read` | View workflow states | `GET /workflow/*` |
| `workflow:write` | Configure workflow | `POST/PUT/PATCH/DELETE /workflow/*` |

#### Admin
| Scope | Purpose | Endpoints |
|-------|---------|-----------|
| `gdpr:admin` | GDPR delete/export operations | `POST /gdpr/delete`, `POST /gdpr/export` |
| `members:admin` | Manage tenant members | `POST/PATCH/DELETE /members/*` |
| `apikey:admin` | Manage API keys | `POST/DELETE /api-keys/*` |

### Hierarchy Rules

```
write implies read:
  feedback:write → feedback:read
  llm:write → llm:read
  enrich:write → enrich:read
  guard:write → guard:read
  notify:write → notify:read
  inbound:write → inbound:read
  digest:write → digest:read
  tags:write → tags:read
  workflow:write → workflow:read

admin scopes are standalone (no sub-scopes):
  gdpr:admin, members:admin, apikey:admin
```

### Schema

```sql
-- Migration 046: API key scopes
BEGIN;

CREATE TABLE IF NOT EXISTS api_key_scopes (
    key_id      UUID        NOT NULL REFERENCES external_api_keys(id) ON DELETE CASCADE,
    scope       TEXT        NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (key_id, scope)
);

CREATE INDEX IF NOT EXISTS idx_api_key_scopes_key
  ON api_key_scopes (key_id);

-- Seed existing keys with all scopes
INSERT INTO api_key_scopes (key_id, scope)
SELECT k.id, s.scope
FROM external_api_keys k
CROSS JOIN (
    VALUES 
    ('ingest:write'),
    ('feedback:read'), ('feedback:write'),
    ('usage:read'), ('audit:read'),
    ('llm:read'), ('llm:write'),
    ('enrich:read'), ('enrich:write'),
    ('guard:read'), ('guard:write'),
    ('notify:read'), ('notify:write'),
    ('inbound:read'), ('inbound:write'),
    ('digest:read'), ('digest:write'),
    ('tags:read'), ('tags:write'),
    ('workflow:read'), ('workflow:write'),
    ('gdpr:admin'), ('members:admin'), ('apikey:admin')
) AS s(scope)
WHERE k.revoked_at IS NULL;

COMMIT;
```

### Domain Model

```go
// internal/domain/scope.go

package domain

type Scope string

const (
    ScopeIngestWrite   Scope = "ingest:write"
    ScopeFeedbackRead  Scope = "feedback:read"
    ScopeFeedbackWrite Scope = "feedback:write"
    // ... all 22 scopes
)

var AllScopes = []Scope{...}

var scopeHierarchy = map[Scope][]Scope{
    ScopeFeedbackWrite: {ScopeFeedbackRead},
    ScopeLLMWrite:      {ScopeLLMRead},
    // ... hierarchy rules
}

// HasScope checks if granted scopes satisfy required, respecting hierarchy.
func HasScope(granted []Scope, required Scope) bool {
    for _, s := range granted {
        if s == required {
            return true
        }
        for _, implied := range scopeHierarchy[s] {
            if implied == required {
                return true
            }
        }
    }
    return false
}
```

### Dual Auth Middleware

Console endpoints accept both session auth AND API key auth:

```go
// internal/handlers/console/internal/auth/dual_auth.go

type DualAuthCtx struct {
    TenantID   string
    UserID     string
    UserType   string           // "admin" | "oidc" | "apikey"
    Scopes     []domain.Scope   // Only for API keys
    IsAPIKey   bool
}

// DualAuthMiddleware: try session first, then API key
func DualAuthMiddleware(sessionSigner, apiKeyVerifier, scopeLoader) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            // 1. Try session auth
            if auth, err := sessionSigner.VerifySession(r); err == nil {
                ctx := WithDualAuth(ctx, &DualAuthCtx{
                    TenantID: auth.TenantID,
                    UserID:   auth.UserID,
                    UserType: auth.UserType,
                    IsAPIKey: false,
                })
                next.ServeHTTP(w, r.WithContext(ctx))
                return
            }
            
            // 2. Try API key auth
            raw := r.Header.Get("X-API-Key")
            if raw != "" {
                tenantID, keyID, err := apiKeyVerifier.Lookup(ctx, raw)
                if err == nil {
                    scopes, _ := scopeLoader.LoadScopes(ctx, keyID)
                    ctx := WithDualAuth(ctx, &DualAuthCtx{
                        TenantID: tenantID,
                        UserID:   keyID.String(),
                        UserType: "apikey",
                        Scopes:   scopes,
                        IsAPIKey: true,
                    })
                    next.ServeHTTP(w, r.WithContext(ctx))
                    return
                }
            }
            
            // 3. Both failed
            dispatcher.Reject(ctx, w, 401, UNAUTHORIZED, "authentication required")
        })
    }
}

// RequireScope: check scope for API key requests, pass-through for sessions
func RequireScope(required domain.Scope) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            auth := FromDualAuth(r.Context())
            
            // Session users bypass scope checks (handled by RBAC)
            if !auth.IsAPIKey {
                next.ServeHTTP(w, r)
                return
            }
            
            // API key: check scope
            if !domain.HasScope(auth.Scopes, required) {
                dispatcher.Reject(ctx, w, 403, FORBIDDEN, 
                    fmt.Sprintf("missing scope: %s", required))
                return
            }
            
            next.ServeHTTP(w, r)
        })
    }
}
```

### Proto Changes

```protobuf
// proto/attune/v1/api_key.proto

message ApiKey {
  string id = 1;
  string key_prefix = 2;
  string label = 3;
  bool is_active = 4;
  string created_at = 5;
  optional string last_used_at = 6;
  optional string revoked_at = 7;
  repeated string scopes = 8;  // NEW
}

message CreateApiKeyRequest {
  string label = 1 [(google.api.field_behavior) = REQUIRED];
  repeated string scopes = 2 [(google.api.field_behavior) = REQUIRED];  // NEW
}

// NEW: scope metadata
message ListScopesRequest {}
message ListScopesResponse {
  repeated ScopeInfo scopes = 1;
}
message ScopeInfo {
  string id = 1;           // "feedback:read"
  string resource = 2;     // "feedback"
  string action = 3;       // "read"
  string description = 4;
  repeated string implies = 5;
}

// NEW: presets
message ListScopePresetsRequest {}
message ListScopePresetsResponse {
  repeated ScopePreset presets = 1;
}
message ScopePreset {
  string id = 1;           // "ingest_only"
  string name = 2;         // "Ingest Only"
  string description = 3;
  repeated string scopes = 4;
}
```

### Preset Templates

| ID | Name | Scopes |
|----|------|--------|
| `ingest_only` | Ingest Only | `ingest:write` |
| `read_only` | Read Only | `feedback:read`, `usage:read`, `audit:read` |
| `integration` | Integration | `ingest:write`, `feedback:read`, `notify:read`, `inbound:read` |
| `full_access` | Full Access | All 22 scopes |

### Console UI

Create API Key dialog:
1. Label input (existing)
2. Preset selector (radio buttons)
3. Expandable "Advanced" section with scope checkboxes grouped by resource
4. Created key shows scope list in confirmation dialog

Key list page:
- Scopes column with tooltip showing full list
- Active keys show scope badges

---

## Alternatives Considered

| Alternative | Why Rejected |
|-------------|--------------|
| **TEXT[] column** | Less query flexibility; can't easily find "keys with gdpr:admin" |
| **Coarse 3-scope model** | Issue #41 originally proposed; user requested fine-grained after research |
| **Handler-level checks** | Simple but repetitive; chose RBAC-style middleware for DRY |
| **Post-creation modification** | Security risk; privilege escalation attack surface |

---

## Risks / Tradeoffs

| Risk | Mitigation |
|------|------------|
| **Scope explosion** | 22 scopes is manageable; presets simplify common cases |
| **Dual-auth complexity** | Clear precedence (session > apikey); well-tested middleware |
| **Migration disruption** | All existing keys get full scopes; zero breaking change |
| **UI complexity** | Presets for 80% case; custom for power users |

---

## Implementation Plan

| Phase | Work |
|-------|------|
| **1. Schema** | Migration 046 with `api_key_scopes` table + seed |
| **2. Domain** | `internal/domain/scope.go` with types, hierarchy, `HasScope` |
| **3. Repo** | `internal/repo/apikeyScope/` for scope CRUD |
| **4. Service** | Update `apikey.Issue` to accept scopes; add `LoadScopes` |
| **5. Middleware** | `DualAuthMiddleware`, `RequireScope` |
| **6. Handlers** | Add `RequireScope` to all Console endpoints |
| **7. Proto** | Update `api_key.proto`, run `make proto` |
| **8. Console handlers** | Pass scopes to service; add preset/scope list endpoints |
| **9. Console UI** | New create dialog, scope display on list page |
| **10. Tests** | Unit, integration, handler table-driven, E2E |
| **11. Docs** | README, private-deploy.md scope documentation |

---

## Verification

| Check | Method |
|-------|--------|
| **Unit tests pass** | `go test ./internal/domain/... ./internal/service/apikey/...` |
| **Integration tests** | `make test-integration` — scope repo + migration |
| **Handler tests** | Table-driven: every endpoint × scope × allow/deny |
| **Console tests** | `pnpm vitest` — dialog interaction, preset selection |
| **E2E** | Create read-only key → can GET feedback → cannot POST ingest |
| **Existing keys work** | Verify migrated keys have all scopes |

---

## References

- [GitHub Fine-grained PATs](https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/managing-your-personal-access-tokens)
- [Stripe Restricted API Keys](https://docs.stripe.com/keys)
- [Slack Token Scopes](https://docs.slack.dev/authentication/tokens)
- [OpenAI API Key Permissions](https://help.openai.com/en/articles/8867743-assign-api-key-permissions)
- [AWS IAM Policies](https://docs.aws.amazon.com/IAM/latest/UserGuide/access_policies.html)
