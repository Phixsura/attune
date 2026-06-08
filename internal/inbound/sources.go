// SPDX-License-Identifier: Apache-2.0

package inbound

import (
	"context"
	"time"
)

// SourceStore — adapters read their configured inbound_sources rows. The
// production implementation lives in internal/repo/inboundsource; tests
// supply in-memory fakes from internal/inbound/inboundtest.
type SourceStore interface {
	List(ctx context.Context, channel string) ([]Source, error)
	Get(ctx context.Context, id string) (Source, error)
	GetBySlugs(ctx context.Context, tenantSlug, channel, sourceSlug string) (Source, error)
	UpdateState(ctx context.Context, id string, state SourceState) error
	SetEnabled(ctx context.Context, id string, enabled bool, reason string) error
}

// Source — one row of inbound_sources, decrypted for the framework /
// adapter to consume. Config is still ciphertext; adapters call
// Secrets.Decrypt on demand to keep plaintext window minimal.
type Source struct {
	ID       string
	TenantID string
	Channel  string
	Name     string
	Slug     string
	Config   []byte
	Enabled  bool
	State    SourceState
}

// SourceState — denormalised columns the adapter writes back as it makes
// progress (last_event_at / last_uid for cursors, last_error for ops UI).
type SourceState struct {
	LastEventAt *time.Time
	LastError   string
	LastUID     int64
}
