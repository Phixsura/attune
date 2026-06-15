# OIDC SSO Integration Guide

This guide explains how to integrate OIDC-based Single Sign-On (SSO) with attune console.

## Overview

attune supports OIDC (OpenID Connect) for enterprise SSO, allowing users to authenticate via identity providers like Okta, Azure AD, Keycloak, Auth0, Google Workspace, and others.

### Security Features

| Feature | Implementation |
|---------|----------------|
| PKCE | S256 code challenge (prevents authorization code interception) |
| Nonce | Replay attack protection |
| State encryption | AES-256-GCM |
| State validation | Constant-time comparison |
| Cookie security | HttpOnly, Secure, SameSite=Lax |
| SSRF protection | Private IP + cloud metadata blocking |
| Open redirect prevention | Relative paths only |

## Quick Start

### 1. Register attune in your IdP

Create a new OIDC/OAuth2 application in your identity provider with:

```
Application Type: Web Application
Grant Type: Authorization Code with PKCE
Redirect URI: https://<your-domain>/fb/v1/console/auth/oidc/callback
Scopes: openid, profile, email
```

Note down the `client_id` and `client_secret`.

### 2. Configure attune

Add the OIDC section to your `config.yaml`:

```yaml
oidc:
  enabled: true
  issuer_url: "https://your-idp.example.com"
  client_id: "attune-console"
  client_secret: "your-client-secret"
  redirect_uri: "https://your-domain.com/fb/v1/console/auth/oidc/callback"
  provider_name: "Enterprise SSO"
```

### 3. Restart attune

The login page will now show an SSO button alongside local login.

## Configuration Reference

### Required Fields

| Field | Type | Description |
|-------|------|-------------|
| `enabled` | bool | Enable OIDC SSO |
| `issuer_url` | string | IdP's OIDC issuer URL (must support `/.well-known/openid-configuration`) |
| `client_id` | string | Client ID from IdP registration |
| `client_secret` | string | Client secret from IdP registration |
| `redirect_uri` | string | Callback URL (must match IdP configuration exactly) |

### Optional Fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `scopes` | []string | `[openid, email, profile]` | OIDC scopes to request |
| `provider_name` | string | `"SSO"` | Display name on login button |
| `allowed_groups` | []string | `[]` (allow all) | Restrict login to specific groups |
| `role_mapping` | []RoleMapping | `[]` | Map IdP groups to attune roles |
| `user_claim` | string | `"sub"` | Claim to use as user identifier |
| `groups_claim` | string | `"groups"` | Claim containing user groups |
| `oidc_only` | bool | `false` | Hide local login form |
| `skip_issuer_check` | bool | `false` | Skip issuer validation (rare edge cases) |
| `insecure_skip_verify` | bool | `false` | Allow HTTP issuer (testing only) |

### Role Mapping

Map IdP groups to attune roles (`admin` or `member`):

```yaml
oidc:
  # ... other config ...
  role_mapping:
    - role: "admin"
      groups:
        - "platform-admins"
        - "super-admins"
    - role: "member"
      groups:
        - "developers"
        - "support-team"
```

Each entry maps multiple groups to a single role. First matching entry wins (ordered evaluation). Users not matching any group get the default role `member`.

### Group-Based Access Control

Restrict login to specific groups:

```yaml
oidc:
  # ... other config ...
  allowed_groups:
    - "attune-users"
    - "platform-team"
```

Users not in any allowed group will see `not_in_group` error.

## IdP-Specific Configuration

### Okta

```yaml
oidc:
  enabled: true
  issuer_url: "https://your-org.okta.com"
  client_id: "0oa..."
  client_secret: "..."
  redirect_uri: "https://your-domain.com/fb/v1/console/auth/oidc/callback"
  scopes:
    - openid
    - profile
    - email
    - groups
  provider_name: "Okta SSO"
```

In Okta Admin Console:
1. Applications → Create App Integration → OIDC - Web Application
2. Sign-in redirect URI: `https://your-domain.com/fb/v1/console/auth/oidc/callback`
3. Assignments: Assign users/groups

### Azure AD (Entra ID)

```yaml
oidc:
  enabled: true
  issuer_url: "https://login.microsoftonline.com/{tenant-id}/v2.0"
  client_id: "..."
  client_secret: "..."
  redirect_uri: "https://your-domain.com/fb/v1/console/auth/oidc/callback"
  scopes:
    - openid
    - profile
    - email
  provider_name: "Microsoft SSO"
```

In Azure Portal:
1. App registrations → New registration
2. Redirect URI: Web → `https://your-domain.com/fb/v1/console/auth/oidc/callback`
3. Certificates & secrets → New client secret
4. API permissions → Add `openid`, `profile`, `email`

### Keycloak

```yaml
oidc:
  enabled: true
  issuer_url: "https://keycloak.example.com/realms/your-realm"
  client_id: "attune-console"
  client_secret: "..."
  redirect_uri: "https://your-domain.com/fb/v1/console/auth/oidc/callback"
  scopes:
    - openid
    - profile
    - email
    - groups
  provider_name: "Keycloak SSO"
```

In Keycloak Admin:
1. Clients → Create client
2. Client Protocol: openid-connect
3. Access Type: confidential
4. Valid Redirect URIs: `https://your-domain.com/fb/v1/console/auth/oidc/callback`
5. Add `groups` mapper if using group-based access

### Auth0

```yaml
oidc:
  enabled: true
  issuer_url: "https://your-tenant.auth0.com"
  client_id: "..."
  client_secret: "..."
  redirect_uri: "https://your-domain.com/fb/v1/console/auth/oidc/callback"
  provider_name: "Auth0 SSO"
```

In Auth0 Dashboard:
1. Applications → Create Application → Regular Web Applications
2. Allowed Callback URLs: `https://your-domain.com/fb/v1/console/auth/oidc/callback`

### Google Workspace

```yaml
oidc:
  enabled: true
  issuer_url: "https://accounts.google.com"
  client_id: "....apps.googleusercontent.com"
  client_secret: "..."
  redirect_uri: "https://your-domain.com/fb/v1/console/auth/oidc/callback"
  provider_name: "Google SSO"
```

In Google Cloud Console:
1. APIs & Services → Credentials → Create OAuth client ID
2. Application type: Web application
3. Authorized redirect URIs: `https://your-domain.com/fb/v1/console/auth/oidc/callback`

### Dex (Local Testing)

```yaml
# attune config.yaml
oidc:
  enabled: true
  issuer_url: "http://localhost:5556/dex"
  client_id: "attune-console"
  client_secret: "attune-test-secret"
  redirect_uri: "http://localhost:8090/fb/v1/console/auth/oidc/callback"
  insecure_skip_verify: true  # HTTP issuer
  provider_name: "Dex SSO"
```

```yaml
# dex config.yaml
issuer: http://localhost:5556/dex

staticClients:
  - id: attune-console
    secret: attune-test-secret
    name: Attune Console
    redirectURIs:
      - http://localhost:8090/fb/v1/console/auth/oidc/callback

staticPasswords:
  - email: "admin@example.com"
    hash: "$2a$10$..."  # bcrypt hash
    username: "admin"
```

## Login Flow

```
┌─────────┐     ┌─────────┐     ┌─────────┐     ┌─────────┐
│  User   │     │ attune  │     │   IdP   │     │   DB    │
└────┬────┘     └────┬────┘     └────┬────┘     └────┬────┘
     │               │               │               │
     │  Click SSO    │               │               │
     │──────────────>│               │               │
     │               │               │               │
     │               │ Generate PKCE │               │
     │               │ + state + nonce               │
     │               │──────────────>│               │
     │               │               │               │
     │   Redirect to IdP login       │               │
     │<──────────────────────────────│               │
     │               │               │               │
     │  Authenticate │               │               │
     │──────────────────────────────>│               │
     │               │               │               │
     │   Redirect with code          │               │
     │<──────────────────────────────│               │
     │               │               │               │
     │  Callback     │               │               │
     │──────────────>│               │               │
     │               │               │               │
     │               │ Exchange code │               │
     │               │──────────────>│               │
     │               │               │               │
     │               │ ID token      │               │
     │               │<──────────────│               │
     │               │               │               │
     │               │ Verify signature, nonce       │
     │               │ Check allowed_groups          │
     │               │ Map role                      │
     │               │               │               │
     │               │ Upsert user   │               │
     │               │───────────────────────────────>
     │               │               │               │
     │               │ Create session│               │
     │               │               │               │
     │  Redirect to console          │               │
     │<──────────────│               │               │
     │               │               │               │
```

## Error Handling

| Error Code | Meaning | User Action |
|------------|---------|-------------|
| `state_expired` | Login took too long (>10 min) | Try again |
| `state_mismatch` | State tampering detected | Try again |
| `auth_failed` | IdP authentication failed | Check credentials |
| `user_cancelled` | User cancelled at IdP | Try again if needed |
| `not_in_group` | User not in `allowed_groups` | Contact admin |
| `token_failed` | Token exchange failed | Check IdP logs |
| `internal_error` | Server error | Check attune logs |

Error pages display a trace ID for debugging.

## Monitoring

### Metrics

| Metric | Type | Description |
|--------|------|-------------|
| `attune_oidc_login_total{result}` | Counter | Login attempts by result |
| `attune_oidc_login_duration_seconds` | Histogram | Full login flow latency |
| `attune_oidc_token_exchange_duration_seconds` | Histogram | Token exchange latency |

### Logs

OIDC events are logged with `trace_id` and `span_id` for distributed tracing:

```json
{"level":"INFO","msg":"[oidc.Handler.Callback] login success,sub:user@example.com,role:member","trace_id":"..."}
{"level":"WARN","msg":"[oidc.Handler.Callback] user not in allowed groups,sub:user@example.com","trace_id":"..."}
```

## Security Considerations

### Production Checklist

- [ ] Use HTTPS for `redirect_uri`
- [ ] Set `insecure_skip_verify: false` (default)
- [ ] Configure `allowed_groups` if restricting access
- [ ] Review `role_mapping` for proper privilege assignment
- [ ] Rotate `client_secret` periodically
- [ ] Monitor `attune_oidc_login_total{result="..."}` for anomalies

### Session Security

- Sessions are HMAC-signed with `console.session_key`
- Session cookies are `HttpOnly`, `Secure`, `SameSite=Lax`
- OIDC state cookies expire in 10 minutes
- State is encrypted with AES-256-GCM

### SSRF Protection

The OIDC client blocks requests to:
- Private IP ranges (10.x, 172.16-31.x, 192.168.x)
- Loopback (127.x, ::1)
- Link-local (169.254.x)
- Cloud metadata endpoints (169.254.169.254)

## Troubleshooting

### "Discovery failed" at startup

- Check `issuer_url` is accessible from attune server
- Verify `/.well-known/openid-configuration` returns valid JSON
- Check network/firewall rules

### "Invalid redirect_uri"

- Ensure `redirect_uri` in config matches IdP configuration exactly
- Check for trailing slashes, protocol (http vs https)

### "Token exchange failed"

- Verify `client_secret` is correct
- Check IdP logs for detailed error
- Ensure IdP clock is synchronized (JWT validation is time-sensitive)

### "Not in allowed groups"

- Check IdP returns `groups` claim
- Verify group names match `allowed_groups` exactly (case-sensitive)
- Some IdPs require additional scope (`groups`) or mapper configuration

### Users getting wrong role

- Check `role_mapping` order (first match wins)
- Verify group names in IdP match mapping configuration
- Default role is `member` if no mapping matches

## Migration from Local Auth

To migrate existing users to SSO:

1. Configure OIDC with `oidc_only: false` (allow both)
2. Have users log in via SSO (creates `oidc_users` records)
3. Verify SSO works for all users
4. Optionally set `oidc_only: true` to hide local login

Note: Local admin accounts remain functional for emergency access even with `oidc_only: true` (direct URL `/console/login` still works).
