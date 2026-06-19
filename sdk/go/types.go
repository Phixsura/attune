package attune

import (
	"strconv"

	attunev1 "github.com/Phixsura/attune/sdk/go/attune/v1"
	"google.golang.org/protobuf/types/known/structpb"
)

// IngestInput is a single feedback item to submit. Only Content is required. It
// is an ergonomic facade over the proto-generated [IngestRequest]: the SDK maps
// it to that generated message and marshals with protojson, so the wire contract
// is single-sourced from proto/attune/v1/ingest.proto, not hand-maintained here.
// SourceMeta is a plain map rather than the generated *structpb.Struct purely for
// ergonomics; callers wanting the raw wire type can use [IngestRequest] directly.
type IngestInput struct {
	// Content is the feedback text (required). The server caps it at 5000 chars.
	Content string
	// Source is the originating channel (e.g. "web", "email"). Defaults to "api".
	Source string
	// SourceUser is an opaque end-user identifier from the originating system.
	SourceUser string
	// SourceMeta is arbitrary source-specific metadata. Values must be
	// JSON-compatible (string, number, bool, nil, []any, map[string]any).
	SourceMeta map[string]any
	// PageURL is the originating page URL for an in-app web widget.
	PageURL string
}

// toProto maps the ergonomic input onto the generated wire message.
func (in IngestInput) toProto() (*attunev1.IngestRequest, error) {
	req := &attunev1.IngestRequest{
		Content:    in.Content,
		Source:     in.Source,
		SourceUser: in.SourceUser,
		PageUrl:    in.PageURL,
	}
	if len(in.SourceMeta) > 0 {
		sm, err := structpb.NewStruct(in.SourceMeta)
		if err != nil {
			return nil, err
		}
		req.SourceMeta = sm
	}
	return req, nil
}

// IngestResult is the server's response to a successful ingest.
type IngestResult struct {
	// ID is the stored feedback row id. The proto field is an int64; the server
	// renders it as a JSON string on the wire, and the SDK exposes it as a string.
	ID string
	// EnrichmentStatus is the enrichment lifecycle state; "pending" at ingest.
	EnrichmentStatus string
}

// resultFromProto maps the generated response message onto the public result.
func resultFromProto(r *attunev1.IngestResponse) IngestResult {
	return IngestResult{
		ID:               strconv.FormatInt(r.GetId(), 10),
		EnrichmentStatus: r.GetEnrichmentStatus(),
	}
}
