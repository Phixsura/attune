# Platform production safety contract for Console and deployment profile

| Field | Value |
| --- | --- |
| **Issue** | [#56](https://github.com/Phixsura/attune/issues/56) |
| **Status** | Implemented |
| **Started** | 2026-07-05 |
| **Related** | [#42](https://github.com/Phixsura/attune/issues/42) (Helm chart), [#64](https://github.com/Phixsura/attune/issues/64) (P0/P1 hardening batch), [#149](https://github.com/Phixsura/attune/issues/149) (production readiness preflight), [security-and-reliability-hardening.md](../../../security-and-reliability-hardening.md), [private-deploy.md](../../../private-deploy.md), [k8s-deploy.md](../../../k8s-deploy.md) |

## Problem

Attune is no longer missing the raw building blocks for enterprise operation.
The current codebase already has:

- config-first runtime loading from one private YAML file;
- secure Console session cookies and same-origin redirect handling;
- an idempotent bootstrap-admin path;
- a production-oriented Helm chart with `profile: production` checks;
- a shared preflight/doctor stack for operator visibility; and
- deployment docs that already describe TLS-fronted production installs.

The problem is that these pieces do not yet form one executable production
contract.

Today:

- `internal/infra/config` validates shape and field-level invariants, but not the
  deployment intent behind them.
- `internal/preflight` can report status, but startup does not consume the same
  safety model.
- Helm knows about `profile: production`, but the Go runtime does not.
- The docs already describe stricter production behavior than the runtime
  enforces.

That split creates a seam between "looks valid" and "is production-safe". The
seam is exactly where issue #56 lives: the platform needs an explicit production
mode and a shared safety contract for Console auth/session behavior instead of a
set of partially aligned guardrails.

## Goals

- Introduce one explicit runtime deployment profile that the process can reason
  about.
- Make production startup fail fast on unsafe Console/auth/session combinations.
- Reuse the same production safety contract in startup, `attune doctor`, and the
  Console preflight endpoint.
- Keep observability metadata aligned with the runtime profile when operators do
  not override it explicitly.
- Keep local development and evaluation workflows intact.
- Make bootstrap-admin lifecycle explicit rather than implicit.
- Align Helm, docs, and runtime behavior so they describe the same contract.
- Add focused tests for both allowed dev paths and rejected production paths.

## Non-goals

- Reintroduce old Console dev-login or insecure-cookie behavior.
- Replace the Console auth model or switch to a different identity provider.
- Add a generic plugin system for arbitrary policy checks.
- Rework the broader P0/P1 hardening batch that is already covered by other
  workstreams.
- Replace Helm, preflight, or the config-first runtime.
- Turn preflight into continuous monitoring.

## Current State

| Layer | What exists today | Gap |
| --- | --- | --- |
| Config loading | One YAML file, `KnownFields(true)`, strong validation of required values. | No explicit runtime deployment profile; validation is mostly structural. |
| Console auth/session | Timing-safe login, secure cookies, SameSite=Lax, same-origin redirect checks, one-shot bootstrap helper. | Safety is enforced locally in handlers, not as one production contract. |
| Startup wiring | Console is enabled when `ConsoleSessionKey` is set and the router can be built. | No production-aware startup gate. |
| Helm chart | `profile: production` already fails on unsafe production topology choices. | Chart profile does not reach the Go runtime as a first-class deployment mode. |
| Preflight/doctor | Shared operator-facing report with pass/warn/fail output and remediation text. | It reports readiness, but startup does not consume the same policy. |
| Documentation | Private deploy and K8s docs already describe TLS-fronted production installs and bootstrap-admin cleanup. | Docs are stricter than enforcement in several places. |

The platform is therefore in a "safe components, unsafe seams" phase. The goal
of this proposal is to close the seams without widening the surface area.

## Proposal

### 1. Add a runtime deployment profile

Add a top-level `profile` field to runtime config with explicit values:

- `dev`
- `production`

Backward compatibility should default empty / omitted config to `dev`.

The chart already has a `profile` value, so the proposal is to make that intent
flow into the runtime config instead of living only in Helm validation. The
production profile becomes the single switch that says "apply production safety
rules now".

The same profile should also become the default deployment environment label for
observability when operators do not set `observability.environment` explicitly,
so traces and metrics stop advertising production instances as `dev`.

### 2. Create one shared production safety contract

Define a shared Console/platform safety contract and use it from both runtime and
operator reporting. The contract should cover:

- `console.base_url` must be present and parse correctly.
- production `console.base_url` must be HTTPS.
- `console.session_key` must be present and at least 32 bytes.
- session cookies must remain `HttpOnly`, `Secure`, and `SameSite=Lax`.
- login redirects must stay same-origin.
- bootstrap-admin must be present when the admins table is empty, but it should
  be treated as a one-shot initialization path rather than a permanent auth
  surface.
- trusted-proxy / forwarded-header semantics must be explicit and documented for
  production deployments.
- any future reintroduced local-only auth or insecure-cookie knobs must be fatal
  in production.

The important part is not the exact helper shape, but the fact that the same
rules are evaluated everywhere instead of being reimplemented separately.

### 3. Make startup consume the contract

`cmd/attune/server.go` should evaluate the production safety contract after
config load and before router wiring / serving begins.

Production behavior:

- hard fail on any unsafe auth/session combination;
- hard fail on missing or invalid production base URL;
- hard fail when bootstrap-admin is missing on a fresh production install;
- keep local/dev workflows permissive when `profile != production`.

The startup path should not duplicate validation logic. It should call the same
shared contract that powers operator reports.

### 4. Keep `doctor` and Console preflight authoritative

`attune doctor` and `/fb/v1/console/system/preflight` should remain the operator
surface area for the same contract.

- `doctor` is the CLI acceptance gate.
- Console preflight is the in-app visibility surface.
- Both should report the same check names, same remediation text, and same pass /
  warn / fail semantics.

That keeps the production story honest: operators can inspect readiness, and the
runtime itself can refuse to boot when the report would be unacceptable.

### 5. Align Helm and docs with runtime behavior

Helm already has production topology validation, so this proposal extends that
pattern instead of inventing a second one.

- Helm should render the runtime `profile` into `config.yaml`.
- Helm validation should continue to fail fast for unsafe production topology.
- Private deploy docs should teach the same production profile and bootstrap
  lifecycle that the code enforces.
- The security-hardening docs should remain the detailed operator reference, but
  they should not describe behavior that is not backed by code.

## Alternatives Considered

### Keep `profile` only in Helm

Rejected. That would leave bare-metal and Compose deployments without a runtime
production switch, and the Go process would still be unable to reason about its
own deployment mode.

### Add a separate environment variable for production mode

Rejected. Attune is config-first and intentionally avoids env-var override
semantics for process config.

### Expand preflight only

Rejected. Preflight is useful, but advisory checks are not enough for a
production safety contract if the startup path can still boot into an unsafe
state.

### Encode everything directly in `config.Validate`

Rejected. `config.Validate` is the right place for structural invariants and
field-level correctness, but production policy needs a higher-level contract and
different behavior in dev versus production.

### Introduce a separate policy file or admission controller

Rejected. That would add moving parts before the platform has a single
internal contract to anchor against.

## Risks / Tradeoffs

- A stricter startup gate can block existing "works on my machine" configs.
  The upside is a safer production boundary; the downside is more explicit
  remediation work when operators are upgrading old installs.
- Bootstrap-admin lifecycle checks must stay operable during first boot and
  subsequent restarts.
- Trusted-proxy semantics need a documented story for reverse proxies and ingress.
  If the rules are too vague, operators will not know which hop count to choose.
- A single production profile is intentionally coarse. If the platform later
  needs more than `dev` and `production`, the next profile should be justified by
  a real deployment need, not by convenience.

## Implementation Plan

1. Add `profile` to the runtime config schema with a backward-compatible `dev`
   default.
2. Extract or formalize the shared production safety contract.
3. Make `cmd/attune/server.go` consume the contract before serving traffic.
4. Reuse the contract in `attune doctor` and Console preflight.
5. Update Helm rendering so production profile reaches the runtime config.
6. Update docs so they describe the enforced behavior, not only the intended
   behavior.
7. Add regression tests for the production-fail and dev-allow paths.

## Verification

- `go test ./...`
- `go vet ./...`
- `go test -race ./...` on the touched Go packages
- `pnpm tsc -b --noEmit`
- `pnpm biome check`
- `pnpm exec vite build`
- `pnpm vitest run --coverage`
- `helm lint deploy/helm/attune`
- startup smoke tests that prove:
  - production refuses missing / invalid `console.base_url`;
  - production refuses unsafe auth/session combinations;
  - dev installs still boot cleanly;
  - `doctor` and Console preflight report the same contract.

## References

- [Issue #56: production console auth and secure-cookie guardrails](https://github.com/Phixsura/attune/issues/56)
- [Issue #42: Helm chart for Kubernetes deployment](https://github.com/Phixsura/attune/issues/42)
- [Issue #64: P0/P1 production / security / reliability hardening batch](https://github.com/Phixsura/attune/issues/64)
- [Issue #149: production readiness preflight](https://github.com/Phixsura/attune/issues/149)
- [security-and-reliability-hardening.md](../../../security-and-reliability-hardening.md)
- [private-deploy.md](../../../private-deploy.md)
- [k8s-deploy.md](../../../k8s-deploy.md)
- [internal/infra/config/config.go](../../../../internal/infra/config/config.go)
- [internal/handlers/console/auth/bootstrap.go](../../../../internal/handlers/console/auth/bootstrap.go)
- [internal/handlers/console/internal/session/session.go](../../../../internal/handlers/console/internal/session/session.go)
- [internal/preflight/checks/config.go](../../../../internal/preflight/checks/config.go)
- [cmd/attune/server.go](../../../../cmd/attune/server.go)
- [deploy/helm/attune/templates/_helpers.tpl](../../../../deploy/helm/attune/templates/_helpers.tpl)
