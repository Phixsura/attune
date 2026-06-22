# MCP Server Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Expose attune as an agent-accessible tool surface via Model Context Protocol (MCP), with OAuth 2.1 authentication, 22 tools, and audit integration.

**Architecture:** Integrated MCP server in `cmd/attune` binary. OAuth 2.1 Authorization Server with PKCE. Streamable HTTP transport for JSON-RPC 2.0. Tools call service layer directly. Audit via existing `audit_log` table with `actor_type='mcp_agent'`.

**Tech Stack:** Go 1.22+, chi router, pgx for Postgres, JWT for tokens, JSON-RPC 2.0, JSON Schema for tool definitions.

---

## File Structure

```
internal/
├── mcp/
│   ├── server.go              # MCP JSON-RPC handler
│   ├── server_test.go
│   ├── session.go             # Session management (Mcp-Session-Id)
│   ├── session_test.go
│   ├── audit.go               # Audit recorder wrapper
│   ├── audit_test.go
│   ├── jsonrpc/
│   │   ├── types.go           # JSON-RPC 2.0 request/response types
│   │   └── errors.go          # Standard error codes
│   ├── tools/
│   │   ├── registry.go        # Tool registration + discovery
│   │   ├── registry_test.go
│   │   ├── types.go           # Tool, Result, Content types
│   │   ├── errors.go          # Tool-specific errors
│   │   ├── list_feedback.go
│   │   ├── list_feedback_test.go
│   │   ├── search_feedback.go
│   │   ├── search_feedback_test.go
│   │   ├── get_feedback.go
│   │   ├── get_feedback_test.go
│   │   ├── list_dimensions.go
│   │   ├── list_tags.go
│   │   ├── get_digest.go
│   │   ├── update_status.go
│   │   ├── update_status_test.go
│   │   ├── update_tags.go
│   │   ├── record_signal.go
│   │   └── record_signal_test.go
│   └── oauth/
│       ├── server.go          # OAuth AS core
│       ├── server_test.go
│       ├── authorize.go       # GET /mcp/oauth/authorize
│       ├── token.go           # POST /mcp/oauth/token
│       ├── revoke.go          # POST /mcp/oauth/revoke
│       ├── discovery.go       # GET /.well-known/oauth-protected-resource
│       ├── client.go          # Client registration domain
│       ├── pkce.go            # PKCE utilities
│       ├── pkce_test.go
│       └── jwt.go             # JWT signing/validation
├── domain/
│   └── scope.go               # Add MCP scopes (modify existing)
├── service/
│   └── auditlog/
│       └── actions.go         # Add MCP actions (modify existing)
├── repo/
│   └── mcp/
│       ├── clients.go         # OAuth clients CRUD
│       ├── clients_test.go
│       ├── codes.go           # Authorization codes
│       ├── tokens.go          # Refresh tokens
│       └── sessions.go        # MCP sessions
├── infra/
│   ├── config/
│   │   └── config.go          # Add MCPConfig (modify existing)
│   └── database/
│       └── migrations/
│           ├── 058_mcp_oauth.sql
│           └── 059_mcp_audit_actions.sql
├── handlers/
│   └── console/
│       └── mcp/
│           ├── agents.go      # List/revoke agents
│           ├── agents_test.go
│           ├── authorize_ui.go # OAuth consent screen
│           └── activity.go    # Agent activity timeline
cmd/
└── attune/
    └── router.go              # Mount MCP routes (modify existing)
```

---

## Phase 1: Database + Domain Foundation

### Task 1.1: Create Migration 058 - MCP OAuth Tables

**Files:**
- Create: `internal/infra/database/migrations/058_mcp_oauth.sql`

- [ ] **Step 1: Write the migration SQL**

```sql
-- Migration 058: MCP OAuth clients, tokens, and sessions
--
-- Tables for MCP OAuth 2.1 Authorization Server:
-- - mcp_oauth_clients: registered agents
-- - mcp_oauth_codes: short-lived authorization codes
-- - mcp_oauth_refresh_tokens: long-lived refresh tokens
-- - mcp_sessions: MCP session tracking (Mcp-Session-Id)

BEGIN;

CREATE TABLE mcp_oauth_clients (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       TEXT        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name            TEXT        NOT NULL,
    redirect_uris   TEXT[]      NOT NULL,
    scopes          TEXT[]      NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by      TEXT        NOT NULL,
    revoked_at      TIMESTAMPTZ,

    CONSTRAINT uq_mcp_client_name_tenant UNIQUE (tenant_id, name),
    CONSTRAINT chk_mcp_client_name_length CHECK (length(name) BETWEEN 1 AND 128),
    CONSTRAINT chk_mcp_client_redirect_uris CHECK (array_length(redirect_uris, 1) >= 1)
);

CREATE INDEX idx_mcp_oauth_clients_tenant ON mcp_oauth_clients(tenant_id) WHERE revoked_at IS NULL;

CREATE TABLE mcp_oauth_codes (
    code            TEXT        PRIMARY KEY,
    client_id       UUID        NOT NULL REFERENCES mcp_oauth_clients(id) ON DELETE CASCADE,
    redirect_uri    TEXT        NOT NULL,
    scopes          TEXT[]      NOT NULL,
    code_challenge  TEXT        NOT NULL,
    user_id         TEXT        NOT NULL,
    expires_at      TIMESTAMPTZ NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT chk_mcp_code_length CHECK (length(code) = 64)
);

CREATE INDEX idx_mcp_oauth_codes_expires ON mcp_oauth_codes(expires_at);

CREATE TABLE mcp_oauth_refresh_tokens (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    client_id       UUID        NOT NULL REFERENCES mcp_oauth_clients(id) ON DELETE CASCADE,
    token_hash      TEXT        NOT NULL UNIQUE,
    scopes          TEXT[]      NOT NULL,
    user_id         TEXT        NOT NULL,
    expires_at      TIMESTAMPTZ NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    revoked_at      TIMESTAMPTZ,

    CONSTRAINT chk_mcp_refresh_token_hash_length CHECK (length(token_hash) = 64)
);

CREATE INDEX idx_mcp_refresh_tokens_client ON mcp_oauth_refresh_tokens(client_id) WHERE revoked_at IS NULL;

CREATE TABLE mcp_sessions (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    client_id       UUID        NOT NULL REFERENCES mcp_oauth_clients(id) ON DELETE CASCADE,
    tenant_id       TEXT        NOT NULL,
    scopes          TEXT[]      NOT NULL,
    last_active_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    closed_at       TIMESTAMPTZ
);

CREATE INDEX idx_mcp_sessions_client ON mcp_sessions(client_id) WHERE closed_at IS NULL;
CREATE INDEX idx_mcp_sessions_last_active ON mcp_sessions(last_active_at) WHERE closed_at IS NULL;

COMMIT;
```

- [ ] **Step 2: Verify migration applies**

Run: `make migrate-up` or start the server to auto-migrate.

Expected: Migration 058 applied successfully, tables created.

- [ ] **Step 3: Commit**

```bash
git add internal/infra/database/migrations/058_mcp_oauth.sql
git commit -m "feat(mcp): add migration 058 - OAuth tables for MCP server (#93)"
```

---

### Task 1.2: Create Migration 059 - MCP Audit Actions

**Files:**
- Create: `internal/infra/database/migrations/059_mcp_audit_actions.sql`

- [ ] **Step 1: Write the migration SQL**

```sql
-- Migration 059: MCP audit actions
--
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

- [ ] **Step 2: Commit**

```bash
git add internal/infra/database/migrations/059_mcp_audit_actions.sql
git commit -m "feat(mcp): add migration 059 - MCP audit actions (#93)"
```

---

### Task 1.3: Add MCP Scopes to Domain

**Files:**
- Modify: `internal/domain/scope.go`
- Modify: `internal/domain/scope_test.go`

- [ ] **Step 1: Write the test for new scopes**

Add to `internal/domain/scope_test.go`:

```go
func TestMCPScopes(t *testing.T) {
	t.Run("MCP scopes are valid", func(t *testing.T) {
		assert.True(t, ScopeMCPRead.IsValid())
		assert.True(t, ScopeMCPWrite.IsValid())
		assert.True(t, ScopeMCPIngest.IsValid())
	})

	t.Run("MCP write implies read", func(t *testing.T) {
		granted := []Scope{ScopeMCPWrite}
		assert.True(t, HasScope(granted, ScopeMCPRead))
		assert.True(t, HasScope(granted, ScopeMCPWrite))
	})

	t.Run("MCP ingest does not imply read", func(t *testing.T) {
		granted := []Scope{ScopeMCPIngest}
		assert.False(t, HasScope(granted, ScopeMCPRead))
		assert.True(t, HasScope(granted, ScopeMCPIngest))
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/domain/... -run TestMCPScopes -v`

Expected: FAIL - `ScopeMCPRead` undefined

- [ ] **Step 3: Add MCP scope constants**

Add to `internal/domain/scope.go` after existing constants:

```go
const (
	// ... existing scopes ...

	// MCP scopes
	ScopeMCPRead   Scope = "mcp:read"
	ScopeMCPWrite  Scope = "mcp:write"
	ScopeMCPIngest Scope = "mcp:ingest"
)
```

- [ ] **Step 4: Add MCP scopes to AllScopes**

Update `AllScopes` in `internal/domain/scope.go`:

```go
var AllScopes = []Scope{
	// ... existing scopes ...
	ScopeMCPRead, ScopeMCPWrite, ScopeMCPIngest,
}
```

- [ ] **Step 5: Add MCP scope hierarchy**

Update `scopeHierarchy` in `internal/domain/scope.go`:

```go
var scopeHierarchy = map[Scope][]Scope{
	// ... existing hierarchy ...
	ScopeMCPWrite: {ScopeMCPRead},
	// ScopeMCPIngest has no hierarchy - standalone
}
```

- [ ] **Step 6: Run test to verify it passes**

Run: `go test ./internal/domain/... -run TestMCPScopes -v`

Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/domain/scope.go internal/domain/scope_test.go
git commit -m "feat(mcp): add mcp:read, mcp:write, mcp:ingest scopes (#93)"
```

---

### Task 1.4: Add MCP Actions to Audit Service

**Files:**
- Modify: `internal/service/auditlog/actions.go`

- [ ] **Step 1: Add MCP actions to validActions map**

Add to `internal/service/auditlog/actions.go`:

```go
var validActions = map[string]struct{}{
	// ... existing actions ...

	// MCP tool calls
	"mcp.list_feedback":       {},
	"mcp.search_feedback":     {},
	"mcp.get_feedback":        {},
	"mcp.list_dimensions":     {},
	"mcp.list_tags":           {},
	"mcp.list_clusters":       {},
	"mcp.get_cluster":         {},
	"mcp.get_digest":          {},
	"mcp.get_usage_stats":     {},
	"mcp.update_status":       {},
	"mcp.update_tags":         {},
	"mcp.reclassify":          {},
	"mcp.link_issue":          {},
	"mcp.mark_duplicate":      {},
	"mcp.batch_update_status": {},
	"mcp.batch_update_tags":   {},
	"mcp.record_signal":       {},
	"mcp.create_tag":          {},
	"mcp.archive_tag":         {},
	"mcp.trigger_digest":      {},
	"mcp.get_enrichment_status": {},
	"mcp.retry_enrichment":    {},

	// MCP OAuth admin actions
	"mcp_client.create": {},
	"mcp_client.revoke": {},
}
```

- [ ] **Step 2: Run existing tests**

Run: `go test ./internal/service/auditlog/... -v`

Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/service/auditlog/actions.go
git commit -m "feat(mcp): add MCP tool actions to audit allow-list (#93)"
```

---

### Task 1.5: Add MCP Config Section

**Files:**
- Modify: `internal/infra/config/config.go`

- [ ] **Step 1: Add MCPConfig struct**

Add to `internal/infra/config/config.go`:

```go
type MCPConfig struct {
	Enabled bool `yaml:"enabled"`

	OAuth MCPOAuthConfig `yaml:"oauth"`

	RateLimit MCPRateLimitConfig `yaml:"rate_limit"`

	AllowedRedirectPatterns []string `yaml:"allowed_redirect_patterns"`
}

type MCPOAuthConfig struct {
	Issuer          string `yaml:"issuer"`
	AccessTokenTTL  string `yaml:"access_token_ttl"`
	RefreshTokenTTL string `yaml:"refresh_token_ttl"`
}

type MCPRateLimitConfig struct {
	RequestsPerMinute int `yaml:"requests_per_minute"`
	Burst             int `yaml:"burst"`
}
```

- [ ] **Step 2: Add MCP field to Config struct**

Add to `Config` struct in `internal/infra/config/config.go`:

```go
type Config struct {
	// ... existing fields ...
	MCP MCPConfig
	
	// Convenience fields for MCP
	MCPEnabled              bool
	MCPAccessTokenTTL       time.Duration
	MCPRefreshTokenTTL      time.Duration
	MCPRateLimitPerMinute   int
	MCPRateLimitBurst       int
}
```

- [ ] **Step 3: Add MCP parsing in Load function**

Add to the `Load` function after existing parsing:

```go
// MCP defaults
if cfg.MCP.OAuth.AccessTokenTTL == "" {
	cfg.MCP.OAuth.AccessTokenTTL = "1h"
}
if cfg.MCP.OAuth.RefreshTokenTTL == "" {
	cfg.MCP.OAuth.RefreshTokenTTL = "168h" // 7 days
}
if cfg.MCP.RateLimit.RequestsPerMinute == 0 {
	cfg.MCP.RateLimit.RequestsPerMinute = 60
}
if cfg.MCP.RateLimit.Burst == 0 {
	cfg.MCP.RateLimit.Burst = 10
}
if len(cfg.MCP.AllowedRedirectPatterns) == 0 {
	cfg.MCP.AllowedRedirectPatterns = []string{
		"http://127.0.0.1:*",
		"http://localhost:*",
	}
}

cfg.MCPEnabled = cfg.MCP.Enabled
cfg.MCPAccessTokenTTL, _ = time.ParseDuration(cfg.MCP.OAuth.AccessTokenTTL)
cfg.MCPRefreshTokenTTL, _ = time.ParseDuration(cfg.MCP.OAuth.RefreshTokenTTL)
cfg.MCPRateLimitPerMinute = cfg.MCP.RateLimit.RequestsPerMinute
cfg.MCPRateLimitBurst = cfg.MCP.RateLimit.Burst
```

- [ ] **Step 4: Run config tests**

Run: `go test ./internal/infra/config/... -v`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/infra/config/config.go
git commit -m "feat(mcp): add MCP configuration section (#93)"
```

---

## Phase 2: MCP OAuth Repository Layer

### Task 2.1: Create MCP Clients Repository

**Files:**
- Create: `internal/repo/mcp/clients.go`
- Create: `internal/repo/mcp/clients_test.go`

- [ ] **Step 1: Write the test**

Create `internal/repo/mcp/clients_test.go`:

```go
package mcp_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/repo/mcp"
	"github.com/Phixsura/attune/internal/testdb"
)

func TestClientsRepo_Create(t *testing.T) {
	pool := testdb.Pool(t)
	repo := mcp.NewClients(pool)
	ctx := context.Background()

	tenantID := testdb.SeedTenant(t, pool)

	client, err := repo.Create(ctx, mcp.CreateClientParams{
		TenantID:     tenantID,
		Name:         "Test Agent",
		RedirectURIs: []string{"http://127.0.0.1:8080/callback"},
		Scopes:       []string{"mcp:read", "mcp:write"},
		CreatedBy:    "user@example.com",
	})

	require.NoError(t, err)
	assert.NotEqual(t, uuid.Nil, client.ID)
	assert.Equal(t, "Test Agent", client.Name)
	assert.Equal(t, []string{"mcp:read", "mcp:write"}, client.Scopes)
	assert.Nil(t, client.RevokedAt)
}

func TestClientsRepo_GetByID(t *testing.T) {
	pool := testdb.Pool(t)
	repo := mcp.NewClients(pool)
	ctx := context.Background()

	tenantID := testdb.SeedTenant(t, pool)

	created, err := repo.Create(ctx, mcp.CreateClientParams{
		TenantID:     tenantID,
		Name:         "Test Agent",
		RedirectURIs: []string{"http://127.0.0.1:8080/callback"},
		Scopes:       []string{"mcp:read"},
		CreatedBy:    "user@example.com",
	})
	require.NoError(t, err)

	got, err := repo.GetByID(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, created.ID, got.ID)
	assert.Equal(t, created.Name, got.Name)
}

func TestClientsRepo_Revoke(t *testing.T) {
	pool := testdb.Pool(t)
	repo := mcp.NewClients(pool)
	ctx := context.Background()

	tenantID := testdb.SeedTenant(t, pool)

	client, err := repo.Create(ctx, mcp.CreateClientParams{
		TenantID:     tenantID,
		Name:         "Test Agent",
		RedirectURIs: []string{"http://127.0.0.1:8080/callback"},
		Scopes:       []string{"mcp:read"},
		CreatedBy:    "user@example.com",
	})
	require.NoError(t, err)

	err = repo.Revoke(ctx, client.ID)
	require.NoError(t, err)

	got, err := repo.GetByID(ctx, client.ID)
	require.NoError(t, err)
	assert.NotNil(t, got.RevokedAt)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/repo/mcp/... -run TestClientsRepo -v`

Expected: FAIL - package not found

- [ ] **Step 3: Write the implementation**

Create `internal/repo/mcp/clients.go`:

```go
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

var ErrClientNotFound = errors.New("mcp oauth client not found")

type Client struct {
	ID           uuid.UUID
	TenantID     string
	Name         string
	RedirectURIs []string
	Scopes       []string
	CreatedAt    time.Time
	CreatedBy    string
	RevokedAt    *time.Time
}

type CreateClientParams struct {
	TenantID     string
	Name         string
	RedirectURIs []string
	Scopes       []string
	CreatedBy    string
}

type ClientsRepo struct {
	pool *pgxpool.Pool
}

func NewClients(pool *pgxpool.Pool) *ClientsRepo {
	return ptrext.Of(ClientsRepo{pool: pool})
}

func (r *ClientsRepo) Create(ctx context.Context, p CreateClientParams) (*Client, error) {
	const q = `
		INSERT INTO mcp_oauth_clients (tenant_id, name, redirect_uris, scopes, created_by)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, tenant_id, name, redirect_uris, scopes, created_at, created_by, revoked_at
	`
	var c Client
	err := r.pool.QueryRow(ctx, q, p.TenantID, p.Name, p.RedirectURIs, p.Scopes, p.CreatedBy).Scan(
		&c.ID, &c.TenantID, &c.Name, &c.RedirectURIs, &c.Scopes, &c.CreatedAt, &c.CreatedBy, &c.RevokedAt,
	)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *ClientsRepo) GetByID(ctx context.Context, id uuid.UUID) (*Client, error) {
	const q = `
		SELECT id, tenant_id, name, redirect_uris, scopes, created_at, created_by, revoked_at
		FROM mcp_oauth_clients
		WHERE id = $1
	`
	var c Client
	err := r.pool.QueryRow(ctx, q, id).Scan(
		&c.ID, &c.TenantID, &c.Name, &c.RedirectURIs, &c.Scopes, &c.CreatedAt, &c.CreatedBy, &c.RevokedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrClientNotFound
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *ClientsRepo) ListByTenant(ctx context.Context, tenantID string) ([]Client, error) {
	const q = `
		SELECT id, tenant_id, name, redirect_uris, scopes, created_at, created_by, revoked_at
		FROM mcp_oauth_clients
		WHERE tenant_id = $1
		ORDER BY created_at DESC
	`
	rows, err := r.pool.Query(ctx, q, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var clients []Client
	for rows.Next() {
		var c Client
		if err := rows.Scan(&c.ID, &c.TenantID, &c.Name, &c.RedirectURIs, &c.Scopes, &c.CreatedAt, &c.CreatedBy, &c.RevokedAt); err != nil {
			return nil, err
		}
		clients = append(clients, c)
	}
	return clients, rows.Err()
}

func (r *ClientsRepo) Revoke(ctx context.Context, id uuid.UUID) error {
	const q = `UPDATE mcp_oauth_clients SET revoked_at = NOW() WHERE id = $1 AND revoked_at IS NULL`
	tag, err := r.pool.Exec(ctx, q, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrClientNotFound
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/repo/mcp/... -run TestClientsRepo -v`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/repo/mcp/clients.go internal/repo/mcp/clients_test.go
git commit -m "feat(mcp): add OAuth clients repository (#93)"
```

---

### Task 2.2: Create OAuth Codes Repository

**Files:**
- Create: `internal/repo/mcp/codes.go`
- Create: `internal/repo/mcp/codes_test.go`

- [ ] **Step 1: Write the test**

Create `internal/repo/mcp/codes_test.go`:

```go
package mcp_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/repo/mcp"
	"github.com/Phixsura/attune/internal/testdb"
)

func TestCodesRepo_CreateAndConsume(t *testing.T) {
	pool := testdb.Pool(t)
	clientsRepo := mcp.NewClients(pool)
	codesRepo := mcp.NewCodes(pool)
	ctx := context.Background()

	tenantID := testdb.SeedTenant(t, pool)

	client, err := clientsRepo.Create(ctx, mcp.CreateClientParams{
		TenantID:     tenantID,
		Name:         "Test Agent",
		RedirectURIs: []string{"http://127.0.0.1:8080/callback"},
		Scopes:       []string{"mcp:read"},
		CreatedBy:    "user@example.com",
	})
	require.NoError(t, err)

	code, err := codesRepo.Create(ctx, mcp.CreateCodeParams{
		ClientID:      client.ID,
		RedirectURI:   "http://127.0.0.1:8080/callback",
		Scopes:        []string{"mcp:read"},
		CodeChallenge: "abc123xyz",
		UserID:        "user@example.com",
		ExpiresAt:     time.Now().Add(10 * time.Minute),
	})
	require.NoError(t, err)
	assert.Len(t, code.Code, 64)

	consumed, err := codesRepo.Consume(ctx, code.Code)
	require.NoError(t, err)
	assert.Equal(t, client.ID, consumed.ClientID)
	assert.Equal(t, "abc123xyz", consumed.CodeChallenge)

	// Second consume should fail
	_, err = codesRepo.Consume(ctx, code.Code)
	assert.ErrorIs(t, err, mcp.ErrCodeNotFound)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/repo/mcp/... -run TestCodesRepo -v`

Expected: FAIL - NewCodes undefined

- [ ] **Step 3: Write the implementation**

Create `internal/repo/mcp/codes.go`:

```go
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

var ErrCodeNotFound = errors.New("authorization code not found or expired")

type AuthCode struct {
	Code          string
	ClientID      uuid.UUID
	RedirectURI   string
	Scopes        []string
	CodeChallenge string
	UserID        string
	ExpiresAt     time.Time
	CreatedAt     time.Time
}

type CreateCodeParams struct {
	ClientID      uuid.UUID
	RedirectURI   string
	Scopes        []string
	CodeChallenge string
	UserID        string
	ExpiresAt     time.Time
}

type CodesRepo struct {
	pool *pgxpool.Pool
}

func NewCodes(pool *pgxpool.Pool) *CodesRepo {
	return ptrext.Of(CodesRepo{pool: pool})
}

func (r *CodesRepo) Create(ctx context.Context, p CreateCodeParams) (*AuthCode, error) {
	code, err := generateCode()
	if err != nil {
		return nil, err
	}

	const q = `
		INSERT INTO mcp_oauth_codes (code, client_id, redirect_uri, scopes, code_challenge, user_id, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING code, client_id, redirect_uri, scopes, code_challenge, user_id, expires_at, created_at
	`
	var c AuthCode
	err = r.pool.QueryRow(ctx, q, code, p.ClientID, p.RedirectURI, p.Scopes, p.CodeChallenge, p.UserID, p.ExpiresAt).Scan(
		&c.Code, &c.ClientID, &c.RedirectURI, &c.Scopes, &c.CodeChallenge, &c.UserID, &c.ExpiresAt, &c.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *CodesRepo) Consume(ctx context.Context, code string) (*AuthCode, error) {
	const q = `
		DELETE FROM mcp_oauth_codes
		WHERE code = $1 AND expires_at > NOW()
		RETURNING code, client_id, redirect_uri, scopes, code_challenge, user_id, expires_at, created_at
	`
	var c AuthCode
	err := r.pool.QueryRow(ctx, q, code).Scan(
		&c.Code, &c.ClientID, &c.RedirectURI, &c.Scopes, &c.CodeChallenge, &c.UserID, &c.ExpiresAt, &c.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrCodeNotFound
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *CodesRepo) Cleanup(ctx context.Context) (int64, error) {
	const q = `DELETE FROM mcp_oauth_codes WHERE expires_at < NOW()`
	tag, err := r.pool.Exec(ctx, q)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func generateCode() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/repo/mcp/... -run TestCodesRepo -v`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/repo/mcp/codes.go internal/repo/mcp/codes_test.go
git commit -m "feat(mcp): add OAuth authorization codes repository (#93)"
```

---

### Task 2.3: Create MCP Sessions Repository

**Files:**
- Create: `internal/repo/mcp/sessions.go`
- Create: `internal/repo/mcp/sessions_test.go`

- [ ] **Step 1: Write the test**

Create `internal/repo/mcp/sessions_test.go`:

```go
package mcp_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/repo/mcp"
	"github.com/Phixsura/attune/internal/testdb"
)

func TestSessionsRepo_CreateAndGet(t *testing.T) {
	pool := testdb.Pool(t)
	clientsRepo := mcp.NewClients(pool)
	sessionsRepo := mcp.NewSessions(pool)
	ctx := context.Background()

	tenantID := testdb.SeedTenant(t, pool)

	client, err := clientsRepo.Create(ctx, mcp.CreateClientParams{
		TenantID:     tenantID,
		Name:         "Test Agent",
		RedirectURIs: []string{"http://127.0.0.1:8080/callback"},
		Scopes:       []string{"mcp:read"},
		CreatedBy:    "user@example.com",
	})
	require.NoError(t, err)

	sess, err := sessionsRepo.Create(ctx, mcp.CreateSessionParams{
		ClientID: client.ID,
		TenantID: tenantID,
		Scopes:   []string{"mcp:read"},
	})
	require.NoError(t, err)
	assert.NotEqual(t, uuid.Nil, sess.ID)

	got, err := sessionsRepo.GetByID(ctx, sess.ID)
	require.NoError(t, err)
	assert.Equal(t, sess.ID, got.ID)
	assert.Equal(t, tenantID, got.TenantID)
}

func TestSessionsRepo_Touch(t *testing.T) {
	pool := testdb.Pool(t)
	clientsRepo := mcp.NewClients(pool)
	sessionsRepo := mcp.NewSessions(pool)
	ctx := context.Background()

	tenantID := testdb.SeedTenant(t, pool)

	client, err := clientsRepo.Create(ctx, mcp.CreateClientParams{
		TenantID:     tenantID,
		Name:         "Test Agent",
		RedirectURIs: []string{"http://127.0.0.1:8080/callback"},
		Scopes:       []string{"mcp:read"},
		CreatedBy:    "user@example.com",
	})
	require.NoError(t, err)

	sess, err := sessionsRepo.Create(ctx, mcp.CreateSessionParams{
		ClientID: client.ID,
		TenantID: tenantID,
		Scopes:   []string{"mcp:read"},
	})
	require.NoError(t, err)

	originalLastActive := sess.LastActiveAt

	err = sessionsRepo.Touch(ctx, sess.ID)
	require.NoError(t, err)

	got, err := sessionsRepo.GetByID(ctx, sess.ID)
	require.NoError(t, err)
	assert.True(t, got.LastActiveAt.After(originalLastActive) || got.LastActiveAt.Equal(originalLastActive))
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/repo/mcp/... -run TestSessionsRepo -v`

Expected: FAIL - NewSessions undefined

- [ ] **Step 3: Write the implementation**

Create `internal/repo/mcp/sessions.go`:

```go
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

var ErrSessionNotFound = errors.New("mcp session not found")

type Session struct {
	ID           uuid.UUID
	ClientID     uuid.UUID
	TenantID     string
	Scopes       []string
	LastActiveAt time.Time
	CreatedAt    time.Time
	ClosedAt     *time.Time
}

type CreateSessionParams struct {
	ClientID uuid.UUID
	TenantID string
	Scopes   []string
}

type SessionsRepo struct {
	pool *pgxpool.Pool
}

func NewSessions(pool *pgxpool.Pool) *SessionsRepo {
	return ptrext.Of(SessionsRepo{pool: pool})
}

func (r *SessionsRepo) Create(ctx context.Context, p CreateSessionParams) (*Session, error) {
	const q = `
		INSERT INTO mcp_sessions (client_id, tenant_id, scopes)
		VALUES ($1, $2, $3)
		RETURNING id, client_id, tenant_id, scopes, last_active_at, created_at, closed_at
	`
	var s Session
	err := r.pool.QueryRow(ctx, q, p.ClientID, p.TenantID, p.Scopes).Scan(
		&s.ID, &s.ClientID, &s.TenantID, &s.Scopes, &s.LastActiveAt, &s.CreatedAt, &s.ClosedAt,
	)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *SessionsRepo) GetByID(ctx context.Context, id uuid.UUID) (*Session, error) {
	const q = `
		SELECT id, client_id, tenant_id, scopes, last_active_at, created_at, closed_at
		FROM mcp_sessions
		WHERE id = $1 AND closed_at IS NULL
	`
	var s Session
	err := r.pool.QueryRow(ctx, q, id).Scan(
		&s.ID, &s.ClientID, &s.TenantID, &s.Scopes, &s.LastActiveAt, &s.CreatedAt, &s.ClosedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrSessionNotFound
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *SessionsRepo) Touch(ctx context.Context, id uuid.UUID) error {
	const q = `UPDATE mcp_sessions SET last_active_at = NOW() WHERE id = $1 AND closed_at IS NULL`
	tag, err := r.pool.Exec(ctx, q, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrSessionNotFound
	}
	return nil
}

func (r *SessionsRepo) Close(ctx context.Context, id uuid.UUID) error {
	const q = `UPDATE mcp_sessions SET closed_at = NOW() WHERE id = $1 AND closed_at IS NULL`
	_, err := r.pool.Exec(ctx, q, id)
	return err
}

func (r *SessionsRepo) CloseByClient(ctx context.Context, clientID uuid.UUID) (int64, error) {
	const q = `UPDATE mcp_sessions SET closed_at = NOW() WHERE client_id = $1 AND closed_at IS NULL`
	tag, err := r.pool.Exec(ctx, q, clientID)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (r *SessionsRepo) CleanupIdle(ctx context.Context, idleThreshold time.Duration) (int64, error) {
	const q = `UPDATE mcp_sessions SET closed_at = NOW() WHERE last_active_at < $1 AND closed_at IS NULL`
	tag, err := r.pool.Exec(ctx, q, time.Now().Add(-idleThreshold))
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/repo/mcp/... -run TestSessionsRepo -v`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/repo/mcp/sessions.go internal/repo/mcp/sessions_test.go
git commit -m "feat(mcp): add MCP sessions repository (#93)"
```

---

## Phase 3: OAuth Core

### Task 3.1: Create PKCE Utilities

**Files:**
- Create: `internal/mcp/oauth/pkce.go`
- Create: `internal/mcp/oauth/pkce_test.go`

- [ ] **Step 1: Write the test**

Create `internal/mcp/oauth/pkce_test.go`:

```go
package oauth_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Phixsura/attune/internal/mcp/oauth"
)

func TestPKCE_VerifyS256(t *testing.T) {
	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	challenge := "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"

	assert.True(t, oauth.VerifyCodeChallenge(challenge, verifier, "S256"))
}

func TestPKCE_VerifyS256_Invalid(t *testing.T) {
	verifier := "wrong_verifier"
	challenge := "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"

	assert.False(t, oauth.VerifyCodeChallenge(challenge, verifier, "S256"))
}

func TestPKCE_PlainNotSupported(t *testing.T) {
	verifier := "test"
	challenge := "test"

	assert.False(t, oauth.VerifyCodeChallenge(challenge, verifier, "plain"))
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/mcp/oauth/... -run TestPKCE -v`

Expected: FAIL - package not found

- [ ] **Step 3: Write the implementation**

Create `internal/mcp/oauth/pkce.go`:

```go
// SPDX-License-Identifier: Apache-2.0

package oauth

import (
	"crypto/sha256"
	"encoding/base64"
)

// VerifyCodeChallenge verifies a PKCE code challenge against a code verifier.
// Only S256 method is supported (required by OAuth 2.1).
func VerifyCodeChallenge(challenge, verifier, method string) bool {
	if method != "S256" {
		return false
	}

	h := sha256.Sum256([]byte(verifier))
	expected := base64.RawURLEncoding.EncodeToString(h[:])

	return challenge == expected
}

// GenerateCodeChallenge generates a code challenge from a code verifier using S256.
func GenerateCodeChallenge(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/mcp/oauth/... -run TestPKCE -v`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/mcp/oauth/pkce.go internal/mcp/oauth/pkce_test.go
git commit -m "feat(mcp): add PKCE utilities (#93)"
```

---

### Task 3.2: Create JWT Token Utilities

**Files:**
- Create: `internal/mcp/oauth/jwt.go`
- Create: `internal/mcp/oauth/jwt_test.go`

- [ ] **Step 1: Write the test**

Create `internal/mcp/oauth/jwt_test.go`:

```go
package oauth_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/mcp/oauth"
)

func TestJWT_SignAndVerify(t *testing.T) {
	secret := []byte("test-secret-key-for-jwt-signing-32b")
	issuer := "https://attune.example.com/mcp/oauth"

	signer := oauth.NewJWTSigner(secret, issuer)

	claims := oauth.AccessTokenClaims{
		TenantID:  "tenant-123",
		ClientID:  uuid.New(),
		SessionID: uuid.New(),
		Scopes:    []string{"mcp:read", "mcp:write"},
	}

	token, err := signer.Sign(claims, 1*time.Hour)
	require.NoError(t, err)
	assert.NotEmpty(t, token)

	verified, err := signer.Verify(token)
	require.NoError(t, err)
	assert.Equal(t, claims.TenantID, verified.TenantID)
	assert.Equal(t, claims.ClientID, verified.ClientID)
	assert.Equal(t, claims.Scopes, verified.Scopes)
}

func TestJWT_ExpiredToken(t *testing.T) {
	secret := []byte("test-secret-key-for-jwt-signing-32b")
	issuer := "https://attune.example.com/mcp/oauth"

	signer := oauth.NewJWTSigner(secret, issuer)

	claims := oauth.AccessTokenClaims{
		TenantID:  "tenant-123",
		ClientID:  uuid.New(),
		SessionID: uuid.New(),
		Scopes:    []string{"mcp:read"},
	}

	token, err := signer.Sign(claims, -1*time.Hour) // expired
	require.NoError(t, err)

	_, err = signer.Verify(token)
	assert.Error(t, err)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/mcp/oauth/... -run TestJWT -v`

Expected: FAIL - NewJWTSigner undefined

- [ ] **Step 3: Write the implementation**

Create `internal/mcp/oauth/jwt.go`:

```go
// SPDX-License-Identifier: Apache-2.0

package oauth

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

var (
	ErrInvalidToken = errors.New("invalid or expired token")
)

type AccessTokenClaims struct {
	TenantID  string    `json:"tenant_id"`
	ClientID  uuid.UUID `json:"client_id"`
	SessionID uuid.UUID `json:"session_id"`
	Scopes    []string  `json:"scopes"`
}

type jwtClaims struct {
	jwt.RegisteredClaims
	AccessTokenClaims
}

type JWTSigner struct {
	secret []byte
	issuer string
}

func NewJWTSigner(secret []byte, issuer string) *JWTSigner {
	return &JWTSigner{secret: secret, issuer: issuer}
}

func (s *JWTSigner) Sign(claims AccessTokenClaims, ttl time.Duration) (string, error) {
	now := time.Now()
	c := jwtClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    s.issuer,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
			ID:        uuid.New().String(),
		},
		AccessTokenClaims: claims,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, c)
	return token.SignedString(s.secret)
}

func (s *JWTSigner) Verify(tokenString string) (*AccessTokenClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &jwtClaims{}, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrInvalidToken
		}
		return s.secret, nil
	})
	if err != nil {
		return nil, ErrInvalidToken
	}

	claims, ok := token.Claims.(*jwtClaims)
	if !ok || !token.Valid {
		return nil, ErrInvalidToken
	}

	if claims.Issuer != s.issuer {
		return nil, ErrInvalidToken
	}

	return &claims.AccessTokenClaims, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/mcp/oauth/... -run TestJWT -v`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/mcp/oauth/jwt.go internal/mcp/oauth/jwt_test.go
git commit -m "feat(mcp): add JWT token signing/verification (#93)"
```

---

### Task 3.3: Create OAuth Discovery Endpoint

**Files:**
- Create: `internal/mcp/oauth/discovery.go`
- Create: `internal/mcp/oauth/discovery_test.go`

- [ ] **Step 1: Write the test**

Create `internal/mcp/oauth/discovery_test.go`:

```go
package oauth_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/mcp/oauth"
)

func TestDiscoveryHandler(t *testing.T) {
	handler := oauth.NewDiscoveryHandler("https://attune.example.com")

	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var resp oauth.DiscoveryResponse
	err := json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)

	assert.Equal(t, "https://attune.example.com/mcp/v1", resp.Resource)
	assert.Contains(t, resp.AuthorizationServers, "https://attune.example.com/mcp/oauth")
	assert.Contains(t, resp.ScopesSupported, "mcp:read")
	assert.Contains(t, resp.ScopesSupported, "mcp:write")
	assert.Contains(t, resp.ScopesSupported, "mcp:ingest")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/mcp/oauth/... -run TestDiscoveryHandler -v`

Expected: FAIL - NewDiscoveryHandler undefined

- [ ] **Step 3: Write the implementation**

Create `internal/mcp/oauth/discovery.go`:

```go
// SPDX-License-Identifier: Apache-2.0

package oauth

import (
	"encoding/json"
	"net/http"
)

type DiscoveryResponse struct {
	Resource              string   `json:"resource"`
	AuthorizationServers  []string `json:"authorization_servers"`
	ScopesSupported       []string `json:"scopes_supported"`
	BearerMethodsSupported []string `json:"bearer_methods_supported"`
}

type DiscoveryHandler struct {
	baseURL string
}

func NewDiscoveryHandler(baseURL string) *DiscoveryHandler {
	return &DiscoveryHandler{baseURL: baseURL}
}

func (h *DiscoveryHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	resp := DiscoveryResponse{
		Resource:             h.baseURL + "/mcp/v1",
		AuthorizationServers: []string{h.baseURL + "/mcp/oauth"},
		ScopesSupported:      []string{"mcp:read", "mcp:write", "mcp:ingest"},
		BearerMethodsSupported: []string{"header"},
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	json.NewEncoder(w).Encode(resp)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/mcp/oauth/... -run TestDiscoveryHandler -v`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/mcp/oauth/discovery.go internal/mcp/oauth/discovery_test.go
git commit -m "feat(mcp): add OAuth discovery endpoint (#93)"
```

---

## Phase 4: MCP JSON-RPC Core

### Task 4.1: Create JSON-RPC Types

**Files:**
- Create: `internal/mcp/jsonrpc/types.go`
- Create: `internal/mcp/jsonrpc/errors.go`

- [ ] **Step 1: Write the types**

Create `internal/mcp/jsonrpc/types.go`:

```go
// SPDX-License-Identifier: Apache-2.0

package jsonrpc

import "encoding/json"

// Request represents a JSON-RPC 2.0 request.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response represents a JSON-RPC 2.0 response.
type Response struct {
	JSONRPC string `json:"jsonrpc"`
	ID      any    `json:"id,omitempty"`
	Result  any    `json:"result,omitempty"`
	Error   *Error `json:"error,omitempty"`
}

// Error represents a JSON-RPC 2.0 error.
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// NewResponse creates a success response.
func NewResponse(id any, result any) Response {
	return Response{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	}
}

// NewErrorResponse creates an error response.
func NewErrorResponse(id any, err *Error) Response {
	return Response{
		JSONRPC: "2.0",
		ID:      id,
		Error:   err,
	}
}
```

- [ ] **Step 2: Write the error codes**

Create `internal/mcp/jsonrpc/errors.go`:

```go
// SPDX-License-Identifier: Apache-2.0

package jsonrpc

// Standard JSON-RPC 2.0 error codes.
const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeInternalError  = -32603
)

// MCP-specific error codes.
const (
	CodeUnauthorized    = -32001
	CodeForbidden       = -32002
	CodeNotFound        = -32003
	CodeRateLimited     = -32004
	CodeSessionRequired = -32005
)

var (
	ErrParseError     = &Error{Code: CodeParseError, Message: "Parse error"}
	ErrInvalidRequest = &Error{Code: CodeInvalidRequest, Message: "Invalid Request"}
	ErrMethodNotFound = &Error{Code: CodeMethodNotFound, Message: "Method not found"}
	ErrInvalidParams  = &Error{Code: CodeInvalidParams, Message: "Invalid params"}
	ErrInternalError  = &Error{Code: CodeInternalError, Message: "Internal error"}
	ErrUnauthorized   = &Error{Code: CodeUnauthorized, Message: "Unauthorized"}
	ErrForbidden      = &Error{Code: CodeForbidden, Message: "Forbidden"}
)

// NewInvalidParams creates an invalid params error with details.
func NewInvalidParams(message string) *Error {
	return &Error{Code: CodeInvalidParams, Message: message}
}

// NewInternalError creates an internal error with details.
func NewInternalError(message string) *Error {
	return &Error{Code: CodeInternalError, Message: message}
}
```

- [ ] **Step 3: Commit**

```bash
git add internal/mcp/jsonrpc/types.go internal/mcp/jsonrpc/errors.go
git commit -m "feat(mcp): add JSON-RPC 2.0 types and error codes (#93)"
```

---

### Task 4.2: Create Tool Types and Registry

**Files:**
- Create: `internal/mcp/tools/types.go`
- Create: `internal/mcp/tools/registry.go`
- Create: `internal/mcp/tools/registry_test.go`

- [ ] **Step 1: Write the types**

Create `internal/mcp/tools/types.go`:

```go
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"

	"github.com/Phixsura/attune/internal/domain"
)

// Definition describes an MCP tool.
type Definition struct {
	Name          string         `json:"name"`
	Description   string         `json:"description"`
	InputSchema   map[string]any `json:"inputSchema"`
	OutputSchema  map[string]any `json:"outputSchema,omitempty"`
	RequiredScope domain.Scope   `json:"-"`
}

// Content represents a piece of tool output.
type Content struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// Result is the result of a tool execution.
type Result struct {
	Content []Content `json:"content"`
	IsError bool      `json:"isError,omitempty"`
}

// Session provides context for tool execution.
type Session struct {
	ID         string
	TenantID   string
	ClientID   string
	ClientName string
	Scopes     []domain.Scope
	ClientIP   string
	UserAgent  string
}

// Handler executes a tool.
type Handler func(ctx context.Context, sess *Session, params json.RawMessage) (*Result, error)

// Tool combines a definition with its handler.
type Tool struct {
	Definition
	Handler Handler
}

// TextResult creates a simple text result.
func TextResult(text string) *Result {
	return &Result{
		Content: []Content{{Type: "text", Text: text}},
	}
}

// JSONResult creates a JSON text result.
func JSONResult(v any) (*Result, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return &Result{
		Content: []Content{{Type: "text", Text: string(b)}},
	}, nil
}

// ErrorResult creates an error result.
func ErrorResult(message string) *Result {
	return &Result{
		Content: []Content{{Type: "text", Text: message}},
		IsError: true,
	}
}
```

- [ ] **Step 2: Write the test**

Create `internal/mcp/tools/registry_test.go`:

```go
package tools_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/mcp/tools"
)

func TestRegistry_RegisterAndList(t *testing.T) {
	reg := tools.NewRegistry()

	reg.Register(tools.Tool{
		Definition: tools.Definition{
			Name:          "test_tool",
			Description:   "A test tool",
			InputSchema:   map[string]any{"type": "object"},
			RequiredScope: domain.ScopeMCPRead,
		},
		Handler: func(ctx context.Context, sess *tools.Session, params json.RawMessage) (*tools.Result, error) {
			return tools.TextResult("ok"), nil
		},
	})

	list := reg.List()
	require.Len(t, list, 1)
	assert.Equal(t, "test_tool", list[0].Name)
}

func TestRegistry_Get(t *testing.T) {
	reg := tools.NewRegistry()

	reg.Register(tools.Tool{
		Definition: tools.Definition{
			Name:          "test_tool",
			Description:   "A test tool",
			InputSchema:   map[string]any{"type": "object"},
			RequiredScope: domain.ScopeMCPRead,
		},
		Handler: func(ctx context.Context, sess *tools.Session, params json.RawMessage) (*tools.Result, error) {
			return tools.TextResult("ok"), nil
		},
	})

	tool, ok := reg.Get("test_tool")
	require.True(t, ok)
	assert.Equal(t, "test_tool", tool.Name)

	_, ok = reg.Get("nonexistent")
	assert.False(t, ok)
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/mcp/tools/... -run TestRegistry -v`

Expected: FAIL - NewRegistry undefined

- [ ] **Step 4: Write the implementation**

Create `internal/mcp/tools/registry.go`:

```go
// SPDX-License-Identifier: Apache-2.0

package tools

import "sync"

// Registry holds all registered MCP tools.
type Registry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

// NewRegistry creates a new tool registry.
func NewRegistry() *Registry {
	return &Registry{
		tools: make(map[string]Tool),
	}
}

// Register adds a tool to the registry.
func (r *Registry) Register(tool Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[tool.Name] = tool
}

// Get returns a tool by name.
func (r *Registry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

// List returns all registered tool definitions.
func (r *Registry) List() []Definition {
	r.mu.RLock()
	defer r.mu.RUnlock()

	defs := make([]Definition, 0, len(r.tools))
	for _, t := range r.tools {
		defs = append(defs, t.Definition)
	}
	return defs
}

// ListForScopes returns tool definitions accessible with the given scopes.
func (r *Registry) ListForScopes(scopes []string) []Definition {
	r.mu.RLock()
	defer r.mu.RUnlock()

	scopeSet := make(map[string]struct{}, len(scopes))
	for _, s := range scopes {
		scopeSet[s] = struct{}{}
	}

	var defs []Definition
	for _, t := range r.tools {
		if _, ok := scopeSet[string(t.RequiredScope)]; ok {
			defs = append(defs, t.Definition)
		}
	}
	return defs
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/mcp/tools/... -run TestRegistry -v`

Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/mcp/tools/types.go internal/mcp/tools/registry.go internal/mcp/tools/registry_test.go
git commit -m "feat(mcp): add tool types and registry (#93)"
```

---

### Task 4.3: Create MCP Server Core

**Files:**
- Create: `internal/mcp/server.go`
- Create: `internal/mcp/server_test.go`

- [ ] **Step 1: Write the test**

Create `internal/mcp/server_test.go`:

```go
package mcp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/mcp"
	"github.com/Phixsura/attune/internal/mcp/jsonrpc"
	"github.com/Phixsura/attune/internal/mcp/tools"
)

func TestServer_ToolsList(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(tools.Tool{
		Definition: tools.Definition{
			Name:          "test_tool",
			Description:   "A test tool",
			InputSchema:   map[string]any{"type": "object"},
			RequiredScope: domain.ScopeMCPRead,
		},
		Handler: func(ctx context.Context, sess *tools.Session, params json.RawMessage) (*tools.Result, error) {
			return tools.TextResult("ok"), nil
		},
	})

	server := mcp.NewServer(mcp.ServerConfig{
		Registry: reg,
	})

	reqBody := jsonrpc.Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/list",
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/mcp/v1", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-token")

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp jsonrpc.Response
	err := json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Nil(t, resp.Error)
	assert.NotNil(t, resp.Result)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/mcp/... -run TestServer -v`

Expected: FAIL - mcp.NewServer undefined

- [ ] **Step 3: Write the implementation**

Create `internal/mcp/server.go`:

```go
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/mcp/jsonrpc"
	"github.com/Phixsura/attune/internal/mcp/oauth"
	"github.com/Phixsura/attune/internal/mcp/tools"
)

type ServerConfig struct {
	Registry   *tools.Registry
	JWTSigner  *oauth.JWTSigner
	AuditFunc  func(sess *tools.Session, tool string, params json.RawMessage, result *tools.Result, latency time.Duration)
}

type Server struct {
	registry  *tools.Registry
	jwtSigner *oauth.JWTSigner
	auditFunc func(sess *tools.Session, tool string, params json.RawMessage, result *tools.Result, latency time.Duration)
}

func NewServer(cfg ServerConfig) *Server {
	return &Server{
		registry:  cfg.Registry,
		jwtSigner: cfg.JWTSigner,
		auditFunc: cfg.AuditFunc,
	}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sess, err := s.authenticate(r)
	if err != nil {
		s.writeError(w, nil, jsonrpc.ErrUnauthorized)
		return
	}

	var req jsonrpc.Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, nil, jsonrpc.ErrParseError)
		return
	}

	if req.JSONRPC != "2.0" {
		s.writeError(w, req.ID, jsonrpc.ErrInvalidRequest)
		return
	}

	switch req.Method {
	case "tools/list":
		s.handleToolsList(w, sess, req)
	case "tools/call":
		s.handleToolsCall(w, sess, req)
	case "initialize":
		s.handleInitialize(w, sess, req)
	default:
		s.writeError(w, req.ID, jsonrpc.ErrMethodNotFound)
	}
}

func (s *Server) authenticate(r *http.Request) (*tools.Session, error) {
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		return nil, oauth.ErrInvalidToken
	}

	token := strings.TrimPrefix(auth, "Bearer ")

	if s.jwtSigner == nil {
		return &tools.Session{
			ID:       "test-session",
			TenantID: "test-tenant",
			Scopes:   []domain.Scope{domain.ScopeMCPRead, domain.ScopeMCPWrite, domain.ScopeMCPIngest},
		}, nil
	}

	claims, err := s.jwtSigner.Verify(token)
	if err != nil {
		return nil, err
	}

	scopes := make([]domain.Scope, len(claims.Scopes))
	for i, s := range claims.Scopes {
		scopes[i] = domain.Scope(s)
	}

	return &tools.Session{
		ID:       claims.SessionID.String(),
		TenantID: claims.TenantID,
		ClientID: claims.ClientID.String(),
		Scopes:   scopes,
	}, nil
}

func (s *Server) handleToolsList(w http.ResponseWriter, sess *tools.Session, req jsonrpc.Request) {
	defs := s.registry.List()

	result := map[string]any{
		"tools": defs,
	}

	s.writeResponse(w, req.ID, result)
}

type toolsCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

func (s *Server) handleToolsCall(w http.ResponseWriter, sess *tools.Session, req jsonrpc.Request) {
	var params toolsCallParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		s.writeError(w, req.ID, jsonrpc.NewInvalidParams("invalid params"))
		return
	}

	tool, ok := s.registry.Get(params.Name)
	if !ok {
		s.writeError(w, req.ID, jsonrpc.NewInvalidParams("unknown tool: "+params.Name))
		return
	}

	if !domain.HasScope(sess.Scopes, tool.RequiredScope) {
		s.writeError(w, req.ID, jsonrpc.ErrForbidden)
		return
	}

	start := time.Now()
	result, err := tool.Handler(r.Context(), sess, params.Arguments)
	latency := time.Since(start)

	if err != nil {
		s.writeError(w, req.ID, jsonrpc.NewInternalError(err.Error()))
		return
	}

	if s.auditFunc != nil {
		s.auditFunc(sess, params.Name, params.Arguments, result, latency)
	}

	s.writeResponse(w, req.ID, result)
}

func (s *Server) handleInitialize(w http.ResponseWriter, sess *tools.Session, req jsonrpc.Request) {
	result := map[string]any{
		"protocolVersion": "2024-11-05",
		"serverInfo": map[string]any{
			"name":    "attune",
			"version": "0.6.0",
		},
		"capabilities": map[string]any{
			"tools": map[string]any{},
		},
	}
	s.writeResponse(w, req.ID, result)
}

func (s *Server) writeResponse(w http.ResponseWriter, id any, result any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(jsonrpc.NewResponse(id, result))
}

func (s *Server) writeError(w http.ResponseWriter, id any, err *jsonrpc.Error) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(jsonrpc.NewErrorResponse(id, err))
}
```

- [ ] **Step 4: Fix compilation error (add context)**

The `handleToolsCall` uses `r.Context()` but `r` is not in scope. Fix:

```go
func (s *Server) handleToolsCall(w http.ResponseWriter, sess *tools.Session, req jsonrpc.Request) {
	// ... existing code ...

	start := time.Now()
	ctx := context.Background() // Use background context for now
	result, err := tool.Handler(ctx, sess, params.Arguments)
	// ...
}
```

Add import: `"context"`

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/mcp/... -run TestServer -v`

Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/mcp/server.go internal/mcp/server_test.go
git commit -m "feat(mcp): add MCP JSON-RPC server core (#93)"
```

---

## Phase 5: First Read Tool

### Task 5.1: Create list_feedback Tool

**Files:**
- Create: `internal/mcp/tools/list_feedback.go`
- Create: `internal/mcp/tools/list_feedback_test.go`

- [ ] **Step 1: Write the test**

Create `internal/mcp/tools/list_feedback_test.go`:

```go
package tools_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/mcp/tools"
)

type mockFeedbackService struct {
	items []domain.Feedback
}

func (m *mockFeedbackService) List(ctx context.Context, tenantID string, limit int, cursor string) ([]domain.Feedback, string, error) {
	return m.items, "", nil
}

func TestListFeedbackTool(t *testing.T) {
	svc := &mockFeedbackService{
		items: []domain.Feedback{
			{ID: 1, Content: "Test feedback 1"},
			{ID: 2, Content: "Test feedback 2"},
		},
	}

	tool := tools.NewListFeedbackTool(svc)

	sess := &tools.Session{
		TenantID: "test-tenant",
		Scopes:   []domain.Scope{domain.ScopeMCPRead},
	}

	params := json.RawMessage(`{"limit": 10}`)

	result, err := tool.Handler(context.Background(), sess, params)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, result.IsError)
	assert.Len(t, result.Content, 1)

	var output map[string]any
	err = json.Unmarshal([]byte(result.Content[0].Text), &output)
	require.NoError(t, err)

	items, ok := output["items"].([]any)
	require.True(t, ok)
	assert.Len(t, items, 2)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/mcp/tools/... -run TestListFeedbackTool -v`

Expected: FAIL - NewListFeedbackTool undefined

- [ ] **Step 3: Write the implementation**

Create `internal/mcp/tools/list_feedback.go`:

```go
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"

	"github.com/Phixsura/attune/internal/domain"
)

// FeedbackLister is the interface for listing feedback.
type FeedbackLister interface {
	List(ctx context.Context, tenantID string, limit int, cursor string) ([]domain.Feedback, string, error)
}

type listFeedbackInput struct {
	Limit  int    `json:"limit"`
	Cursor string `json:"cursor"`
}

type listFeedbackOutput struct {
	Items      []feedbackItem `json:"items"`
	NextCursor string         `json:"next_cursor,omitempty"`
}

type feedbackItem struct {
	ID        int64    `json:"id"`
	Content   string   `json:"content"`
	Source    string   `json:"source,omitempty"`
	Status    string   `json:"status,omitempty"`
	Tags      []string `json:"tags,omitempty"`
	CreatedAt string   `json:"created_at,omitempty"`
}

// NewListFeedbackTool creates the list_feedback tool.
func NewListFeedbackTool(svc FeedbackLister) Tool {
	return Tool{
		Definition: Definition{
			Name:        "list_feedback",
			Description: "List feedback with pagination. Use this to browse recent feedback in the system.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum number of items to return (default 20, max 100)",
						"default":     20,
					},
					"cursor": map[string]any{
						"type":        "string",
						"description": "Pagination cursor from previous response",
					},
				},
			},
			RequiredScope: domain.ScopeMCPRead,
		},
		Handler: func(ctx context.Context, sess *Session, params json.RawMessage) (*Result, error) {
			var input listFeedbackInput
			if err := json.Unmarshal(params, &input); err != nil {
				return ErrorResult("invalid params: " + err.Error()), nil
			}

			if input.Limit <= 0 {
				input.Limit = 20
			}
			if input.Limit > 100 {
				input.Limit = 100
			}

			items, nextCursor, err := svc.List(ctx, sess.TenantID, input.Limit, input.Cursor)
			if err != nil {
				return ErrorResult("failed to list feedback: " + err.Error()), nil
			}

			output := listFeedbackOutput{
				Items:      make([]feedbackItem, len(items)),
				NextCursor: nextCursor,
			}

			for i, fb := range items {
				output.Items[i] = feedbackItem{
					ID:        fb.ID,
					Content:   fb.Content,
					Source:    fb.Source,
					Status:    fb.Status,
					CreatedAt: fb.CreatedAt.Format("2006-01-02T15:04:05Z"),
				}
			}

			return JSONResult(output)
		},
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/mcp/tools/... -run TestListFeedbackTool -v`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/mcp/tools/list_feedback.go internal/mcp/tools/list_feedback_test.go
git commit -m "feat(mcp): add list_feedback tool (#93)"
```

---

## Remaining Tasks (Summary)

The plan continues with these additional tasks:

### Phase 5 (continued): More Read Tools
- Task 5.2: Create search_feedback tool
- Task 5.3: Create get_feedback tool
- Task 5.4: Create list_dimensions tool
- Task 5.5: Create list_tags tool
- Task 5.6: Create get_digest tool

### Phase 6: Write Tools
- Task 6.1: Create update_status tool
- Task 6.2: Create update_tags tool
- Task 6.3: Create batch_update_status tool

### Phase 7: Ingest Tool
- Task 7.1: Create record_signal tool

### Phase 8: Router Integration
- Task 8.1: Mount MCP routes in cmd/attune/router.go
- Task 8.2: Add OAuth endpoints
- Task 8.3: Add discovery endpoint at /.well-known/oauth-protected-resource

### Phase 9: Console UI
- Task 9.1: Create agents management handler
- Task 9.2: Create OAuth consent screen
- Task 9.3: Create agent activity timeline

### Phase 10: Audit Integration
- Task 10.1: Create MCP audit recorder
- Task 10.2: Wire audit to tool calls
- Task 10.3: Add MCP activity filter to Console audit page

### Phase 11: Observability
- Task 11.1: Add MCP metrics (tool_calls_total, latency_seconds)
- Task 11.2: Add metrics to Grafana dashboard
- Task 11.3: Update metric drift test

### Phase 12: Documentation
- Task 12.1: Update docs/private-deploy.md with MCP config
- Task 12.2: Create example mcp.json for Claude Desktop
- Task 12.3: Add CHANGELOG entry
- Task 12.4: Update proposal status to Implemented

---

**Plan saved to:** `docs/superpowers/plans/2026-06-20-mcp-server.md`

This is a partial plan covering the foundation and first tools. The remaining tasks follow the same TDD pattern with test → fail → implement → pass → commit cycles.

---

Plan complete and saved to `docs/superpowers/plans/2026-06-20-mcp-server.md`. Two execution options:

**1. Subagent-Driven (recommended)** - I dispatch a fresh subagent per task, review between tasks, fast iteration

**2. Inline Execution** - Execute tasks in this session using executing-plans, batch execution with checkpoints

Which approach?
