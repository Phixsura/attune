<!-- markdownlint-disable MD013 -->

# Governance plane: service accounts, delegated admin, and breakglass lifecycle

| Field | Value |
| --- | --- |
| **Issue** | N/A (platform maturity subtrack under [#202](https://github.com/Phixsura/attune/issues/202)) |
| **Status** | Implemented |
| **Started** | 2026-07-05 |
| **Related** | [#56](https://github.com/Phixsura/attune/issues/56) (production auth guardrails), [#149](https://github.com/Phixsura/attune/issues/149) (production readiness preflight), [#153](https://github.com/Phixsura/attune/issues/153) (MCP governance), [#41](https://github.com/Phixsura/attune/issues/41) (API-key scopes), [platform maturity program](./2026-07-05-platform-maturity-program.md) |

---

## Problem

Attune already has the first layer of access control:

- OIDC / SSO for human login;
- RBAC for product roles;
- API-key scopes for machine usage;
- production startup guardrails for unsafe auth/session settings;
- audit logging for privileged actions.

That is enough to authenticate users and block obvious misuse. It is not yet
enough to model the full governance plane that mature platforms expose.

The missing pieces are the ones operators rely on in real organizations:

- service accounts or machine principals that are not tied to a human login;
- delegated admin roles that separate day-to-day operators from security
  owners;
- temporary elevation / breakglass with explicit expiry and audit visibility;
- identity lifecycle hooks for external directory changes;
- a clear distinction between human identity, automation identity, and session
  identity;
- reviewable policy surfaces for privileged objects and actions.

GitLab, Vault, and Backstage converge on this split. They do not treat identity
as a single blob of "logged in or not". They make the governance model explicit.

## Goals

- Separate human, machine, and session identity in the product model.
- Add service accounts or equivalent machine principals for automation use.
- Add delegated admin roles so privilege can be scoped and reviewed.
- Add a breakglass / temporary-elevation path with expiry and audit evidence.
- Add identity lifecycle hooks for external IdP changes where supported.
- Keep privileged object changes visible in audit logs and admin surfaces.
- Preserve existing OIDC / SSO and API-key workflows while adding the new
  governance layer.

## Non-goals

- Do not replace the existing SSO provider or session model.
- Do not redesign the Console auth flow.
- Do not require full SCIM implementation in the first pass.
- Do not collapse machine identity into API keys only.
- Do not introduce a second authentication stack.

## Proposal

### 1. Model identity explicitly

Introduce a small internal model that distinguishes:

- `human` principals
- `service_account` principals
- `session` instances
- `delegated_admin` grants
- `elevated` or `breakglass` grants

This model should be used by authorization and audit code, even if the first
UI remains small.

### 2. Add service accounts as first-class objects

Service accounts should be:

- created and rotated by admins;
- scoped to tenant or workspace boundaries;
- auditable;
- revocable or removable without revoking the owning human account;
- clearly separated from personal login sessions.

The first implementation can keep the UI minimal, but the lifecycle should be
real: create, list, disable, rotate, revoke.

### 3. Add delegated admin and elevation flows

Not every privileged action should require full superuser status.

The governance layer should support:

- delegated admin roles with explicit permission sets;
- elevation requests or temporary grants with TTLs;
- a breakglass path for recovery when SSO or the normal admin path is
  unavailable;
- audit events for grant creation, grant use, and grant expiry.

### 4. Add identity lifecycle hooks

If the external identity provider changes, Attune should have a place to react:

- user disabled or deleted;
- group membership changed;
- role mapping changed;
- service-account ownership changed.

This does not require full directory sync on day one. It does require a clear
integration seam.

### 5. Keep policy review visible

Privileged objects and actions should be reviewable in one place:

- which identities have elevated access;
- which service accounts exist;
- which grants are active;
- when the last elevation happened;
- what was changed by whom.

## Alternatives considered

### Keep API keys as the only machine identity

Rejected. API keys are useful, but they do not model ownership, delegation, or
temporary elevation well enough.

### Fold delegated admin into existing RBAC only

Rejected. RBAC alone does not express the lifecycle and audit needs around
service accounts and breakglass access.

### Add SCIM before anything else

Rejected. External sync is useful, but the repo first needs a stable identity
model to sync into.

## Risks / Tradeoffs

- More identity types can confuse operators if labels are not crisp.
- Breakglass paths can become unsafe if they are not time-bound and auditable.
- Service accounts can sprawl if creation and ownership are not visible.
- Delegated admin needs careful wording so it does not read like a second
  superuser model.

## Implementation Plan

1. Introduce identity-principal and grant types in the backend.
2. Add audit events and admin queries for identity/grant lifecycle.
3. Add service-account creation, rotation, disable/revoke, and delete flows.
4. Add delegated admin and temporary-elevation flows.
5. Add UI surfaces for review and operational clarity.
6. Add tests for authz, expiration, and audit coverage.

## Verification

- Unit tests for principal classification and grant evaluation.
- Authorization matrix tests for service accounts and delegated admins.
- Audit-log tests for grant creation, use, and expiry.
- Manual operator review that the UI distinguishes human, machine, and session
  identity.

## References

- [GitLab service accounts](https://docs.gitlab.com/user/profile/service_accounts/)
- [GitLab SAML](https://docs.gitlab.com/integration/saml/)
- [Vault policies](https://developer.hashicorp.com/vault/docs/concepts/policies)
- [Vault auth methods](https://developer.hashicorp.com/vault/docs/auth)
- [Vault identity](https://developer.hashicorp.com/vault/docs/secrets/identity)
- [Backstage permissions overview](https://backstage.io/docs/permissions/overview/)
- [Backstage permission policy](https://backstage.io/docs/permissions/writing-a-policy/)
