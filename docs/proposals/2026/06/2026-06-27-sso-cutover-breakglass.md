# SSO Cutover and Break-Glass Recovery Controls

| Field | Value |
|-------|-------|
| Issue | [#158](https://github.com/Phixsura/attune/issues/158) |
| Status | Implemented |
| Started | 2026-06-27 |
| Related | #66 (local admin auth), OIDC SSO implementation |

---

## Problem

attune supports both local admin login and OIDC SSO, but production customers need:

1. **Safe SSO cutover** — A way to disable bootstrap/local login in production without risking admin lockout.
2. **Break-glass recovery** — An emergency access path when SSO is unavailable (IdP outage, misconfiguration, accidental group removal).
3. **Preflight validation** — Automated checks that prevent operators from cutting over to SSO-only mode without a working recovery path.

**Current state gaps:**

| Capability | Status |
|------------|--------|
| `oidc_only: true` config | ✅ Hides local login form |
| Runtime SSO-only toggle | ❌ Requires config change + restart |
| Break-glass token | ❌ Not implemented |
| Preflight admin continuity | ❌ Only checks OIDC reachability |
| Cutover audit trail | ❌ No dedicated actions |

**Industry context:** Deep research across 10 projects (Vault, Entra ID, Okta, Authentik, Ory Kratos, Zitadel) reveals that platforms without declarative break-glass mechanisms create severe operational risk. Zitadel's community describes their JWT-based recovery as "the worst recovery experience" — a cautionary example attune must avoid.

---

## Goals

1. Admins can switch to SSO-only mode at runtime via Console/CLI without restart.
2. Admins cannot lock themselves out — preflight checks block cutover if no recovery path exists.
3. Break-glass tokens are one-time, time-limited, and fully audited.
4. Production mode enforces SSO-only operation while maintaining emergency access.
5. All cutover and break-glass events are captured in the audit log.

## Non-goals

- Multi-IdP federation (single OIDC provider is sufficient for v1.0).
- Shamir secret sharing (overkill for attune's threat model).
- Hardware security key enforcement for break-glass (nice-to-have post-v1.0).
- Automatic break-glass token rotation (manual issuance is safer).

---

## Proposal

### 1. Auth Mode State Machine

Introduce a runtime-configurable auth mode stored in the database:

```
┌─────────────┐     cutover (preflight pass)     ┌─────────────┐
│   hybrid    │ ─────────────────────────────────▶│  sso_only   │
│ (default)   │                                   │             │
└─────────────┘◀───────────────────────────────── └─────────────┘
                    fallback / break-glass
```

| Mode | Local Login | OIDC Login | Break-glass |
|------|-------------|------------|-------------|
| `hybrid` | ✅ | ✅ (if configured) | N/A |
| `sso_only` | ❌ | ✅ | ✅ (if token valid) |

**Storage:** New `system_settings` table row `auth.mode` (enum: `hybrid`, `sso_only`).

**Transition rules:**
- `hybrid → sso_only`: Requires all preflight checks to pass.
- `sso_only → hybrid`: Requires admin role + audit log entry (fallback action).

### 2. Break-Glass Token Design

Based on Authentik + Ory Kratos patterns with attune improvements:

| Property | Value | Rationale |
|----------|-------|-----------|
| **TTL** | Configurable, default 30 minutes | Authentik's 10 min too short for incident response |
| **One-time** | Yes | NIST FAL replay protection requirement |
| **Format** | `bg_<random-32-bytes-base64url>` | Distinguishable prefix for log grep |
| **Storage** | `break_glass_tokens` table, bcrypt-hashed | Never store plaintext |
| **Display** | Show once at generation, never again | Security standard |
| **Scope** | Tenant-scoped, targets specific admin email | Principle of least privilege |

**Schema:**

```sql
CREATE TABLE break_glass_tokens (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id TEXT NOT NULL REFERENCES tenants(id),
    admin_email TEXT NOT NULL,
    token_hash TEXT NOT NULL,           -- bcrypt hash
    expires_at TIMESTAMPTZ NOT NULL,
    used_at TIMESTAMPTZ,                -- NULL until consumed
    issued_by TEXT NOT NULL,            -- admin who issued
    issued_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    revoked_at TIMESTAMPTZ,             -- explicit revocation
    CONSTRAINT chk_not_both CHECK (used_at IS NULL OR revoked_at IS NULL)
);
CREATE INDEX idx_breakglass_lookup ON break_glass_tokens(tenant_id, admin_email, expires_at)
    WHERE used_at IS NULL AND revoked_at IS NULL;
```

**CLI:**

```bash
# Issue a break-glass token
attune auth breakglass issue --admin admin@example.com --ttl 30m
# Output:
# Break-glass token issued for admin@example.com
# Expires: 2026-06-27T15:30:00Z
# Token: bg_Abc123...XyZ (SAVE THIS - shown only once)
# Login URL: https://attune.example.com/console/breakglass?token=bg_Abc123...XyZ

# List active tokens (shows metadata, never the token itself)
attune auth breakglass list

# Revoke a token
attune auth breakglass revoke --id <uuid>
```

**Console UI:**
- Settings → Security → Break-Glass Tokens
- Issue / List / Revoke actions
- Token shown in modal once, with copy button and warning

### 3. Preflight Checks for SSO Cutover

New checks in `internal/preflight/checks/` that MUST all pass before `hybrid → sso_only`:

| Check | Pass Condition | Remediation |
|-------|----------------|-------------|
| `sso:oidc_enabled` | `oidc.enabled: true` | Enable OIDC in config |
| `sso:issuer_reachable` | Discovery endpoint returns 200 | Fix network/firewall |
| `sso:redirect_uri_match` | `redirect_uri` hostname matches `console.base_url` | Fix config mismatch |
| `sso:admin_in_groups` | ≥1 current admin's email is in `allowed_groups` (if configured) | Add admin to IdP group |
| `sso:breakglass_ready` | ≥1 unexpired break-glass token exists OR explicit skip flag | Issue a token first |

**API:**

```
POST /fb/v1/console/auth/sso-cutover
{
  "skip_breakglass_check": false  // requires explicit opt-in to skip
}

Response (preflight fail):
{
  "code": "PRECONDITION_FAILED",
  "message": "SSO cutover blocked by preflight checks",
  "preflight": {
    "sso:oidc_enabled": "pass",
    "sso:issuer_reachable": "pass",
    "sso:redirect_uri_match": "fail",
    "sso:admin_in_groups": "pass",
    "sso:breakglass_ready": "pass"
  }
}
```

### 4. Break-Glass Login Flow

```
┌─────────────────────────────────────────────────────────────────┐
│ GET /console/breakglass?token=bg_...                            │
├─────────────────────────────────────────────────────────────────┤
│ 1. Validate token format (bg_ prefix)                           │
│ 2. Query break_glass_tokens WHERE tenant matches, not expired,  │
│    not used, not revoked                                        │
│ 3. bcrypt.Compare(token, token_hash)                            │
│ 4. If match:                                                    │
│    a. Mark used_at = NOW() (one-time)                           │
│    b. Log audit: breakglass.use                                 │
│    c. Issue session cookie (UserType: breakglass)               │
│    d. Redirect to /console/                                     │
│ 5. If fail: generic "invalid or expired token" (no enumeration) │
└─────────────────────────────────────────────────────────────────┘
```

**Session distinction:** `UserType` field in session cookie distinguishes `local`, `oidc`, `breakglass` — enables audit filtering and potential session restrictions.

### 5. Audit Actions

New actions added to `validActions` (Go) and `chk_audit_action_value` (DB migration):

| Action | Trigger | Target Type |
|--------|---------|-------------|
| `auth.mode_change` | SSO cutover or fallback | `system` |
| `breakglass.issue` | Token generated | `breakglass_token` |
| `breakglass.use` | Token consumed for login | `breakglass_token` |
| `breakglass.revoke` | Token manually revoked | `breakglass_token` |
| `breakglass.expire` | Scheduled cleanup of expired tokens | `breakglass_token` |

**Audit entry includes:**
- `actor_type`: `admin` (for issue/revoke) or `breakglass` (for use)
- `actor_email`: Who performed the action
- `target_id`: Token UUID (for token actions) or `auth.mode` (for mode change)
- `before_json` / `after_json`: Mode value or token metadata

### 6. Config Changes

```yaml
# config.yaml additions
auth:
  # Runtime mode stored in DB, this is the boot default
  default_mode: hybrid  # hybrid | sso_only

  breakglass:
    default_ttl: 30m        # CLI default if --ttl not specified
    max_ttl: 24h            # Hard cap
    min_ttl: 5m             # Floor to prevent accidents
    cleanup_interval: 1h    # Prune expired tokens
```

---

## Alternatives Considered

### A. Config-only `oidc_only: true` (current approach)

**Pros:** Simple, already implemented.  
**Cons:** Requires restart, no runtime toggle, no preflight, no break-glass.  
**Decision:** Keep as boot default option, add runtime layer on top.

### B. Shamir secret sharing (Vault pattern)

**Pros:** Cryptographically robust, multi-party custody.  
**Cons:** Overkill for attune's admin-console use case, complex UX, requires physical key distribution.  
**Decision:** Reject — break-glass tokens are sufficient.

### C. Permanent emergency accounts (Entra ID pattern)

**Pros:** Always available, no TTL management.  
**Cons:** Permanent credentials are higher risk, no one-time property.  
**Decision:** Reject — time-limited tokens are safer.

### D. CLI-only recovery (Authentik pattern)

**Pros:** Simple implementation.  
**Cons:** Undiscoverable for operators without docs, no Console UI.  
**Decision:** Implement both CLI and Console UI.

---

## Risks / Tradeoffs

| Risk | Mitigation |
|------|------------|
| Break-glass token leaked | Short TTL (30m default), one-time use, audit on use |
| Operator skips preflight | `skip_breakglass_check` requires explicit flag, logged as warning |
| IdP outage during cutover | Preflight checks issuer reachability, but can't predict future outages |
| Token never issued before cutover | `sso:breakglass_ready` check blocks cutover unless explicit skip |
| Expired token unnoticed | CLI `breakglass list` shows expiry, Console shows warning banner |

---

## Implementation Plan

### Phase 1: Foundation (Day 1)

1. Migration: `break_glass_tokens` table + `system_settings` auth.mode row
2. Domain types: `BreakGlassToken`, `AuthMode` enum
3. Repo: `breakglass.Repo` with Issue/Get/MarkUsed/Revoke/Cleanup
4. Audit actions: Add to `validActions` + DB constraint migration

### Phase 2: Break-Glass Token (Day 1-2)

5. Service: `breakglass.Service` with token generation (crypto/rand + bcrypt)
6. CLI: `attune auth breakglass {issue,list,revoke}`
7. Handler: `GET /console/breakglass` login endpoint
8. Session: Add `UserType` field to session cookie

### Phase 3: SSO Cutover (Day 2)

9. Preflight checks: `sso:*` checks in `internal/preflight/checks/sso.go`
10. Service: `authmode.Service` with GetMode/SetMode/Cutover/Fallback
11. Handler: `POST /fb/v1/console/auth/sso-cutover`, `POST .../sso-fallback`
12. Auth middleware: Check `auth.mode` before showing local login

### Phase 4: Console UI (Day 2-3)

13. Settings page: Security → Auth Mode card with cutover button
14. Settings page: Security → Break-Glass Tokens CRUD
15. Login page: Conditionally hide local form based on mode
16. Break-glass login page: `/console/breakglass` with token input

### Phase 5: Tests & Docs (Day 3)

17. Unit tests: Repo, service, handlers (≥80% coverage)
18. Integration tests: Full cutover → break-glass → fallback flow
19. E2E test: Real OIDC (Keycloak testcontainer) + break-glass login
20. Docs: Update `docs/private-deploy.md` with SSO cutover runbook

---

## Verification

### Unit Tests

- [ ] `breakglass.Repo`: Issue/Get/MarkUsed/Revoke/Cleanup
- [ ] `breakglass.Service`: Token generation, bcrypt hashing, TTL validation
- [ ] `authmode.Service`: Mode transitions, preflight integration
- [ ] Handlers: SSO cutover, fallback, break-glass login
- [ ] Preflight checks: All 5 `sso:*` checks

### Integration Tests

- [ ] Full flow: hybrid → cutover (preflight pass) → sso_only → break-glass login → fallback
- [ ] Cutover blocked: Missing OIDC config, unreachable issuer, no break-glass token
- [ ] Break-glass: Valid token works, expired token fails, used token fails, revoked token fails
- [ ] Audit: All 5 new actions logged correctly

### Manual Verification

- [ ] CLI: `attune auth breakglass issue/list/revoke` works as documented
- [ ] Console: Cutover button with preflight status display
- [ ] Console: Break-glass token issuance modal (token shown once)
- [ ] Login: Local form hidden in sso_only mode
- [ ] Break-glass login: Token URL works, session created with correct UserType

---

## References

- [NIST SP 800-63C-4](https://nvlpubs.nist.gov/nistpubs/SpecialPublications/NIST.SP.800-63C-4.pdf) — Federation assurance levels, replay protection
- [Microsoft Entra Emergency Access](https://learn.microsoft.com/en-us/entra/identity/role-based-access-control/security-emergency-access) — Cloud-only account pattern
- [Okta Admin Session Protection](https://sec.okta.com/articles/protectingadminsessions/) — ASN binding, protected actions
- [Authentik Recovery Keys](https://docs.goauthentik.io/troubleshooting/login/) — CLI-based TTL tokens
- [Ory Kratos Account Recovery](https://www.ory.com/docs/kratos/manage-identities/account-recovery) — Admin-initiated one-time codes
- [Zitadel Break-Glass Gap](https://github.com/zitadel/zitadel/issues/11487) — Cautionary anti-pattern
- [HashiCorp Vault Recovery Mode](https://developer.hashicorp.com/vault/tutorials/monitoring/recovery-mode) — Shamir + recovery mode
