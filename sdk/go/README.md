# attune Go SDK

[![Go Reference](https://pkg.go.dev/badge/github.com/Phixsura/attune/sdk/go.svg)](https://pkg.go.dev/github.com/Phixsura/attune/sdk/go)

The official Go client for the [attune](https://github.com/Phixsura/attune)
ingest API. Submit feedback from a Go service without hand-rolling HTTP, retries,
or idempotency. The request/response types are **generated from the proto
contract** (`proto/attune/v1`) and marshaled with `protojson`, so the wire shape
is single-sourced from proto, never hand-maintained.

## Install

```bash
go get github.com/Phixsura/attune/sdk/go@latest
```

The package is `attune`, so `import "github.com/Phixsura/attune/sdk/go"`
resolves to `attune.New(…)` with **no import alias required** (the examples below
spell out `attune "…/sdk/go"` only for clarity, since the path's last segment is
`go`). The `…/sdk/go` layout follows the monorepo SDK convention used by, e.g.,
the Azure SDK for Go (`github.com/Azure/azure-sdk-for-go/sdk/azcore`), with
per-module `sdk/go/vX.Y.Z` release tags.

Minimum Go version: **1.25** (the proto-generated types pull
`google.golang.org/protobuf` and `google.golang.org/genproto`, which require it).

## Quickstart

```go
package main

import (
	"context"
	"fmt"
	"log"

	attune "github.com/Phixsura/attune/sdk/go"
)

func main() {
	client, err := attune.New("https://attune.example.com", "att_sk_…")
	if err != nil {
		log.Fatal(err)
	}

	res, err := client.Ingest(context.Background(), attune.IngestInput{
		Content:    "the export button is broken",
		Source:     "web",
		SourceUser: "user-42",
		SourceMeta: map[string]any{"plan": "pro"},
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("stored feedback id:", res.ID)
}
```

The API key needs the `ingest:write` scope. A `*Client` is safe for concurrent
use — create one and share it across goroutines.

## Request & response

`Ingest(ctx, IngestInput, ...RequestOption) (IngestResult, error)` — `ctx` is
always first; cancel it or give it a deadline to abort in flight.

`IngestInput`:

| Field | Type | Required | Notes |
|---|---|---|---|
| `Content` | `string` | **yes** | the feedback text (server caps at 5000 chars) |
| `Source` | `string` | no | channel, e.g. `"web"`; server defaults to `"api"` |
| `SourceUser` | `string` | no | opaque end-user identifier |
| `SourceMeta` | `map[string]any` | no | arbitrary JSON metadata |
| `PageURL` | `string` | no | originating page URL for an in-app widget |

`IngestResult`:

| Field | Type | Notes |
|---|---|---|
| `ID` | `string` | stored feedback row id (the proto `int64`, rendered as a string) |
| `EnrichmentStatus` | `string` | enrichment lifecycle state; `"pending"` at ingest |

## Errors

Non-success outcomes return `*attune.AttuneError`. Switch on its stable `Code` —
never the human-facing `Message`:

```go
res, err := client.Ingest(ctx, attune.IngestInput{Content: "…"})
if err != nil {
	var ae *attune.AttuneError
	if errors.As(err, &ae) {
		switch ae.Code {
		case attune.CodeRateLimited:
			// back off and retry later (the SDK already retried per its policy)
		case attune.CodeUnauthorized, attune.CodeForbidden:
			// fix the API key / scope
		default:
			log.Printf("ingest failed: %s (status %d, request %s)", ae.Code, ae.Status, ae.RequestID)
		}
	}
	return err
}
```

`AttuneError` fields: `Code`, `Message`, `Status` (HTTP status; `0` for transport
errors), `RequestID`, `Headers` (response headers, `nil` for transport errors).

| Code constant | When |
|---|---|
| `CodeBadRequest` / `CodeValidation` | malformed body / invalid field |
| `CodeUnauthorized` / `CodeForbidden` | missing/invalid key, or missing `ingest:write` scope |
| `CodeConflict` / `CodeIdempotencyConflict` | same `Idempotency-Key`, different body |
| `CodeBodyTooLarge` | request body over the server limit |
| `CodeRateLimited` | per-key/per-tenant rate limit (includes `Retry-After`) |
| `CodeInternal` | server 5xx, or an undecodable response |
| `CodeNetwork` / `CodeTimeout` / `CodeAborted` | transport failures (no HTTP response) |

## Behavior

- **Retries.** Transient failures (HTTP 408, 429, any 5xx, and network/timeout
  errors) are retried with bounded exponential backoff (200 ms → 2 s) and ±25 %
  jitter, honoring a `Retry-After` header when present. Default 2 retries; tune
  with `attune.WithMaxRetries(n)`. Deterministic 4xx (400/401/403/409/413/422)
  are never retried, and a cancelled `ctx` is never retried.
- **Idempotency.** Every `Ingest` call carries an auto-generated `Idempotency-Key`,
  stable across that call's retries, so a blind retry is deduplicated server-side
  instead of inserting a duplicate. Override it per call with
  `attune.WithIdempotencyKey("…")` (8–64 chars of `[A-Za-z0-9_-]`). Key
  generation never fails.
- **Security.** The client never follows 3xx redirects (so the `X-API-Key`
  header can't leak to a redirect target), rejects CR/LF in the API key and
  idempotency key, and reads the response body under a 1 MiB cap.
- **Concurrency.** `*Client` is immutable after `New` and safe to share across
  goroutines.

## Options

Construction (`attune.New(baseURL, apiKey, opts...)`):

| Option | Default | Purpose |
|---|---|---|
| `WithHTTPClient(*http.Client)` | internal client | bring your own transport (e.g. `otelhttp`) |
| `WithMaxRetries(int)` | `2` | max retries for transient failures (negative → 0) |
| `WithTimeout(time.Duration)` | `30s` | per-attempt request timeout (`≤0` disables) |
| `WithUserAgentSuffix(string)` | — | append a token to the `User-Agent` |
| `WithDefaultHeaders(map[string]string)` | — | extra headers on every request (reserved headers always win) |

Per call (`c.Ingest(ctx, in, opts...)`):

| Option | Purpose |
|---|---|
| `WithIdempotencyKey(string)` | override the auto-generated idempotency key |

## Public surface

Beyond `Client` / `New` / `Ingest` / `AttuneError`, the package re-exports the
proto-generated wire types and the retry policy (mirroring `@phixsura/attune`):

- **Wire types** — `IngestRequest`, `IngestResponse`, `ErrorResponse`, `ErrorCode`
  (aliases of `github.com/Phixsura/attune/sdk/go/attune/v1`, the generated
  package; import it directly for the full `ErrorCode` enum values).
- **Error codes** — the `Code*` string constants (= the wire values) and the
  grouped `TransportErrorCode{Network, Timeout, Aborted}`.
- **Retry policy** — `IsRetryable(status)`, `BackoffDelay(attempt)`,
  `ParseRetryAfter(headers, now)`, for callers building their own loop.

## Example CLI

[`examples/ingest-cli`](examples/ingest-cli) is a small, real CLI built on the
SDK:

```bash
export ATTUNE_BASE_URL=https://attune.example.com
export ATTUNE_API_KEY=att_sk_…
go run ./examples/ingest-cli -content "the dashboard is slow" -source web
# or pipe content on stdin:
echo "the dashboard is slow" | go run ./examples/ingest-cli
```

## Development

```bash
go test -race ./...     # unit tests
./scripts/e2e.sh        # full e2e against a real server + Postgres (needs docker)
```

Wire types are generated from `proto/attune/v1` by `make proto` at the repo root
(do not edit `attune/v1/*.pb.go` by hand); the `proto-sync` CI gate enforces it.

## License

Apache-2.0. See [LICENSE](LICENSE).
