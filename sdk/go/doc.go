// Package attune is the official Go client for the attune ingest API.
//
// It wraps the single public ingest endpoint (POST /v1/feedback/ingest) so a Go
// service can submit feedback without hand-rolling HTTP, retries, or idempotency
// handling. The request/response wire types are generated from the proto
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
// Errors from the API surface as [*AttuneError] with a stable Code field; switch
// on Code (never on the human-facing Message). Transport failures use the codes
// [CodeNetwork], [CodeTimeout], and [CodeAborted].
//
// # Installation
//
// This is a nested module (minimum Go 1.25) released with sdk/go/vX.Y.Z tags:
//
//	go get github.com/Phixsura/attune/sdk/go@vX.Y.Z
package attune
