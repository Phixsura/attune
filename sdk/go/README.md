# attune Go SDK

The official Go client for the [attune](https://github.com/Phixsura/attune)
ingest API. Submit feedback from a Go service without hand-rolling HTTP, retries,
or idempotency. The request/response types are **generated from the proto
contract** (`proto/attune/v1`) and marshaled with `protojson`, so the wire shape
is single-sourced from proto, never hand-maintained.

## Install

This is a nested module, versioned with `sdk/go/vX.Y.Z` tags. `go get` resolves
the tag from the version:

```bash
go get github.com/Phixsura/attune/sdk/go@v0.1.0
```

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

The API key needs the `ingest:write` scope.

## Behavior

- **Retries.** Transient failures (HTTP 408, 429, any 5xx, and network/timeout
  errors) are retried with bounded exponential backoff (200 ms → 2 s) and ±25 %
  jitter, honoring a `Retry-After` header when present. Default 2 retries; tune
  with `attune.WithMaxRetries(n)`. Deterministic 4xx (400/401/403/409/413/422)
  are never retried.
- **Idempotency.** Every `Ingest` call carries an auto-generated `Idempotency-Key`,
  stable across that call's retries, so a blind retry is deduplicated server-side
  instead of inserting a duplicate. Override it per call with
  `attune.WithIdempotencyKey("…")` (8–64 chars of `[A-Za-z0-9_-]`).
- **Errors.** Non-success outcomes return `*attune.AttuneError`. Switch on its
  stable `Code` field — never the human-facing `Message`:

  ```go
  var ae *attune.AttuneError
  if errors.As(err, &ae) && ae.Code == attune.CodeRateLimited {
      // back off and retry later
  }
  ```

  Transport failures use `CodeNetwork`, `CodeTimeout`, `CodeAborted`.
- **Security.** The client never follows 3xx redirects (so the `X-API-Key`
  header can't leak to a redirect target), rejects CR/LF in the API key and
  idempotency key, and reads the response body under a 1 MiB cap.

## Public surface

Beyond `Client` / `New` / `Ingest` / `AttuneError`, the package re-exports the
proto-generated wire types and the retry policy (mirroring `@phixsura/attune`):

- **Wire types** — `IngestRequest`, `IngestResponse`, `ErrorResponse`, `ErrorCode`
  (aliases of `github.com/Phixsura/attune/sdk/go/attune/v1`, the generated
  package; import it directly for the full `ErrorCode` enum values).
- **Error codes** — `Code*` string constants (= the wire values) and the grouped
  `TransportErrorCode{Network,Timeout,Aborted}`.
- **Retry policy** — `IsRetryable(status)`, `BackoffDelay(attempt)`,
  `ParseRetryAfter(headers, now)` for callers building their own loop.

## Options

Construction (`attune.New(baseURL, apiKey, opts...)`):

| Option | Default | Purpose |
|---|---|---|
| `WithHTTPClient(*http.Client)` | internal client | bring your own transport (e.g. `otelhttp`) |
| `WithMaxRetries(int)` | `2` | max retries for transient failures |
| `WithTimeout(time.Duration)` | `30s` | per-attempt request timeout |
| `WithUserAgentSuffix(string)` | — | append a token to the `User-Agent` |
| `WithDefaultHeaders(map[string]string)` | — | extra headers on every request (reserved headers always win) |

Per call (`c.Ingest(ctx, in, opts...)`):

| Option | Purpose |
|---|---|
| `WithIdempotencyKey(string)` | override the auto-generated idempotency key |

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

## License

Apache-2.0. See [LICENSE](LICENSE).
