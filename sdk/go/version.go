package attune

// Version is the released version of this SDK. It is sent to the server as part
// of the User-Agent header (attune-go/<Version> go/<goversion>) so the server
// can attribute SDK traffic and stage per-SDK-version rollouts.
//
// A nested Go module is released with a tag of the form sdk/go/vX.Y.Z; this
// constant must match the version in that tag (the release workflow enforces
// it).
const Version = "0.1.0"
