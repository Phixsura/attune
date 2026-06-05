# Proposal - instrument llmclient outbound HTTP transport

| | |
|---|---|
| **Issue** | #55 |
| **Status** | Implemented |
| **Started** | 2026-06-05 15:27 WAT |
| **Related** | #4 (`lint-slog.sh` rule-3), CLAUDE.md section 7 |

## Problem

`internal/infra/llmclient/openai_backend.go` builds an `http.Client` for
OpenAI-compatible `/v1/chat/completions` calls without an explicit
`otelhttp.Transport`. The request still works, but outbound LLM calls do not
emit client-side OpenTelemetry spans, so traces lose the downstream LLM node.

This is the exact rule-3 class documented by `scripts/lint-slog.sh` and
CLAUDE.md section 7.

## Goals

- Wrap the LLM client's outbound transport with `otelhttp.NewTransport`.
- Preserve the existing 60s timeout and standard `http.DefaultTransport`
  behavior for proxy, TLS, pooling, and environment handling.
- Add a package-level regression test so the wrapper is not only enforced by the
  shell linter.
- Keep the change local to the OpenAI-compatible backend.

## Non-goals

- Changing request/response logging, body truncation, or auth header behavior.
- Introducing a custom transport stack or new dependency.
- Making `scripts/lint-slog.sh` strict in CI; this issue only clears this
  package's rule-3 finding.

## Proposal

Construct the backend client as:

```go
&http.Client{
	Transport: otelhttp.NewTransport(http.DefaultTransport),
	Timeout:   openaiHTTPTimeout,
}
```

Using `http.DefaultTransport` under the OTel wrapper keeps Go's normal outbound
HTTP behavior intact while adding span creation around each LLM request. The
existing `Chat` path continues to build requests with the caller's context, so
those spans can attach to the active request trace.

Add a focused unit test that asserts `NewOpenAI` installs an
`*otelhttp.Transport`.

## Alternatives considered

- **Leave the default transport implicit** - simplest, but it is the current
  bug: no outbound spans and rule-3 keeps warning.
- **Use a custom `http.Transport`** - unnecessary for this issue and risks
  changing connection pooling, proxy, or TLS behavior. Wrapping
  `http.DefaultTransport` is the smallest observability-only change.
- **Rely only on `scripts/lint-slog.sh`** - catches this specific syntax, but a
  unit test gives the llmclient package a direct behavior guard.

## Risks / tradeoffs

- `otelhttp.NewTransport` adds tracing middleware around every LLM request.
  That is intended here, but it means LLM calls participate in trace sampling
  and exporter behavior configured by the process.
- The test checks the concrete transport type. That is acceptable because the
  project policy explicitly requires this wrapper; if the policy changes, the
  test should change with it.

## Implementation plan

1. Import `go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp` in the
   OpenAI backend.
2. Wrap `http.DefaultTransport` when constructing the backend `http.Client`.
3. Add a regression test for the transport wrapper.
4. Verify with the issue's requested commands.
5. Changelog is skipped because this is a `chore` PR under CLAUDE.md section 2.

## Verification

- `bash scripts/lint-slog.sh`
- `go test ./internal/infra/llmclient/...`

## References

- #55
- `scripts/lint-slog.sh` rule-3
- CLAUDE.md section 7 outbound HTTP client convention
