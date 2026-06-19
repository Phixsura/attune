package attune

import attunev1 "github.com/Phixsura/attune/sdk/go/attune/v1"

// Proto-generated wire types, re-exported from the root package for single-import
// convenience — mirroring the Node SDK, which likewise exposes its generated
// IngestRequest / IngestResponse / ErrorResponse / ErrorCode. These are aliases
// to the generated package github.com/Phixsura/attune/sdk/go/attune/v1, the
// single source of truth (DO NOT EDIT the generated output). The full ErrorCode
// enum values live in that package (e.g. attunev1.ErrorCode_VALIDATION); the
// human-friendly string equivalents are the Code* constants in this package.
type (
	// IngestRequest is the generated request message. [IngestInput] is the
	// ergonomic facade most callers use; this is the raw wire type.
	IngestRequest = attunev1.IngestRequest
	// IngestResponse is the generated response message. [Client.Ingest] returns
	// the ergonomic [IngestResult]; this is the raw wire type.
	IngestResponse = attunev1.IngestResponse
	// ErrorResponse is the generated error envelope ({code, message, requestId}).
	ErrorResponse = attunev1.ErrorResponse
	// ErrorCode is the generated error-code enum. Its values are the wire strings
	// the Code* constants mirror.
	ErrorCode = attunev1.ErrorCode
)

// TransportErrorCode groups the client-side transport error codes — the
// [AttuneError.Code] values used when no HTTP response was received — mirroring
// the Node SDK's TransportErrorCode export.
var TransportErrorCode = struct {
	Network string
	Timeout string
	Aborted string
}{
	Network: CodeNetwork,
	Timeout: CodeTimeout,
	Aborted: CodeAborted,
}
