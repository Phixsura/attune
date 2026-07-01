# Outbound adapter conformance and delivery safety harness

| | |
|---|---|
| **Issue** | [#167](https://github.com/Phixsura/attune/issues/167) |
| **Status** | Implemented |
| **Started** | 2026-07-01T14:58:54+08:00 |
| **Related** | [#34](../06/2026-06-14-outbound-adapter-framework.md) (outbound framework), [#31](../06/2026-06-20-slack-adapter-wiring.md) (Slack adapter), [#32](../06/2026-06-20-discord-adapter.md) (Discord adapter), [#66](../06/2026-06-08-channel-agnostic-inbound.md) (inbound framework precedent), [#27](../06/2026-06-13-daily-digest-rollup.md) (digest consumer) |

---

## Problem

attune now has five outbound destination types behind the `internal/outbound`
framework: raw webhook, GitHub Issue, Slack, Discord, and Lark. The architecture
is already good: adapters self-register, `cmd/attune` is the only blank-import
site, event delivery goes through `outbound.LookupEvent`, digest delivery goes
through `outbound.LookupDigest`, and `notify.Transport` owns HTTP send, retry,
and backoff.

The weak point is not coverage volume. The weak point is that each adapter proves
its own behavior in its own test dialect. There is no shared executable contract
for:

- rendering shape and stable request snapshots
- truncation bounds
- rate-limit classification
- terminal versus retryable errors
- redaction of URL tokens, secrets, authorization values, and sensitive body text
- mention-safety on chat and issue surfaces
- adapter documentation requirements

That means a new adapter can appear correct while skipping one of the safety
properties the current adapters already learned the hard way. It also means a
small behavior drift in one adapter can escape if that adapter's local tests do
not happen to cover the same status code, token-in-path URL, or mention payload.

The repository already has the right in-house pattern on the inbound side:
`internal/inbound/inboundtest.TestAdapterContract` is a shared suite that every
inbound adapter calls from its own package. Outbound needs the same pattern, but
with delivery-specific safety checks.

## Goals / Non-goals

### Goals

- Add an `internal/outbound/outboundtest` package that exposes shared
  conformance runners for event channels, digest channels, response checkers,
  golden rendering, log redaction, and mention safety.
- Apply the shared suite to every existing adapter: raw webhook, GitHub Issue,
  Slack, Discord, and Lark.
- Add stable request fixture snapshots where the request body is intentionally
  stable, with dynamic fields normalized before comparison.
- Add a repository gate that makes it hard to add an adapter without a
  `conformance_test.go` file using the shared runner.
- Document the outbound adapter contract in `internal/outbound/README.md`.
- Fix adapter behavior that fails the contract, including token-in-path URL logs
  and sensitive request-body logs.
- Keep the framework boundary intact: adapters depend on `internal/outbound`,
  not on service, repo, handlers, or notify internals.
- Keep the work test-focused. The purpose is to lock the delivery contract, not
  to redesign provider UX.

### Non-goals

- Do not add new outbound channels.
- Do not change the outbox retry schedule.
- Do not require live Slack, Discord, Lark, GitHub, or customer webhook
  credentials in CI.
- Do not add a runtime plugin system. The compile-time blank-import model from
  #34 remains the contract.
- Do not make Console notify-target URL redaction a hidden side effect of this
  issue. The conformance work defines adapter and delivery-surface redaction.
  Any change to whether the notify-target list API returns full URLs should be a
  deliberate API/product decision.
- Do not make `Retry-After` parsing part of this issue. The current
  `ResponseChecker` receives only status and body; header-aware retry timing is a
  separate transport interface decision.

## Current code reality

| Area | Verified state | Consequence for #167 |
|---|---|---|
| Framework interfaces | `EventChannel`, `DigestChannel`, `Rendered`, and `ResponseChecker` live in [`internal/outbound/outbound.go`](../../../../internal/outbound/outbound.go). | The conformance suite can test adapters through public outbound interfaces. |
| Registry | `Register`, `LookupEvent`, `LookupDigest`, and `Channels` live in [`internal/outbound/registry.go`](../../../../internal/outbound/registry.go). | The suite can assert ID/support behavior without importing sibling adapters. |
| Assembly | `cmd/attune` blank-imports all outbound adapters in one legal place. | A cmd-level assembly test can continue to verify registered channels. |
| Event send path | `sendByDestType` uses `outbound.LookupEvent`, renders once, and translates `outbound.ErrTerminal` to `notify.ErrTerminal` in [`internal/service/outbox/outbox_worker_send.go`](../../../../internal/service/outbox/outbox_worker_send.go). | Contract failures map into existing dead-queue and retry behavior. |
| Digest send path | The digest sender uses `outbound.LookupDigest` in [`internal/service/digest/sender.go`](../../../../internal/service/digest/sender.go). | Digest-capable adapters need the same request/redaction/golden checks. |
| Console test-send | `notify.TestSend` is registry-driven and no-retry in [`internal/notify/test_send.go`](../../../../internal/notify/test_send.go). | Adapter conformance protects both background delivery and the Test button. |
| Existing precedent | `internal/inbound/inboundtest` already provides a shared adapter contract. | Use the same pattern instead of inventing a new test idiom. |
| Adapter docs | [`internal/outbound/README.md`](../../../../internal/outbound/README.md) documents how to add an adapter, but not conformance or fixtures. | README must become the adapter contract entry point. |

Existing adapter tests are broad but local:

| Adapter | Current `func Test` count | Shared conformance today |
|---|---:|---|
| Discord | 37 | No |
| Raw webhook (`generic`) | 16 | No |
| GitHub Issue | 11 | No |
| Lark | 34 | No |
| Slack | 42 | No |

There are no outbound `conformance_test.go` files and no outbound adapter
`testdata` snapshots today.

## Acceptance criteria mapping

| #167 acceptance criterion | Proposal mechanism | Verification signal |
|---|---|---|
| Existing adapters pass the same safety/error contract. | `internal/outbound/outboundtest` plus adapter-local `conformance_test.go` files for all five adapters. | `go test ./internal/outbound/...` fails when any adapter violates the shared runner. |
| New adapters cannot skip redaction or retry classification tests. | `scripts/lint-outbound-conformance.sh` requires every adapter package to import and call `outboundtest`. | CI fails before adapter-specific tests can be merged without the shared runner. |
| CI catches rendering drift. | Golden request snapshots normalize dynamic values and fail on unexpected method/header/body changes. | Snapshot diffs fail normal test runs unless intentionally updated. |
| CI catches response drift. | Shared response profile cases exercise success, retryable, terminal, provider-code, and truncation behavior. | Response case failures show the adapter, profile, status, and expected verdict. |
| Docs explain the adapter contract. | `internal/outbound/README.md` gains a conformance checklist and current adapter matrix. | New adapter docs point to the same runner, fixtures, and profile definitions. |

## Findings to encode in the contract

| Finding | Evidence | Contract decision |
|---|---|---|
| Token-in-path URL redaction is already a project invariant for Slack, Lark, and Discord. | `notifytarget.URLIsCredential` marks those destination types as URL credentials in [`notify_targets.go`](../../../../internal/repo/notifytarget/notify_targets.go). | Every adapter log line and delivery error surface must be tested with a token-bearing URL. |
| Lark currently logs the full webhook URL. | [`internal/outbound/adapter/lark/lark.go`](../../../../internal/outbound/adapter/lark/lark.go) logs `dst.URL` directly. | Lark must redact to host-only before the conformance suite passes. |
| GitHub currently logs the generated issue body. | [`internal/outbound/adapter/githubissue/githubissue.go`](../../../../internal/outbound/adapter/githubissue/githubissue.go) logs a truncated body. | Request bodies containing customer feedback must not be logged by adapters. |
| Raw webhook already logs body size instead of body content. | [`internal/outbound/adapter/generic/generic.go`](../../../../internal/outbound/adapter/generic/generic.go) logs `body_bytes`. | Make body-size logging the expected safe pattern for sensitive payloads. |
| Outbox operator fields are already URL-redacted. | [`internal/handlers/console/outbox/handler.go`](../../../../internal/handlers/console/outbox/handler.go) redacts destination target, last error, and dead reason. | Conformance should complement this by preventing adapter-generated leaks before they enter errors. |
| NotifyTarget list returns `url`. | [`proto/attune/v1/notify_target.proto`](../../../../proto/attune/v1/notify_target.proto) exposes `url`, and [`toNotifyProto`](../../../../internal/handlers/console/notifytarget/notify_targets.go) copies it. | Document this as outside the adapter conformance boundary unless the API contract is intentionally changed. |
| GitHub 403 classification has legacy drift. | `outbound.CheckGitHub` treats every 403 as retryable, while the old notify-adapter comment says secondary rate limits should be parsed from the body. | Treat 403 as retryable only when the response body indicates rate limiting; treat ordinary forbidden/bad-token 403 responses as terminal. |
| Lark 200 with invalid JSON currently succeeds. | `checkLarkResponse` returns nil when JSON unmarshal fails. | Treat malformed or missing Lark provider JSON on HTTP 200 as terminal provider-contract failure unless it explicitly contains `StatusCode:0`. |
| Some adapters under-read the real outbox envelope shape. | The outbox envelope stores title, urgency, rationale, and attrs under `feedback.enriched`, while TestSend uses a flatter shape. | Canonical conformance fixtures use the outbox shape and adapters must support both outbox and TestSend shapes. |
| `ResponseChecker` cannot read headers. | The signature is `func(ctx, status, body) error`. | Rate-limit conformance covers status/body classification only. Header-aware retry timing is not part of this issue. |

## Threat model

The conformance suite protects four asset classes:

| Asset | Examples | Leak or drift channel |
|---|---|---|
| URL credentials | Slack/Lark/Discord webhook path tokens | adapter logs, returned errors, outbox/dead-queue fields, golden snapshots |
| Header and body credentials | GitHub `Authorization`, raw webhook `Secret`, Lark signature secret | request snapshots, debug logs, assertion failure messages |
| Customer content | feedback title/content, user ID, rationale, attributes | adapter request logs, GitHub issue body logs, provider error text copied into logs |
| Operational correctness | terminal/retryable verdicts, provider body codes, truncation limits | response checker drift, silent success on malformed provider responses |

The attacker model is intentionally simple: any tenant-controlled feedback text,
destination URL, provider response body, or destination secret can contain a
unique marker that must not appear in logs, snapshots, or operator-facing
delivery errors unless the contract explicitly allows it. Mention safety uses
the same model: user-controlled text may contain provider-specific mention
syntax and must not create an active notification side effect.

## Industry alignment findings

The industry pattern is not "more tests"; it is a named contract plus an
executable suite that downstream implementations must call.

| Project / program | Practice to borrow | attune decision |
|---|---|---|
| Kubernetes Gateway API conformance | Named profiles, common test suite, and submitted conformance reports. | Define a small outbound profile matrix: event, digest, URL credential, active mention surface, provider body errors. |
| Kubernetes CSI sanity tests | A reusable package and CLI validate drivers against the spec independently of product-specific tests. | `outboundtest` should be a reusable Go package, not copy-pasted test helpers in each adapter. |
| Smithy HTTP protocol compliance tests | Protocol request/response examples become executable serialization assertions. | Store canonical request snapshots for stable adapter renderings. |
| Terraform provider acceptance tests | Real lifecycle behavior is tested through the public provider surface, not internal helpers only. | Conformance should call `RenderEvent` and `RenderDigest`, then build real `http.Request` values. |
| Pact provider verification | Interactions are replayed and verified against a shared contract. | Response classification cases should be table-driven and replayed for every adapter. |
| Spring Cloud Contract | Contracts generate verification artifacts so provider behavior cannot drift silently. | Golden snapshots should make rendering drift visible in CI. |
| Prometheus Alertmanager | Routing and receivers are separated, while retryability is a clear integration verdict. | Keep shared transport/backoff, but require every adapter to state response verdicts consistently. |
| OpenTelemetry Collector | Many components plug into a stable pipeline model with component-specific config. | Keep the #34 adapter registry; conformance tests validate each component without weakening boundaries. |
| Apprise | Many notification providers share a central plugin registry and URL-safe service abstractions. | Preserve attune's registry model, but test service-specific URL credential handling. |
| shoutrrr | Notification services share a common send surface while each service owns URL and payload semantics. | Keep per-adapter render/check logic, but run it through one common safety harness. |

## Proposal

### 1. Add `internal/outbound/outboundtest`

Add a test-only helper package:

```text
internal/outbound/outboundtest/
  contract.go       event and digest conformance runners
  response.go       shared response-classification cases
  golden.go         stable JSON normalization and snapshot comparison
  logcapture.go     logext/slog capture helpers
  fixtures.go       canonical event envelope and digest view builders
```

The package should expose small typed entry points:

```go
type EventCase struct {
    Channel       outbound.EventChannel
    Target        outbound.Target
    Envelope      *outbound.Envelope
    Golden        string
    Capabilities  Capabilities
    ResponseCases []ResponseCase
}

type DigestCase struct {
    Channel       outbound.DigestChannel
    Target        outbound.Target
    View          any
    Golden        string
    Capabilities  Capabilities
    ResponseCases []ResponseCase
}

type Capabilities struct {
    URLIsCredential           bool
    HasActiveMentions         bool
    RequiresAuthHeader        bool
    AllowsHTTP201             bool
    AllowsHTTP204             bool
    RequiresProviderCodeZero  bool
    PreservesRawCustomerBody  bool
}

func TestEventChannel(t *testing.T, tc EventCase)
func TestDigestChannel(t *testing.T, tc DigestCase)
func TestResponseChecker(t *testing.T, check outbound.ResponseChecker, cases []ResponseCase)
```

The exact exported shape can be adjusted during implementation, but the package
must stay boring: no reflection-heavy framework, no external dependency, no
network calls, and no dependency from `internal/outbound` root into adapters.

Log capture should be serial, not parallel. `logext` writes through the process
global `slog` default logger, so tests that install a capture handler must not
call `t.Parallel` while the handler is active.

### 2. Define conformance profiles

Profiles make the suite explicit about which checks are universal and which are
capability-gated.

| Profile | Required checks |
|---|---|
| `event` | non-empty stable ID, successful `RenderEvent`, request method and headers, response checker profile, no secret leaks in request snapshot or logs |
| `digest` | successful `RenderDigest`, digest request snapshot, digest redaction, response checker profile |
| `url-credential` | URL path/query/token never appears in logs, errors, or snapshots; host-only redaction is allowed |
| `auth-header` | `Authorization` or equivalent auth headers are present in the request but normalized out of snapshots and never logged |
| `active-mention-surface` | user-controlled mention payloads are escaped, neutralized, or rendered only into fields that cannot notify users |
| `provider-body-code` | provider-specific success/failure body codes are parsed; malformed provider bodies do not silently succeed |
| `raw-body-preserving` | raw webhook preserves the customer envelope by design, but still must not log body content |

Each adapter opts into profiles through a typed `Capabilities` value. The runner
must not expose a "skip redaction" or "skip retry classification" escape hatch.
If a provider cannot satisfy a profile, the exception belongs in this proposal
or a separate proposal, not in an adapter-local test.

### 3. Add adapter-local conformance tests

Each adapter gets a package-local file so it can instantiate package-private
`channel{}` values without exporting constructors:

```text
internal/outbound/adapter/discord/conformance_test.go
internal/outbound/adapter/generic/conformance_test.go
internal/outbound/adapter/githubissue/conformance_test.go
internal/outbound/adapter/lark/conformance_test.go
internal/outbound/adapter/slack/conformance_test.go
```

Each file calls `outboundtest.TestEventChannel` and, where supported,
`outboundtest.TestDigestChannel`.

Expected capability declarations:

| Adapter | Event | Digest | URL credential | Active mention surface | Response profile |
|---|---:|---:|---:|---:|---|
| Raw webhook | Yes | Yes | No | No | Generic webhook: 2xx success, 408/429 retryable, other 4xx terminal, 5xx retryable |
| GitHub Issue | Yes | No | No | Yes | GitHub: 200/201 success, 403 retryable, 408/429 retryable, other 4xx terminal, 5xx retryable |
| Slack | Yes | Yes | Yes | Yes | Webhook/chat: 2xx success, 408/429 retryable, other 4xx terminal, 5xx retryable |
| Discord | Yes | Yes | Yes | Yes | Webhook/chat: 2xx success including 204, 408/429 retryable, other 4xx terminal, 5xx retryable |
| Lark | Yes | Yes | Yes | Yes | Lark: HTTP 200 with `StatusCode:0` success, `StatusCode:9499` retryable, other nonzero provider codes terminal, HTTP 429 retryable, other 4xx terminal, 5xx retryable |

Provider-specific decisions:

| Adapter | Decision |
|---|---|
| GitHub Issue | Parse 403 response bodies. Secondary-rate-limit or primary-rate-limit messages are retryable; ordinary permission, bad-token, disabled-issues, or validation 403 responses are terminal. |
| GitHub Issue | Neutralize user-controlled `@` mention tokens in title/body/table values using an ASCII-visible escape strategy or a documented `\u200d` insertion. Do not neutralize attune-owned repository labels or code spans unless they contain user-controlled values. |
| Lark | HTTP 200 is success only when the provider body parses and reports `StatusCode:0`. Malformed JSON, empty body, or missing status code is terminal provider-contract failure. |
| Lark | `StatusCode:9499` remains retryable. Other nonzero Lark status codes are terminal unless provider docs identify a specific retryable code. |

### 4. Golden request snapshots

Add stable snapshots under each adapter's `testdata/` directory:

```text
internal/outbound/adapter/<channel>/testdata/event_request.json
internal/outbound/adapter/<channel>/testdata/digest_request.json
```

The snapshot should record the request method, URL host shape, selected headers,
and normalized JSON body. It must not store secrets. Dynamic values should be
normalized before comparison:

- signatures become `<signature>`
- timestamps generated during `Build` become `<timestamp>`
- authorization tokens become `<authorization>`
- delivery IDs become `<delivery-id>` unless the adapter intentionally exposes a
  stable ID header
- map key order is canonicalized through JSON decoding and re-encoding

Snapshots should include both ordinary and adversarial fixture content:

| Fixture | Purpose |
|---|---|
| Canonical event envelope | Locks the normal event shape that outbox sends. |
| TestSend-shaped event envelope | Locks the Console Test button path, whose feedback fields are flatter. |
| Long UTF-8 content | Proves truncation is rune-safe and does not split multibyte sequences. |
| Mention attack content | Proves active mention surfaces are neutralized. |
| Canonical digest view | Locks digest-capable adapter rendering. |
| Unknown digest view | Locks fallback rendering and truncation behavior. |

The golden updater should be opt-in through an environment variable such as
`ATTUNE_UPDATE_GOLDEN=1`, following the usual Go snapshot pattern. Normal test
runs should fail on drift.

### 5. Redaction harness

Every adapter conformance run should build a request with:

- a token-in-path URL where applicable
- a separate `Secret`
- a customer feedback title/content containing a unique marker
- a provider response body containing a unique marker

The harness captures logs produced by the adapter's `Build` and `Check` paths
and asserts that no log contains:

- the full webhook URL path token
- the secret value
- an `Authorization` header value
- the unique sensitive feedback marker
- provider response body text beyond the adapter's documented truncation policy

This catches the known Lark URL log and GitHub issue-body log. It also prevents
future adapters from reintroducing the same leak.

### 6. Mention-safety harness

Adapters that render into active mention surfaces must prove that user-supplied
text cannot trigger a channel mention.

Required attack strings:

```text
@channel
@here
@everyone
<@U123456>
<!here>
<!everyone>
<at id=all></at>
@octocat
@org/team
```

Channel-specific assertions:

| Adapter | Assertion |
|---|---|
| Slack | Mrkdwn control tokens are escaped or neutralized in user-controlled text. |
| Discord | `allowed_mentions.parse` is empty and user-controlled text is not placed in a mention-enabled top-level content field. |
| Lark | User-controlled text does not produce `at` elements or active all-hands mention tags. |
| GitHub Issue | User-controlled `@` mention tokens are neutralized in issue body text. |
| Raw webhook | No assertion; raw webhook preserves the customer envelope by design. |

GitHub mention safety is the easy place to under-test. The default decision is
to neutralize user-controlled `@` tokens in fields rendered into issue title or
body. If exact text preservation is later judged more important than notification
safety, that reversal should be a separate product/security decision.

### 7. Response-classification conformance

Add table-driven response suites and require each adapter to opt into the
smallest matching profile:

```go
outboundtest.GenericWebhookResponses()
outboundtest.GitHubIssueResponses()
outboundtest.ChatWebhookResponses()
outboundtest.LarkWebhookResponses()
```

Each response case checks:

- whether the checker returns nil
- whether the error is terminal via `errors.Is(err, outbound.ErrTerminal)`
- whether the error is retryable by exclusion from terminal
- whether provider body text is truncated before entering the error
- whether a provider-specific body code is parsed and classified correctly

The outbox bridge that translates `outbound.ErrTerminal` to `notify.ErrTerminal`
already exists. Add focused service tests proving:

- a terminal adapter verdict lands as a dead row, not a retryable row
- a retryable adapter verdict remains retryable
- `notify.DeliveryError.Kind` preserves `http_4xx` / `http_5xx` status
  precedence when a checker wraps `outbound.ErrTerminal`

### 8. Static gate for new adapters

The shared runner proves behavior only when an adapter calls it. Add a small
repository script:

```text
scripts/lint-outbound-conformance.sh
```

The script should:

- list every `internal/outbound/adapter/*` package with non-test Go files
- require `conformance_test.go`
- require an import of `internal/outbound/outboundtest`
- require at least one `outboundtest.TestEventChannel` or
  `outboundtest.TestDigestChannel` call

Wire the script into `scripts/check.sh` and CI's Go checks. This is the part that
makes the acceptance criterion "new adapters cannot skip redaction or retry
classification tests" real.

### 9. Documentation

Update [`internal/outbound/README.md`](../../../../internal/outbound/README.md)
with:

- the adapter contract
- the conformance runner API
- how to add event and digest fixtures
- how to update golden snapshots intentionally
- the response-classification profiles
- the redaction rule for URL-as-credential destinations
- the mention-safety rule for chat and issue surfaces
- the CI gate that requires `conformance_test.go`
- a current conformance matrix listing each adapter, supported profiles, fixture
  files, and response profile

The README should include a short adapter checklist:

1. Implement `EventChannel` and/or `DigestChannel`.
2. Self-register in `init`.
3. Add the `cmd/attune` blank import.
4. Add destination type constants, migrations, config, and routing where needed.
5. Add `conformance_test.go`.
6. Add request snapshots.
7. Run `go test ./internal/outbound/...` and the lint script.

## Alternatives considered

### Keep adapter-local tests only

Rejected. The current approach has high line coverage but no shared safety
contract. It cannot guarantee that every adapter tests redaction, mention safety,
or retry classification in the same way.

### Add a root test that imports every adapter

Rejected as the primary enforcement mechanism. A root assembly test is useful
for registry wiring, but it cannot instantiate package-private adapter values or
inspect adapter-local fixtures cleanly without weakening encapsulation. The
better shape is adapter-local conformance tests plus a static gate requiring
their presence.

### Make conformance a production registry field

Rejected. Conformance metadata belongs in tests and docs. Production code should
not carry test-only manifests just to prove test coverage.

### Redesign `ResponseChecker` to include headers

Rejected for this issue. Header-aware retry timing may be valuable, especially
for `Retry-After`, but the issue's acceptance criteria can be satisfied with the
current status/body checker and existing outbox backoff. A signature change would
touch transport callers and increase blast radius.

### Use live provider contract tests

Rejected for CI. Live Slack, Discord, Lark, and GitHub credentials would add
secret management, flake risk, rate limits, and provider cost. Local or scheduled
manual smoke tests can exist, but PR CI should use deterministic `httptest`
fixtures and request snapshots.

## Risks / tradeoffs

- Golden snapshots can become noisy if dynamic fields are not normalized
  carefully. Mitigation: normalize signatures, timestamps, authorization values,
  and delivery IDs before comparison.
- Mention safety may change visible text in GitHub issues or chat cards.
  Mitigation: constrain neutralization to user-controlled fields and document
  the behavior in the adapter contract.
- The static gate is source-based. It proves that an adapter calls the shared
  runner, not that every possible branch is covered. Mitigation: keep adapter
  local tests for provider-specific edge cases and use coverage as a secondary
  signal.
- Redaction boundaries can be confused with product display boundaries.
  Mitigation: explicitly define this issue's boundary as adapter logs, delivery
  errors, audit snapshots, and operator delivery surfaces. NotifyTarget list API
  behavior remains a separate contract decision.
- The suite can initially expose real defects. Mitigation: fix the defects in
  the same PR as the suite so CI lands green and the contract starts enforced.

## Implementation plan

1. Add `internal/outbound/outboundtest` with canonical fixtures, log capture,
   golden comparison, and response case helpers.
2. Add adapter-local `conformance_test.go` files for generic, GitHub Issue,
   Slack, Discord, and Lark.
3. Add adapter-local `testdata` snapshots for stable event and digest requests.
4. Fix adapter failures exposed by the suite:
   - redact Lark webhook URLs before logging
   - remove or sanitize GitHub issue body logging
   - neutralize GitHub mention tokens in user-controlled issue fields
   - change Lark malformed or missing provider JSON on HTTP 200 from silent
     success to terminal provider-contract failure
   - parse GitHub 403 bodies so only rate-limit 403 responses are retryable
   - make GitHub Issue and Lark read the nested `feedback.enriched` outbox
     shape as well as the flatter TestSend shape
5. Add `scripts/lint-outbound-conformance.sh` and wire it into `scripts/check.sh`
   and CI.
6. Update `internal/outbound/README.md`.
7. Add a `CHANGELOG.md` entry under `### Fixed` or `### Changed`, depending on
   the final behavior changes.
8. Run the verification matrix and cite the output in the PR.

## Verification

Required before marking this proposal `Implemented`:

```sh
scripts/lint-outbound-conformance.sh
go test ./internal/outbound/... ./internal/notify/... ./internal/service/outbox/... ./internal/service/digest/...
go test -race ./internal/outbound/... ./internal/notify/... ./internal/service/outbox/...
go test -cover ./internal/outbound/...
make ci-check
```

Expected coverage behavior:

- all five adapters have a conformance test
- all event-capable adapters pass response classification cases
- all digest-capable adapters pass digest rendering and redaction cases
- request snapshot drift fails CI unless intentionally updated
- URL-as-credential tokens do not appear in captured logs or delivery errors
- GitHub 403 forbidden responses are terminal while GitHub rate-limit 403
  responses are retryable
- Lark HTTP 200 responses without parseable `StatusCode:0` are not silent
  successes
- mention attack strings do not become active mentions on Slack, Discord, Lark,
  or GitHub Issue

Frontend tests are not required unless the implementation intentionally changes
Console notify-target list rendering or API behavior.

## References

- [Issue #167](https://github.com/Phixsura/attune/issues/167)
- [`internal/outbound/README.md`](../../../../internal/outbound/README.md)
- [`internal/inbound/inboundtest/contract.go`](../../../../internal/inbound/inboundtest/contract.go)
- [Gateway API conformance](https://gateway-api.sigs.k8s.io/docs/concepts/conformance/)
- [Gateway API GEP-917: Conformance Testing](https://gateway-api.sigs.k8s.io/geps/gep-917/)
- [Gateway API GEP-1709: Conformance Profiles](https://gateway-api.sigs.k8s.io/geps/gep-1709/)
- [Kubernetes CSI sanity tests](https://kubernetes-csi.github.io/docs/unit-testing.html)
- [Smithy HTTP protocol compliance tests](https://smithy.io/2.0/additional-specs/http-protocol-compliance-tests.html)
- [Terraform provider acceptance tests](https://developer.hashicorp.com/terraform/plugin/framework/acctests)
- [Pact provider verification](https://docs.pact.io/implementation_guides/jvm/provider)
- [Spring Cloud Contract](https://spring.io/projects/spring-cloud-contract)
- [OpenTelemetry Collector components](https://opentelemetry.io/docs/concepts/components/)
- [Prometheus Alertmanager](https://prometheus.io/docs/alerting/latest/alertmanager/)
- [Apprise documentation](https://appriseit.com/)
- [shoutrrr service URL schema](https://containrrr.dev/shoutrrr/v0.8/)
