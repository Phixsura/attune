// SPDX-License-Identifier: Apache-2.0

// Package inbound is attune's channel-agnostic ingest framework.
//
// "Inbound" covers ANY event landing in attune's normalized ingest path —
// regardless of who initiates the TCP connection: webhooks are remote-
// initiated (push), IMAP/RSS pollers are attune-initiated (pull), MQ
// subscribers stream, schedulers crawl on a cron. All four modes
// implement the same Adapter interface.
//
// Hard rule: no package under internal/service|handlers|repo|infra|notify
// may import internal/inbound/adapter/*. cmd/attune is the only legal
// blank-import site. Enforced by golangci-lint depguard (see
// docs/proposals/2026/06/2026-06-08-channel-agnostic-inbound.md §CI boundary
// guard).
package inbound

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/domain"
)

// Adapter — every channel implements this.
//
// Mode mapping:
//
//	push:     Start mounts deps.Mux routes; Shutdown is a no-op.
//	poll:     Start spawns a ticker goroutine; Shutdown cancels & waits.
//	schedule: Same as poll with a cron expression instead of a fixed tick.
//	stream:   Start opens a subscription + spawns a reader goroutine;
//	          Shutdown closes the subscription & waits.
//
// Start MUST return quickly (OTel Component contract). Long-running work
// uses context.WithCancel(context.Background()) stored on the receiver;
// Shutdown cancels it and waits on a sync.WaitGroup.
type Adapter interface {
	Channel() string
	Start(ctx context.Context, deps Deps) error
	Shutdown(ctx context.Context) error
}

// ShutdownTimeouter — optional role (Caddy-style). If an Adapter
// implements it, the Manager honours the per-adapter deadline instead of
// DefaultShutdownTimeout. IMAP/MQ/stream adapters typically declare > 5s;
// webhook adapters that need immediate return implement this and return 0.
type ShutdownTimeouter interface {
	ShutdownTimeout() time.Duration
}

// Mux — narrow router-agnostic surface the framework hands to push
// adapters. Deliberately not chi.Router so the framework boundary stays
// decoupled from chi. cmd/attune passes a chiMux that satisfies this;
// in tests inboundtest supplies a FakeMux struct.
type Mux interface {
	Method(method, pattern string, h http.Handler)
}

// IngestPort — adapters call this to reach the core. Signature mirrors
// service.Ingestor.IngestRow so cmd/attune wiring is a trivial shim and
// there is no parallel ingest code path. keyID is uuid.Nil for inbound-
// adapter-sourced rows; the originating inbound_sources.id flows through
// in.SourceMeta["inbound_source_id"].
type IngestPort interface {
	Ingest(
		ctx context.Context,
		tenantID string,
		keyID uuid.UUID,
		in domain.IngestInput,
	) (feedbackID int64, err error)
}

// IngestFunc — convenience wrapper so a bare function can satisfy
// IngestPort (used by cmd/attune for the 3-line shim from
// service.Ingestor.IngestRow).
type IngestFunc func(ctx context.Context, tenantID string, keyID uuid.UUID, in domain.IngestInput) (int64, error)

// Ingest satisfies IngestPort.
func (f IngestFunc) Ingest(ctx context.Context, tenantID string, keyID uuid.UUID, in domain.IngestInput) (int64, error) {
	return f(ctx, tenantID, keyID, in)
}

// Deps — handed to every adapter at Start.
// Add fields here only when a dependency becomes universal across
// adapters; adapter-specific config comes from inbound_sources.config
// (encrypted JSON).
type Deps struct {
	Mux     Mux
	Ingest  IngestPort
	Sources SourceStore
	Secrets SecretStore
	Metrics InboundMetrics
	Logger  Logger
}
