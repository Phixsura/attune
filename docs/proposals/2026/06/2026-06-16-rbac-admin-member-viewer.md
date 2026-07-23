# Role-Based Access Control: admin / member / viewer

| | |
|---|---|
| **Issue** | #38 |
| **Status** | Implemented |
| **Started** | 2026-06-16 |
| **Related** | #39 (audit log — pairs with RBAC), #40 (OIDC SSO — merged, provides role mapping), #41 (API key scopes — similar permission model), #43 (GDPR export/delete — needs permission check), #121 (OIDC SSO — merged) |

---

## Problem

Today the console treats everyone as full-rights. Once a user has a valid session, they can:

- View, edit, and delete any feedback
- Modify classification config, workflow states, tags
- Create/revoke API keys with full tenant access
- Configure LLM channels and manage secrets
- Access all settings without restriction

**Evidence from codebase:**

1. **No unified authorization layer** — `RequireSession` middleware (session.go:181-215) only validates session existence, not role.

2. **Single exception** — `requireAdmin` middleware (router.go:393-422) exists only for `/llm/*` routes, checking if user is in the `admins` table. This is a point solution, not a systematic approach.

3. **Role columns exist but unused** — Three user tables have `role` columns that are largely ignored:
   - `tenant_users.role` — CHECK `(admin|member)`, defaults to `member`, but service layer doesn't check it
   - `oidc_users.role` — Set by `MapRole()` in OIDC flow, returned in `/me`, but not enforced
   - `admins.role` — Defaults to `admin`, used by `requireAdmin` but no hierarchy

4. **Three user sources, no unified model** — Users can authenticate via:
   - `admins` table (local console login)
   - `oidc_users` table (SSO via #121)
   - `tenant_users` table (legacy, pre-SSO)
   
   Each stores role independently, making cross-source role management impossible.

**Impact:**

- **Security risk** — Any authenticated user can access sensitive settings (LLM keys, webhook secrets)
- **Multi-team friction** — Teams > 2 people need role separation (admin manages, member operates, viewer observes)
- **Enterprise blocker** — RBAC is table-stakes for enterprise adoption; v1.0 milestone requires it

---

## Industry Research Summary

Surveyed 10 top projects to inform design:

| Project | Model | Key Insight |
|---------|-------|-------------|
| **Gitea** | RBAC + Unit permissions | Fine-grained per-unit (Code/Issues/Wiki) access within teams |
| **Grafana** | Action + Scope | Clear `action:scope` pattern for API authorization |
| **Mattermost** | Layered RBAC | System → Team → Channel permission schemes with override |
| **Casbin** | PERM metamodel | Model/policy separation; too complex for our needs |
| **CASL** | Isomorphic RBAC+ABAC | **Frontend-backend unified permission definitions** |
| **Keycloak** | Full authz service | Realm/Client roles; policy composition |
| **Ory Keto** | ReBAC (Zanzibar) | Relationship-based; overkill for 3-role system |
| **GitLab** | DeclarativePolicy | **Condition + Rule separation; cacheable** |
| **Supabase** | PostgreSQL RLS | Database-layer enforcement; defense in depth |
| **Plane** | 2-layer RBAC | **Simple 3-role (admin/member/guest) at workspace level** |

**Best fit for attune:**
- **Plane's simplicity** — 3-role hierarchy is sufficient
- **CASL's isomorphism** — Shared permission semantics frontend/backend
- **GitLab's declarative policies** — Clean condition/rule separation
- **Supabase's RLS** — Optional database-layer enforcement

---

## Goals

| Category | Goal |
|----------|------|
| **Access Control** | Three-level hierarchy: `admin > member > viewer` |
| | All console handlers protected by role check |
| | Role inherited: higher roles get all lower permissions |
| **Data Model** | Unified `tenant_members` table associates users with tenant roles |
| | Support all three user sources (admin, oidc_user, tenant_user) |
| | Migration: existing users auto-promoted to admin (zero disruption) |
| **Backend** | `RequireRole(min)` middleware for route-level checks |
| | Policy classes for resource-level checks (e.g., "can delete own feedback") |
| | Role embedded in session/JWT to minimize DB queries |
| **Frontend** | `usePermissions()` hook for permission checks |
| | `<Can>` component for declarative UI guards |
| | Route guards redirect unauthorized users |
| | UI hides unavailable actions (not just disables) |
| **Observability** | Permission denials logged with user/role/action context |
| | Metrics: `attune_authz_denied_total{role, action}` |
| **Testing** | Table-driven test: every handler × every role × allow/deny matrix |
| | Integration test: OIDC user gets correct role from mapping |

---

## Non-Goals

| Scope | Rationale |
|-------|-----------|
| Fine-grained permissions (e.g., per-feedback ACL) | 3-role covers 80% of needs; defer complexity |
| Custom role creation | Enterprise feature; can add later without schema change |
| Multi-tenant role inheritance | Single-tenant focus for v1.0 |
| API key RBAC (#41) | Separate issue; shares design but different enforcement point |
| Real-time role sync from IdP | Users re-login to get updated role; acceptable for v1.0 |
| Audit log integration (#39) | Pairs with RBAC but separate implementation |

---

## Proposal

### 1. Role Definition

```go
// internal/domain/role.go

type Role string

const (
    RoleAdmin  Role = "admin"   // Full tenant management
    RoleMember Role = "member"  // Standard operations
    RoleViewer Role = "viewer"  // Read-only access
)

var roleHierarchy = map[Role]int{
    RoleViewer: 0,
    RoleMember: 1,
    RoleAdmin:  2,
}

// AtLeast checks if this role meets minimum requirement
func (r Role) AtLeast(min Role) bool {
    return roleHierarchy[r] >= roleHierarchy[min]
}

// CanManage checks if this role can manage target role
func (r Role) CanManage(target Role) bool {
    return roleHierarchy[r] > roleHierarchy[target]
}
```

### 2. Permission Matrix

#### 2.1 Console Features

| Feature | admin | member | viewer |
|---------|:-----:|:------:|:------:|
| **Feedback** | | | |
| View list/detail | ✓ | ✓ | ✓ |
| Change workflow state | ✓ | ✓ | — |
| Add/remove tags | ✓ | ✓ | — |
| Generate reply draft | ✓ | ✓ | — |
| Delete feedback | ✓ | own | — |
| Batch operations | ✓ | ✓ | — |
| **Settings** | | | |
| Classification config | ✓ | — | — |
| Workflow states | ✓ | — | — |
| Tags management | ✓ | — | — |
| Notify targets | ✓ | — | — |
| Inbound sources | ✓ | — | — |
| Guard policies | ✓ | — | — |
| **Security** | | | |
| API keys | ✓ | — | — |
| LLM channels | ✓ | — | — |
| **Usage** | | | |
| View usage stats | ✓ | ✓ | ✓ |
| View LLM costs | ✓ | ✓ | ✓ |
| **Members** | | | |
| View members | ✓ | ✓ | ✓ |
| Invite members | ✓ | — | — |
| Change roles | ✓ | — | — |
| Remove members | ✓ | — | — |

Legend: ✓ = allowed, — = denied, `own` = only own resources

#### 2.2 API Endpoints

| Endpoint | Method | Min Role |
|----------|--------|----------|
| `/console/feedback` | GET | viewer |
| `/console/feedback/{id}` | GET | viewer |
| `/console/feedback/{id}/transition` | POST | member |
| `/console/feedback/{id}/tags` | POST/DELETE | member |
| `/console/feedback/{id}` | DELETE | admin (or member for own) |
| `/console/feedback/batch/*` | POST | member |
| `/console/enrich-config/*` | * | admin |
| `/console/workflow-states/*` | * | admin |
| `/console/tags/*` | * | admin |
| `/console/notify-targets/*` | * | admin |
| `/console/inbound-sources/*` | * | admin |
| `/console/guard-policies/*` | * | admin |
| `/console/api-keys/*` | * | admin |
| `/console/llm/*` | * | admin |
| `/console/members` | GET | viewer |
| `/console/members` | POST | admin |
| `/console/members/{id}` | PATCH/DELETE | admin |
| `/console/usage` | GET | viewer |
| `/console/llm-usage` | GET | viewer |

### 3. Database Schema

```sql
-- migrations/035_create_tenant_members.sql

-- ============================================================
-- 1. Create unified membership table
-- ============================================================
CREATE TABLE tenant_members (
    id            UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     TEXT         NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    
    -- User reference (supports multiple sources)
    member_type   TEXT         NOT NULL DEFAULT 'oidc_user'
                  CHECK (member_type IN ('admin', 'oidc_user', 'tenant_user')),
    user_id       TEXT         NOT NULL,
    
    -- Three-level role
    role          TEXT         NOT NULL DEFAULT 'member'
                  CHECK (role IN ('admin', 'member', 'viewer')),
    
    -- Role source (prevents OIDC from overwriting manual assignments)
    role_source   TEXT         NOT NULL DEFAULT 'idp'
                  CHECK (role_source IN ('idp', 'manual', 'bootstrap')),
    
    -- Invitation audit trail
    invited_by    UUID         REFERENCES tenant_members(id) ON DELETE SET NULL,
    invited_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    accepted_at   TIMESTAMPTZ,  -- NULL = pending invitation
    
    -- Standard timestamps
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    
    -- Unique: one membership per user per tenant
    UNIQUE (tenant_id, member_type, user_id)
);

-- Indexes for common queries
CREATE INDEX idx_tm_tenant ON tenant_members (tenant_id);
CREATE INDEX idx_tm_user ON tenant_members (member_type, user_id);
CREATE INDEX idx_tm_role ON tenant_members (tenant_id, role);

-- Auto-update updated_at
CREATE TRIGGER trg_tenant_members_updated
    BEFORE UPDATE ON tenant_members
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();

-- ============================================================
-- 2. Migrate existing users (zero-disruption: all become admin)
-- ============================================================

-- Migrate tenant_users
INSERT INTO tenant_members (
    tenant_id, member_type, user_id, role, role_source, invited_at, accepted_at
)
SELECT 
    tenant_id, 
    'tenant_user', 
    id, 
    'admin',      -- Promote to admin
    'bootstrap',  -- Mark as migration
    created_at, 
    created_at
FROM tenant_users 
WHERE is_active = TRUE
ON CONFLICT DO NOTHING;

-- Migrate OIDC users (if any exist pre-migration)
-- NOTE: Single-tenant assumption — OIDC users associate with the sole tenant.
-- Multi-tenant deployments should have oidc_users.tenant_id by design;
-- if not, this migration skips OIDC users (they'll get membership on next login).
INSERT INTO tenant_members (
    tenant_id, member_type, user_id, role, role_source, invited_at, accepted_at
)
SELECT 
    COALESCE(
        ou.tenant_id,                        -- Use tenant_id if present
        (SELECT id FROM tenants LIMIT 1)     -- Fallback for single-tenant
    ),
    'oidc_user',
    ou.id,
    CASE WHEN ou.role = 'admin' THEN 'admin' ELSE 'member' END,
    'bootstrap',
    ou.created_at,
    ou.created_at
FROM oidc_users ou
WHERE EXISTS (SELECT 1 FROM tenants)
  AND (ou.tenant_id IS NOT NULL OR (SELECT COUNT(*) FROM tenants) = 1)
ON CONFLICT DO NOTHING;

-- ============================================================
-- 3. Update oidc_users constraint (add viewer)
-- ============================================================
ALTER TABLE oidc_users DROP CONSTRAINT IF EXISTS chk_oidc_role;
ALTER TABLE oidc_users ADD CONSTRAINT chk_oidc_role 
    CHECK (role IN ('admin', 'member', 'viewer'));

-- ============================================================
-- 4. Optional: Row-Level Security for defense in depth
-- ============================================================
-- ALTER TABLE tenant_members ENABLE ROW LEVEL SECURITY;
-- 
-- CREATE POLICY "tenant_isolation" ON tenant_members
--     FOR ALL USING (tenant_id = current_setting('app.tenant_id', true));
-- 
-- CREATE POLICY "admin_manage" ON tenant_members
--     FOR ALL USING (current_setting('app.user_role', true) = 'admin');
```

### 4. Backend Implementation

#### 4.1 Repository (`internal/repo/tenantmember/`)

```go
// internal/repo/tenantmember/tenant_member.go

type TenantMember struct {
    ID          string
    TenantID    string
    MemberType  string    // "admin" | "oidc_user" | "tenant_user"
    UserID      string
    Role        domain.Role
    RoleSource  string    // "idp" | "manual" | "bootstrap"
    InvitedBy   *string
    InvitedAt   time.Time
    AcceptedAt  *time.Time
    CreatedAt   time.Time
    UpdatedAt   time.Time
}

type Repo struct {
    pool *pgxpool.Pool
}

// GetByUser finds membership by user type and ID
func (r *Repo) GetByUser(ctx context.Context, tenantID, memberType, userID string) (*TenantMember, error)

// GetRole is a fast path for middleware (returns role string only)
func (r *Repo) GetRole(ctx context.Context, tenantID, memberType, userID string) (domain.Role, error)

// List returns all members for a tenant
func (r *Repo) List(ctx context.Context, tenantID string) ([]TenantMember, error)

// Invite creates a pending membership
func (r *Repo) Invite(ctx context.Context, tenantID, memberType, userID string, role domain.Role, invitedBy string) (*TenantMember, error)

// Accept marks invitation as accepted
func (r *Repo) Accept(ctx context.Context, id string) error

// UpdateRole changes a member's role (manual source)
// Returns ErrLastAdmin if demoting the last admin
func (r *Repo) UpdateRole(ctx context.Context, id string, newRole domain.Role, changedBy string) error

// Remove deletes a membership
// Returns ErrLastAdmin if removing the last admin
func (r *Repo) Remove(ctx context.Context, id string) error

// EnsureOIDCMember creates or updates OIDC user membership on login
func (r *Repo) EnsureOIDCMember(ctx context.Context, tenantID, userID string, role domain.Role, source string) (*TenantMember, error)

// CountAdmins returns the number of admins for a tenant (for last-admin check)
func (r *Repo) CountAdmins(ctx context.Context, tenantID string) (int, error)
```

**Last-Admin Protection (Transaction-Level):**

```go
// Remove with last-admin protection
func (r *Repo) Remove(ctx context.Context, id string) error {
    tx, err := r.pool.Begin(ctx)
    if err != nil {
        return err
    }
    defer tx.Rollback(ctx)
    
    // Get member info first
    var tenantID string
    var role domain.Role
    err = tx.QueryRow(ctx, `
        SELECT tenant_id, role FROM tenant_members WHERE id = $1
    `, id).Scan(&tenantID, &role)
    if err != nil {
        return err
    }
    
    // If removing an admin, check count
    if role == domain.RoleAdmin {
        var adminCount int
        err = tx.QueryRow(ctx, `
            SELECT COUNT(*) FROM tenant_members 
            WHERE tenant_id = $1 AND role = 'admin' AND id != $2
        `, tenantID, id).Scan(&adminCount)
        if err != nil {
            return err
        }
        if adminCount == 0 {
            return ErrLastAdmin
        }
    }
    
    // Proceed with delete
    _, err = tx.Exec(ctx, `DELETE FROM tenant_members WHERE id = $1`, id)
    if err != nil {
        return err
    }
    
    return tx.Commit(ctx)
}

var ErrLastAdmin = errors.New("cannot remove last admin")
```

#### 4.2 Middleware (`internal/handlers/console/middleware/`)

```go
// internal/handlers/console/middleware/rbac.go

type RBACMiddleware struct {
    members *tenantmember.Repo
    cache   *roleCache  // Optional: in-memory cache with TTL
}

func NewRBACMiddleware(members *tenantmember.Repo) *RBACMiddleware {
    return &RBACMiddleware{
        members: members,
        cache:   newRoleCache(5 * time.Minute),
    }
}

// RequireRole creates middleware that checks minimum role
func (m *RBACMiddleware) RequireRole(min domain.Role) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            ctx := r.Context()
            auth := session.FromContext(ctx)
            
            // Try cache first
            cacheKey := fmt.Sprintf("%s:%s:%s", auth.TenantID, auth.UserType, auth.UserID)
            role, ok := m.cache.Get(cacheKey)
            if !ok {
                // Cache miss: query DB
                var err error
                role, err = m.members.GetRole(ctx, auth.TenantID, auth.UserType, auth.UserID)
                if err != nil {
                    if errors.Is(err, tenantmember.ErrNotFound) {
                        metrics.AuthzDeniedTotal.WithLabelValues("unknown", min.String()).Inc()
                        dispatcher.Reject(ctx, w, http.StatusForbidden,
                            attunev1.ErrorCode_FORBIDDEN, "not a tenant member")
                        return
                    }
                    dispatcher.Reject(ctx, w, http.StatusInternalServerError,
                        attunev1.ErrorCode_INTERNAL, "failed to verify membership")
                    return
                }
                m.cache.Set(cacheKey, role)
            }
            
            // Check hierarchy
            if !role.AtLeast(min) {
                metrics.AuthzDeniedTotal.WithLabelValues(string(role), min.String()).Inc()
                logext.Warnf(ctx, "[rbac] denied,user:%s,role:%s,required:%s",
                    auth.UserID, role, min)
                dispatcher.Reject(ctx, w, http.StatusForbidden,
                    attunev1.ErrorCode_FORBIDDEN,
                    fmt.Sprintf("requires %s role or higher", min))
                return
            }
            
            // Attach role to context for downstream use
            ctx = WithRole(ctx, role)
            next.ServeHTTP(w, r.WithContext(ctx))
        })
    }
}

// Convenience methods
func (m *RBACMiddleware) RequireAdmin() func(http.Handler) http.Handler {
    return m.RequireRole(domain.RoleAdmin)
}

func (m *RBACMiddleware) RequireMember() func(http.Handler) http.Handler {
    return m.RequireRole(domain.RoleMember)
}

func (m *RBACMiddleware) RequireViewer() func(http.Handler) http.Handler {
    return m.RequireRole(domain.RoleViewer)
}

// InvalidateCache clears cached role for a user (call on role change)
func (m *RBACMiddleware) InvalidateCache(tenantID, memberType, userID string) {
    m.cache.Delete(fmt.Sprintf("%s:%s:%s", tenantID, memberType, userID))
}

// RequireRoleStrict bypasses cache — use for sensitive operations
// (API key creation, LLM config, member management)
func (m *RBACMiddleware) RequireRoleStrict(min domain.Role) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            ctx := r.Context()
            auth := session.FromContext(ctx)
            
            // Always query DB for sensitive operations
            role, err := m.members.GetRole(ctx, auth.TenantID, auth.UserType, auth.UserID)
            if err != nil {
                if errors.Is(err, tenantmember.ErrNotFound) {
                    metrics.AuthzDeniedTotal.WithLabelValues("unknown", min.String()).Inc()
                    dispatcher.Reject(ctx, w, http.StatusForbidden,
                        attunev1.ErrorCode_FORBIDDEN, "not a tenant member")
                    return
                }
                dispatcher.Reject(ctx, w, http.StatusInternalServerError,
                    attunev1.ErrorCode_INTERNAL, "failed to verify membership")
                return
            }
            
            if !role.AtLeast(min) {
                metrics.AuthzDeniedTotal.WithLabelValues(string(role), min.String()).Inc()
                logext.Warnf(ctx, "[rbac] strict denied,user:%s,role:%s,required:%s",
                    auth.UserID, role, min)
                dispatcher.Reject(ctx, w, http.StatusForbidden,
                    attunev1.ErrorCode_FORBIDDEN,
                    fmt.Sprintf("requires %s role or higher", min))
                return
            }
            
            ctx = WithRole(ctx, role)
            next.ServeHTTP(w, r.WithContext(ctx))
        })
    }
}

// RequireAdminStrict bypasses cache — use for API keys, LLM config, member ops
func (m *RBACMiddleware) RequireAdminStrict() func(http.Handler) http.Handler {
    return m.RequireRoleStrict(domain.RoleAdmin)
}
```

#### 4.3 Policy Classes (`internal/service/policy/`)

```go
// internal/service/policy/feedback.go

type FeedbackPolicy struct {
    role     domain.Role
    userID   string
}

func NewFeedbackPolicy(role domain.Role, userID string) *FeedbackPolicy {
    return &FeedbackPolicy{role: role, userID: userID}
}

// CanView — viewer+
func (p *FeedbackPolicy) CanView() bool {
    return p.role.AtLeast(domain.RoleViewer)
}

// CanEdit — member+ (transition, tags)
func (p *FeedbackPolicy) CanEdit() bool {
    return p.role.AtLeast(domain.RoleMember)
}

// CanDelete — admin OR member deleting own
func (p *FeedbackPolicy) CanDelete(feedback *domain.Feedback) bool {
    if p.role == domain.RoleAdmin {
        return true
    }
    if p.role == domain.RoleMember && feedback.CreatedByUserID == p.userID {
        return true
    }
    return false
}

// CanBatchDelete — admin only
func (p *FeedbackPolicy) CanBatchDelete() bool {
    return p.role == domain.RoleAdmin
}
```

```go
// internal/service/policy/member.go

type MemberPolicy struct {
    actorRole domain.Role
}

func NewMemberPolicy(actorRole domain.Role) *MemberPolicy {
    return &MemberPolicy{actorRole: actorRole}
}

// CanInvite — admin only
func (p *MemberPolicy) CanInvite() bool {
    return p.actorRole == domain.RoleAdmin
}

// CanChangeRole — admin can demote or promote within non-admin roles
// Promoting TO admin requires CanPromoteToAdmin + explicit confirmation flow
func (p *MemberPolicy) CanChangeRole(currentRole, newRole domain.Role) bool {
    if p.actorRole != domain.RoleAdmin {
        return false
    }
    // Cannot manage equal or higher roles
    if !p.actorRole.CanManage(currentRole) {
        return false
    }
    // Promoting to admin requires separate flow (CanPromoteToAdmin)
    if newRole == domain.RoleAdmin {
        return false
    }
    return true
}

// CanPromoteToAdmin — explicit admin promotion (separate action with confirmation)
func (p *MemberPolicy) CanPromoteToAdmin() bool {
    return p.actorRole == domain.RoleAdmin
}

// CanRemove — admin can remove non-admins; cannot remove self
// Note: Last-admin protection enforced at repo layer via transaction
func (p *MemberPolicy) CanRemove(targetID, actorID string, targetRole domain.Role) bool {
    if p.actorRole != domain.RoleAdmin {
        return false
    }
    if targetID == actorID {
        return false  // Cannot remove self
    }
    return p.actorRole.CanManage(targetRole)
}
```

#### 4.4 Router Integration

```go
// internal/handlers/console/router.go

func (r *Router) buildRoutes(mux chi.Router) {
    rbac := middleware.NewRBACMiddleware(r.members)
    
    // Store rbac for cache invalidation
    r.rbac = rbac
    
    // ──────────────────────────────────────────────
    // Public (session only, no role check)
    // ──────────────────────────────────────────────
    mux.Get("/me", r.me.Me)
    mux.Post("/logout", r.session.Logout)
    
    // ──────────────────────────────────────────────
    // Viewer+ routes
    // ──────────────────────────────────────────────
    mux.Group(func(v chi.Router) {
        v.Use(rbac.RequireViewer())
        
        // Feedback (read)
        v.Get("/feedback", r.feedback.List)
        v.Get("/feedback/{id}", r.feedback.Get)
        v.Get("/feedback/stats", r.feedback.Stats)
        
        // Usage (read)
        v.Get("/usage", r.usage.Get)
        v.Get("/llm-usage", r.llmUsage.List)
        
        // Members (list only)
        v.Get("/members", r.members.List)
        
        // Clusters (read)
        v.Get("/clusters", r.clusters.List)
    })
    
    // ──────────────────────────────────────────────
    // Member+ routes
    // ──────────────────────────────────────────────
    mux.Group(func(m chi.Router) {
        m.Use(rbac.RequireMember())
        
        // Feedback (write)
        m.Post("/feedback/{id}/transition", r.feedback.Transition)
        m.Post("/feedback/{id}/tags", r.feedback.AddTag)
        m.Delete("/feedback/{id}/tags/{tagId}", r.feedback.RemoveTag)
        m.Post("/feedback/{id}/reply-draft/regenerate", r.replyDraft.Regenerate)
        
        // Batch operations
        m.Post("/feedback/batch/tags", r.feedbackBatch.BatchTags)
        m.Post("/feedback/batch/transition", r.feedbackBatch.BatchTransition)
    })
    
    // ──────────────────────────────────────────────
    // Admin routes (cached role check OK)
    // ──────────────────────────────────────────────
    mux.Group(func(a chi.Router) {
        a.Use(rbac.RequireAdmin())
        
        // Settings (non-sensitive)
        a.Route("/enrich-config", r.mountEnrichConfig)
        a.Route("/workflow-states", r.mountWorkflowStates)
        a.Route("/tags", r.mountTags)
        a.Route("/notify-targets", r.mountNotifyTargets)
        a.Route("/inbound-sources", r.mountInboundSources)
        a.Route("/guard-policies", r.mountGuardPolicies)
        a.Route("/digest-subscriptions", r.mountDigestSubscriptions)
        
        // Dangerous operations
        a.Delete("/feedback/{id}", r.feedback.Delete)
        a.Post("/feedback/batch/delete", r.feedbackBatch.BatchDelete)
    })
    
    // ──────────────────────────────────────────────
    // Sensitive admin routes (bypass cache, always check DB)
    // ──────────────────────────────────────────────
    mux.Group(func(s chi.Router) {
        s.Use(rbac.RequireAdminStrict())
        
        // Security — API keys & LLM secrets
        s.Route("/api-keys", r.mountAPIKeys)
        s.Route("/llm", r.mountLLMConfig)
        
        // Members — role changes affect authorization
        s.Post("/members", r.members.Invite)
        s.Patch("/members/{id}", r.members.UpdateRole)
        s.Delete("/members/{id}", r.members.Remove)
    })
}
```

#### 4.5 OIDC Integration Update

```go
// internal/handlers/console/oidc/handler.go

func (h *Handler) Callback(w http.ResponseWriter, r *http.Request) {
    // ... existing code ...
    
    // Map role from IdP groups
    role := h.svc.MapRole(claims.Groups)
    
    // Find or create OIDC user
    user, err := h.svc.FindOrCreateUser(ctx, claims, role)
    
    // NEW: Ensure tenant membership
    _, err = h.members.EnsureOIDCMember(ctx, tenantID, user.ID, domain.Role(role), "idp")
    if err != nil {
        // Log but don't fail — user can still login, just won't have membership
        logext.Warnf(ctx, "[oidc] failed to ensure membership,user:%s,err:%v", user.ID, err)
    }
    
    // Issue session (unchanged)
    h.signer.IssueSessionCookieWithType(w, tenantID, user.ID, "oidc")
}
```

Implementation note, 2026-07-23: local production-profile OIDC validation found
this membership sync missing in the shipped callback path. The implementation
now injects an OIDC membership store into `oidcauth`, syncs the IdP user into
`tenant_members` before issuing an OIDC session, and fails closed if the
membership write cannot be completed. That keeps `/me`, the session cookie, and
tenant-scoped RBAC aligned for OIDC users. Tenant membership reads also
normalize the historical session user type `oidc` to the canonical membership
type `oidc_user`, so existing OIDC cookies resolve the same row that login syncs.

```go
// internal/repo/tenantmember/oidc.go

// EnsureOIDCMember creates or updates membership on OIDC login
func (r *Repo) EnsureOIDCMember(ctx context.Context, tenantID, userID string, role domain.Role, source string) (*TenantMember, error) {
    const q = `
        INSERT INTO tenant_members (
            tenant_id, member_type, user_id, role, role_source, invited_at, accepted_at
        ) VALUES ($1, 'oidc_user', $2, $3, $4, NOW(), NOW())
        ON CONFLICT (tenant_id, member_type, user_id) DO UPDATE SET
            -- Only update role if source is 'idp' (not manual)
            role = CASE 
                WHEN tenant_members.role_source = 'idp' THEN EXCLUDED.role
                ELSE tenant_members.role
            END,
            updated_at = NOW()
        RETURNING id, role, role_source, created_at, updated_at
    `
    // ... scan and return ...
}
```

### 5. Frontend Implementation

#### 5.1 Permission Hook

```typescript
// console/src/hooks/usePermissions.ts

import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { meQuery } from '@/features/session/api/get-me'

export type Role = 'admin' | 'member' | 'viewer'

const ROLE_HIERARCHY: Record<Role, number> = {
  viewer: 0,
  member: 1,
  admin: 2,
}

export interface Permissions {
  role: Role
  userId: string | undefined
  
  // Role checks
  isAdmin: boolean
  isMember: boolean
  isViewer: boolean
  
  // Permission checks
  canView: () => boolean      // viewer+
  canEdit: () => boolean      // member+
  canManage: () => boolean    // admin only
  canDelete: (ownerId?: string) => boolean  // admin OR member+own
}

export function usePermissions(): Permissions {
  const { data: me } = useQuery(meQuery())
  
  const role = (me?.user?.role as Role) ?? 'viewer'
  const userId = me?.user?.openId
  
  return useMemo<Permissions>(() => ({
    role,
    userId,
    
    isAdmin: role === 'admin',
    isMember: role === 'member',
    isViewer: role === 'viewer',
    
    canView: () => true,
    canEdit: () => ROLE_HIERARCHY[role] >= ROLE_HIERARCHY.member,
    canManage: () => role === 'admin',
    canDelete: (ownerId?: string) => {
      if (role === 'admin') return true
      if (role === 'member' && ownerId && ownerId === userId) return true
      return false
    },
  }), [role, userId])
}
```

#### 5.2 Can Component

```tsx
// console/src/components/auth/Can.tsx

import { ReactNode } from 'react'
import { usePermissions } from '@/hooks/usePermissions'

type Action = 'view' | 'edit' | 'manage' | 'delete'

interface CanProps {
  action: Action
  ownerId?: string
  children: ReactNode
  fallback?: ReactNode
}

export function Can({ action, ownerId, children, fallback = null }: CanProps) {
  const perms = usePermissions()
  
  const allowed = (() => {
    switch (action) {
      case 'view': return perms.canView()
      case 'edit': return perms.canEdit()
      case 'manage': return perms.canManage()
      case 'delete': return perms.canDelete(ownerId)
    }
  })()
  
  return <>{allowed ? children : fallback}</>
}
```

#### 5.3 Route Guards

```typescript
// console/src/routes/_authed.settings.tsx

import { createFileRoute, redirect } from '@tanstack/react-router'
import { meQuery } from '@/features/session/api/get-me'

export const Route = createFileRoute('/_authed/settings')({
  beforeLoad: async ({ context }) => {
    const me = await context.queryClient.ensureQueryData(meQuery())
    
    if (me.user?.role !== 'admin') {
      throw redirect({
        to: '/',
        search: { error: 'permission_denied', required: 'admin' },
      })
    }
  },
  component: SettingsPage,
})
```

```typescript
// console/src/routes/_authed.llm-config.tsx

export const Route = createFileRoute('/_authed/llm-config')({
  beforeLoad: async ({ context }) => {
    const me = await context.queryClient.ensureQueryData(meQuery())
    
    if (me.user?.role !== 'admin') {
      throw redirect({
        to: '/',
        search: { error: 'permission_denied', required: 'admin' },
      })
    }
  },
  component: LLMConfigPage,
})
```

#### 5.4 Navigation Adaptation

```tsx
// console/src/features/session/components/topbar.tsx

export function TopBar({ me }: TopBarProps) {
  const { canManage } = usePermissions()
  const { t } = useTranslation()
  
  return (
    <header className="border-b bg-background">
      <div className="flex items-center h-14 px-4">
        <Logo />
        
        <nav className="ml-6 flex items-center gap-4 text-sm">
          {/* All roles */}
          <NavLink to="/feedback">{t('nav.feedback')}</NavLink>
          <NavLink to="/usage">{t('nav.usage')}</NavLink>
          <NavLink to="/llm-usage">{t('nav.llm_usage')}</NavLink>
          
          {/* Admin only */}
          {canManage() && (
            <>
              <NavLink to="/llm-config">{t('nav.llm_config')}</NavLink>
              <NavLink to="/settings">{t('nav.settings')}</NavLink>
            </>
          )}
        </nav>
        
        <div className="ml-auto flex items-center gap-4">
          <RoleBadge role={me.user?.role} />
          <UserMenu me={me} />
        </div>
      </div>
    </header>
  )
}

function RoleBadge({ role }: { role?: string }) {
  const { t } = useTranslation()
  
  const styles: Record<string, string> = {
    admin: 'bg-red-500/10 text-red-600 border-red-200',
    member: 'bg-blue-500/10 text-blue-600 border-blue-200',
    viewer: 'bg-gray-500/10 text-gray-600 border-gray-200',
  }
  
  return (
    <span className={`px-2 py-0.5 rounded border text-xs font-medium ${styles[role ?? 'viewer']}`}>
      {t(`role.${role ?? 'viewer'}`)}
    </span>
  )
}
```

#### 5.5 Feature-Level Guards

```tsx
// console/src/routes/_authed.feedback.tsx

function FeedbackActions({ feedback }: { feedback: Feedback }) {
  const { canEdit, canDelete, canManage } = usePermissions()
  const { t } = useTranslation()
  
  return (
    <div className="flex gap-2">
      {/* member+ */}
      <Can action="edit">
        <Button variant="outline" size="sm" onClick={openTransition}>
          {t('feedback.transition')}
        </Button>
      </Can>
      
      {/* admin OR member+own */}
      <Can action="delete" ownerId={feedback.createdByUserId}>
        <Button variant="destructive" size="sm" onClick={confirmDelete}>
          {t('feedback.delete')}
        </Button>
      </Can>
    </div>
  )
}

function BatchActions({ selected }: { selected: Set<string> }) {
  const { canEdit, canManage } = usePermissions()
  
  if (selected.size === 0) return null
  
  return (
    <div className="flex gap-2 p-2 bg-muted rounded">
      <span className="text-sm text-muted-foreground">
        {selected.size} selected
      </span>
      
      {canEdit() && (
        <>
          <Button size="sm" variant="outline" onClick={batchTransition}>
            Batch Transition
          </Button>
          <Button size="sm" variant="outline" onClick={batchTag}>
            Batch Tag
          </Button>
        </>
      )}
      
      {canManage() && (
        <Button size="sm" variant="destructive" onClick={batchDelete}>
          Batch Delete
        </Button>
      )}
    </div>
  )
}
```

### 6. Metrics & Observability

```go
// internal/infra/metrics/authz.go

var (
    AuthzDeniedTotal = promauto.NewCounterVec(
        prometheus.CounterOpts{
            Name: "attune_authz_denied_total",
            Help: "Authorization denials by role and required permission",
        },
        []string{"role", "required"},
    )
    
    AuthzCheckDuration = promauto.NewHistogramVec(
        prometheus.HistogramOpts{
            Name:    "attune_authz_check_duration_seconds",
            Help:    "Time spent checking authorization",
            Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1},
        },
        []string{"cache_hit"},
    )
    
    MemberRoleChanges = promauto.NewCounterVec(
        prometheus.CounterOpts{
            Name: "attune_member_role_changes_total",
            Help: "Role changes by direction",
        },
        []string{"from_role", "to_role"},
    )
)
```

### 7. Testing Strategy

#### 7.1 Handler × Role Matrix Test

```go
// internal/handlers/console/router_rbac_test.go

func TestRBACMatrix(t *testing.T) {
    cases := []struct {
        endpoint string
        method   string
        minRole  domain.Role
        testData map[domain.Role]int // role → expected status
    }{
        // Viewer+ endpoints
        {"/feedback", "GET", domain.RoleViewer, map[domain.Role]int{
            domain.RoleAdmin:  200,
            domain.RoleMember: 200,
            domain.RoleViewer: 200,
        }},
        
        // Member+ endpoints
        {"/feedback/123/transition", "POST", domain.RoleMember, map[domain.Role]int{
            domain.RoleAdmin:  200,
            domain.RoleMember: 200,
            domain.RoleViewer: 403,
        }},
        
        // Admin endpoints
        {"/settings/tags", "POST", domain.RoleAdmin, map[domain.Role]int{
            domain.RoleAdmin:  200,
            domain.RoleMember: 403,
            domain.RoleViewer: 403,
        }},
        
        // ... all endpoints
    }
    
    for _, tc := range cases {
        for role, expectedStatus := range tc.testData {
            t.Run(fmt.Sprintf("%s %s as %s", tc.method, tc.endpoint, role), func(t *testing.T) {
                // Setup test user with role
                // Make request
                // Assert status code
            })
        }
    }
}
```

#### 7.2 OIDC Role Mapping Test

```go
// internal/service/oidcauth/role_test.go

func TestMapRole(t *testing.T) {
    svc := NewService(OIDCConfig{
        RoleMapping: []RoleMappingEntry{
            {Role: "admin", Groups: []string{"attune-admins"}},
            {Role: "member", Groups: []string{"attune-users"}},
        },
    })
    
    cases := []struct {
        groups   []string
        expected string
    }{
        {[]string{"attune-admins"}, "admin"},
        {[]string{"attune-users"}, "member"},
        {[]string{"attune-admins", "attune-users"}, "admin"}, // First match wins
        {[]string{"other-group"}, "member"},                   // Default
        {[]string{}, "member"},                                 // Empty → default
    }
    
    for _, tc := range cases {
        got := svc.MapRole(tc.groups)
        assert.Equal(t, tc.expected, got)
    }
}
```

#### 7.3 Frontend Permission Test

```typescript
// console/src/hooks/usePermissions.test.ts

describe('usePermissions', () => {
  it('admin can do everything', () => {
    mockMe({ role: 'admin', openId: 'user-1' })
    const { result } = renderHook(() => usePermissions())
    
    expect(result.current.canView()).toBe(true)
    expect(result.current.canEdit()).toBe(true)
    expect(result.current.canManage()).toBe(true)
    expect(result.current.canDelete('other-user')).toBe(true)
  })
  
  it('member can edit but not manage', () => {
    mockMe({ role: 'member', openId: 'user-1' })
    const { result } = renderHook(() => usePermissions())
    
    expect(result.current.canView()).toBe(true)
    expect(result.current.canEdit()).toBe(true)
    expect(result.current.canManage()).toBe(false)
    expect(result.current.canDelete('user-1')).toBe(true)   // Own
    expect(result.current.canDelete('other-user')).toBe(false)
  })
  
  it('viewer is read-only', () => {
    mockMe({ role: 'viewer', openId: 'user-1' })
    const { result } = renderHook(() => usePermissions())
    
    expect(result.current.canView()).toBe(true)
    expect(result.current.canEdit()).toBe(false)
    expect(result.current.canManage()).toBe(false)
    expect(result.current.canDelete('user-1')).toBe(false)
  })
})
```

---

## Alternatives Considered

### Alternative 1: Per-Resource ACL

**Description:** Fine-grained access control where each feedback item has an ACL.

**Pros:**
- Maximum flexibility
- Can share specific items with specific users

**Cons:**
- Complex schema (junction table per resource type)
- Performance impact on list queries
- UI complexity for managing ACLs

**Decision:** Rejected — overkill for current needs; 3-role covers 80% of use cases.

### Alternative 2: Casbin Integration

**Description:** Use Casbin library for policy engine.

**Pros:**
- Battle-tested policy engine
- Supports multiple models (RBAC, ABAC, ReBAC)
- External policy files

**Cons:**
- Additional dependency
- Learning curve for PERM model
- Config-file-driven adds deployment complexity

**Decision:** Rejected — simple 3-role doesn't justify the complexity.

### Alternative 3: PostgreSQL RLS Only

**Description:** Implement all authorization at database layer using Row-Level Security.

**Pros:**
- Impossible to bypass at application layer
- Works for direct DB queries (debugging, admin)
- Defense in depth

**Cons:**
- Complex policy maintenance
- Performance tuning required
- Can't enforce action-based permissions (only row access)

**Decision:** Partial adoption — RLS as optional defense in depth, not primary mechanism.

### Alternative 4: JWT-Based Authorization

**Description:** Embed all permissions in JWT, check without DB query.

**Pros:**
- Zero DB queries for authorization
- Stateless scaling

**Cons:**
- Can't revoke permissions instantly (token expiry)
- Token bloat with many permissions
- Complex token refresh logic

**Decision:** Partial adoption — embed role in session, but verify against DB on sensitive operations.

---

## Risks and Mitigations

| Risk | Severity | Probability | Mitigation |
|------|----------|-------------|------------|
| **Existing users locked out** | High | Low | Migration promotes all existing users to admin |
| **OIDC role override** | Medium | Medium | `role_source` field distinguishes `idp` vs `manual` |
| **Missed endpoint protection** | High | Medium | Table-driven test covers all endpoints; CI fails on gaps |
| **Cache inconsistency** | Medium | Low | Cache TTL 5min; explicit invalidation on role change; **sensitive ops bypass cache** |
| **Performance regression** | Low | Low | Cache + indexed queries; benchmark in PR |
| **Frontend bypass** | Low | Low | Backend enforces all permissions; frontend is UX only |
| **Last admin removal** | High | Low | **Repo-layer transaction check** prevents removing last admin; policy prevents self-removal |
| **Concurrent admin demotion** | Medium | Low | Transaction-level check counts admins atomically before any role change |
| **Admin lockout recovery** | Medium | Low | Document emergency bootstrap via `attune admin create` CLI |

---

## Implementation Plan

### Phase 1: Foundation (1 day)

**Database:**
- [ ] Create `035_create_tenant_members.sql` migration
- [ ] Migrate existing users to admin role
- [ ] Add viewer to oidc_users constraint

**Repository:**
- [ ] Create `internal/repo/tenantmember/` package
- [ ] Implement CRUD operations
- [ ] Add `GetRole()` fast path

### Phase 2: Backend Authorization (1.5 days)

**Middleware:**
- [ ] Create `RequireRole` middleware
- [ ] Add role caching with TTL
- [ ] Add cache invalidation on role change

**Policy:**
- [ ] Create `FeedbackPolicy` class
- [ ] Create `MemberPolicy` class

**Router:**
- [ ] Annotate all routes with role requirements
- [ ] Refactor existing `requireAdmin` to use new middleware

**OIDC:**
- [ ] Update callback to ensure tenant membership
- [ ] Handle `role_source` for idp vs manual

### Phase 3: Frontend (1 day)

**Hooks & Components:**
- [ ] Create `usePermissions` hook
- [ ] Create `<Can>` component
- [ ] Create `RoleBadge` component

**Route Guards:**
- [ ] Add `beforeLoad` guards to `/settings`
- [ ] Add `beforeLoad` guards to `/llm-config`

**UI Adaptation:**
- [ ] Update `TopBar` navigation
- [ ] Update feedback actions
- [ ] Update batch operations
- [ ] Update member list (Phase 4 UI)

### Phase 4: Member Management UI (1 day)

**Backend API:**
- [ ] `GET /console/members` — list members
- [ ] `POST /console/members` — invite member
- [ ] `PATCH /console/members/{id}` — change role
- [ ] `DELETE /console/members/{id}` — remove member

**Frontend:**
- [ ] Add "Members" tab to settings
- [ ] Create invite dialog
- [ ] Create role change dropdown
- [ ] Create remove confirmation

### Phase 5: Testing & Documentation (0.5 day)

**Testing:**
- [ ] Add handler × role matrix test
- [ ] Add OIDC role mapping test
- [ ] Add frontend permission tests

**Documentation:**
- [ ] Update CHANGELOG.md (Added + Security)
- [ ] Update CLAUDE.md if needed
- [ ] Document role mapping config in deploy docs

---

## Verification

### Automated

- [ ] `go test ./internal/handlers/console/...` — role matrix passes
- [ ] `pnpm test` — permission hook tests pass
- [ ] `golangci-lint run` — no new findings
- [ ] CI pipeline green

### Manual

- [ ] Create OIDC user with "member" mapping → verify cannot access `/settings`
- [ ] Change user role in DB → verify cache invalidates within 5 min
- [ ] Viewer user cannot see action buttons
- [ ] Member user can transition but not delete (unless own)
- [ ] Admin user can do everything
- [ ] Invite new member via UI → verify email/notification (if implemented)

### Metrics

After deployment, verify:
- `attune_authz_denied_total` incrementing on 403s
- `attune_authz_check_duration_seconds` p99 < 10ms (cached), < 50ms (uncached)
- `attune_member_role_changes_total` tracks role modifications

---

## References

### Industry Research

| Project | Documentation |
|---------|---------------|
| Gitea | [Permissions](https://docs.gitea.com/usage/permissions), [Organization](https://pkg.go.dev/code.gitea.io/gitea/models/organization) |
| Grafana | [RBAC](https://grafana.com/docs/grafana/latest/administration/roles-and-permissions/access-control/), [Custom Roles](https://grafana.com/docs/grafana/latest/administration/roles-and-permissions/access-control/custom-role-actions-scopes/) |
| Mattermost | [Advanced Permissions](https://docs.mattermost.com/administration-guide/onboard/advanced-permissions.html) |
| Casbin | [Documentation](https://casbin.apache.org/), [Model Storage](https://casbin.apache.org/docs/model-storage/) |
| CASL | [Official](https://casl.js.org/), [React Integration](https://casl.js.org/v4/en/package/casl-react/) |
| Keycloak | [Authorization Services](https://www.keycloak.org/docs/latest/authorization_services/index.html) |
| Ory Keto | [GitHub](https://github.com/ory/keto), [OPL](https://www.ory.com/docs/keto/modeling/create-permission-model) |
| GitLab | [DeclarativePolicy](https://docs.gitlab.com/ee/development/policies.html) |
| Supabase | [RLS](https://supabase.com/docs/guides/database/postgres/row-level-security) |
| Plane | [Member Roles](https://docs.plane.so/workspaces-and-users/roles) |

### Internal References

- Issue #38: Role-based access control
- Issue #39: Audit log (pairs with RBAC)
- Issue #40: OIDC SSO (merged as #121)
- Issue #41: API key scopes (similar model)
- `internal/handlers/console/router.go` — current route structure
- `internal/handlers/console/internal/session/session.go` — session implementation
- `internal/service/oidcauth/oidcauth.go` — OIDC role mapping

---

## Appendix: Configuration Examples

### OIDC Role Mapping

```yaml
# config.yaml
console:
  oidc:
    enabled: true
    issuer_url: https://auth.example.com
    client_id: attune
    client_secret: ${OIDC_CLIENT_SECRET}
    redirect_uri: https://attune.example.com/fb/v1/console/auth/oidc/callback
    
    # Groups claim configuration
    groups_claim: groups
    
    # Role mapping (evaluated in order; first match wins)
    role_mapping:
      - role: admin
        groups:
          - attune-admins
          - devops-leads
      
      - role: member
        groups:
          - attune-users
          - product-team
      
      # Unmapped users default to 'viewer' (implicit)
```

### First Admin Bootstrap

```bash
# When deploying without OIDC, bootstrap local admin:
attune admin create --email admin@example.com --password-stdin

# This creates:
# 1. Row in `admins` table
# 2. Row in `tenant_members` with role='admin', role_source='bootstrap'
```

---

## Appendix: Error Messages

| Code | HTTP | Message | When |
|------|------|---------|------|
| `FORBIDDEN` | 403 | "not a tenant member" | User has session but no membership |
| `FORBIDDEN` | 403 | "requires admin role or higher" | Member/Viewer accessing admin route |
| `FORBIDDEN` | 403 | "requires member role or higher" | Viewer accessing member route |
| `FORBIDDEN` | 403 | "cannot modify own role" | Admin trying to change self |
| `FORBIDDEN` | 403 | "cannot remove last admin" | Trying to remove sole admin |
| `FORBIDDEN` | 403 | "cannot assign higher role" | Member trying to promote to admin |
