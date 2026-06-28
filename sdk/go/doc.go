// Package attune is the official Go client for the attune ingest and tenant
// management APIs.
//
// It wraps the public ingest endpoint (POST /v1/feedback/ingest) plus the
// scoped management surfaces under /v1/* so a Go service can submit feedback
// and automate tenant operations without hand-rolling HTTP, retries, or
// idempotency handling. The request/response wire types are generated from the proto
// contract (proto/attune/v1) and marshaled with protojson, so the client
// depends on google.golang.org/protobuf and its proto annotation modules.
//
// # Quickstart
//
//	c, err := attune.New("https://attune.example.com", "att_sk_...")
//	if err != nil {
//		log.Fatal(err)
//	}
//	res, err := c.Ingest(ctx, attune.IngestInput{Content: "the dashboard is slow"})
//	if err != nil {
//		log.Fatal(err)
//	}
//	fmt.Println(res.ID)
//
// # Behavior
//
// Ingest retries transient failures (HTTP 408, 429, any 5xx, and network or
// timeout errors) with bounded exponential backoff and jitter, honoring a
// Retry-After header when present. Every call carries a stable Idempotency-Key
// (generated per call, reused across that call's retries) so a blind retry is
// deduplicated server-side rather than inserting a duplicate row. Override the
// key with [WithIdempotencyKey].
//
// The client never follows 3xx redirects (so the X-API-Key header can't leak to
// a redirect target), rejects CR/LF in the key and idempotency key, and reads the
// response body under a 1 MiB cap. A *[Client] is immutable after [New] and safe
// for concurrent use across goroutines.
//
// # Errors
//
// Non-success outcomes return [*AttuneError]; switch on its stable Code (never
// the human-facing Message). Code is one of the server [CodeBadRequest] …
// [CodeInternal] constants, or a transport code ([CodeNetwork], [CodeTimeout],
// [CodeAborted]). AttuneError also carries Status, RequestID, and the response
// Headers (nil for transport errors).
//
// # Public surface
//
// The proto-generated wire types are re-exported for convenience —
// [IngestRequest], [IngestResponse], [ErrorResponse], [ErrorCode] — along with
// the retry policy ([IsRetryable], [BackoffDelay], [ParseRetryAfter]) and the
// grouped [TransportErrorCode]. The full ErrorCode enum lives in the generated
// package github.com/Phixsura/attune/sdk/go/attune/v1.
//
// # Installation
//
// This is a nested module (minimum Go 1.25) released with sdk/go/vX.Y.Z tags:
//
//	go get github.com/Phixsura/attune/sdk/go@vX.Y.Z
package attune
