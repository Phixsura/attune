// Package domain holds the pure types shared across attune's layered
// architecture. Files in this package MUST NOT depend on any other
// attune/internal/* package and MUST NOT import pgx, net/http, gRPC,
// or any I/O — keeping the boundary clean prevents cycles when
// handlers, service, repo, and notify all need to talk about the
// same shape.
package domain

import (
	"fmt"
	"maps"
	"sort"
	"strings"
	"time"
)

// MaxContentLen caps how much raw text a single feedback row may hold.
// Beyond this size the ingest path rejects with 400.
const MaxContentLen = 5000

// Source enums — kept as plain strings so they round-trip cleanly
// through JSON and SQL without enum machinery.
//
// The valid set is registry-driven (#95): inbound adapters self-declare their
// channel (webhook, email, …) and the union of CoreSources ∪ registered
// channels is assembled once at the composition root (cmd/attune) into a
// SourceSet and injected into the validators. domain stays pure — it owns the
// SourceSet type and the never-an-adapter CoreSources, but never imports the
// registry. See docs/proposals/2026/06/2026-06-23-registry-driven-source-set.md.
//
// A source string is a FROZEN storage + wire token: it is persisted in
// user_feedback.source (no DB CHECK) and emitted on the outbound envelope.
// The set is append-only — never rename in place, never repurpose, never
// hard-delete rows carrying a source. TestSourceVocabulary_AppendOnly gates this.
//
// coreSources are the sources with no inbound adapter and no Start/Shutdown
// lifecycle: api (the /v1/feedback/ingest default), web (in-app JS widget),
// mcp (the #93 MCP handler integration), other (catch-all). They carry their
// own display labels and are RESERVED — an adapter channel may never claim one.
//
// Unexported on purpose: a registry's backing store is never a public mutable
// global (no benchmarked Go registry ships one — database/sql, gob, gRPC
// resolver all keep theirs private). Read it via IsReservedSource (membership)
// or CoreSources (a defensive clone).
var coreSources = map[string]string{
	"api":    "API client",
	"web":    "Web Widget",
	"mcp":    "MCP",
	"other":  "Other",
	"portal": "Portal",
}

// CoreSources returns a copy of the reserved core source→label map for the
// composition root to seed the assembled SourceSet. A clone so a caller can
// never mutate the reserved tier.
func CoreSources() map[string]string { return maps.Clone(coreSources) }

// IsReservedSource reports whether a channel name is a core source an inbound
// adapter may never claim. One source of truth: a future core-source addition
// extends the reservation (and cmd/attune's boot-time collision guard)
// automatically, with no second hand-maintained list to drift.
func IsReservedSource(channel string) bool {
	_, ok := coreSources[channel]
	return ok
}

// SourceSet is the injectable, frozen union of valid sources with their display
// labels. It is the Vault BuiltinRegistry seam: an interface declared in the
// pure domain layer lets validators depend on registry-derived data without an
// import cycle (the concrete value is assembled outside domain and injected).
type SourceSet interface {
	Has(source string) bool       // membership — used by the ingest/guard validators
	Display(source string) string // human label; never empty, never panics
	All() []string                // sorted keys — for the "valid values" list in errors
}

// NewSourceSet freezes entries (source → display label) into an immutable
// SourceSet. The caller assembles the union (CoreSources ∪ registered channels);
// domain only stores and reads it. The sorted-keys slice is precomputed once
// here so All() is a zero-allocation O(1) read (the set is immutable, so the
// sort is genuinely once-per-set, not once-per-call).
func NewSourceSet(entries map[string]string) SourceSet {
	m := make(map[string]string, len(entries))
	for k, v := range entries {
		m[k] = v
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return sourceSet{m: m, keys: keys}
}

type sourceSet struct {
	m    map[string]string
	keys []string // precomputed sorted keys; All() returns it directly (immutable set)
}

func (s sourceSet) Has(source string) bool { _, ok := s.m[source]; return ok }

// Display returns the label for a source, falling back to the raw key for an
// unknown source so historical rows and queued envelopes carrying a retired
// token still render (never empty, never panics).
func (s sourceSet) Display(source string) string {
	if d, ok := s.m[source]; ok && d != "" {
		return d
	}
	return source
}

// All returns the sorted source keys. The slice is precomputed in NewSourceSet
// and never mutated; callers must not mutate it either (it is the caller's
// responsibility — like sql.Drivers, this is a documented invariant).
func (s sourceSet) All() []string { return s.keys }

// defaultSourceSet is the package-level singleton returned by DefaultSourceSet.
// Computed once at init: the per-call construction the previous version did was
// a real CPU regression on the renderer-fallback hot path (SourceDisplayName is
// called for every legacy in-flight outbox row missing source_display).
// Immutability is preserved by sourceSet's value-receiver methods + private
// fields — no caller can mutate it.
var defaultSourceSet = func() SourceSet {
	m := make(map[string]string, len(coreSources)+2)
	for k, v := range coreSources {
		m[k] = v
	}
	m["webhook"] = "Webhook"
	m["email"] = "Email"
	m["slack"] = "Slack"
	return NewSourceSet(m)
}()

// DefaultSourceSet reproduces the shipped source set for any caller without an
// injected one — pure-package tests and the SourceDisplayName fallback. It is
// CoreSources plus the two built-in inbound adapters' channels; the live set
// assembled in cmd/attune additionally picks up any future adapter's channel.
func DefaultSourceSet() SourceSet { return defaultSourceSet }

const (
	SourceMetaInboundSourceID   = "inbound_source_id"
	SourceMetaInboundSourceName = "inbound_source_name"
)

// SourceDisplayName returns the human-facing label for a source enum, via the
// DefaultSourceSet. It is the permanent pure read-path fallback the github-issue
// renderers use when an envelope carries no resolved source_display — historical
// rows and queued envelopes may carry a token no longer in the live set, so it
// must never hard-error: unknown sources fall back to the raw key.
//
// The display strings are English-canonical by design; per-tenant localization
// is a lookup keyed by the stable token at the envelope-build site (where the
// DisplayLocale is known), never a mutation of the source string itself.
func SourceDisplayName(source string) string {
	return DefaultSourceSet().Display(source)
}

// IngestInput is the canonical shape every ingest path normalizes to
// before hitting the database. Handlers fill it, service.Ingestor
// validates and persists.
type IngestInput struct {
	Content    string         `json:"content"`
	Source     string         `json:"source"`
	Type       string         `json:"type,omitempty"`
	SourceUser string         `json:"sourceUser,omitempty"`
	SourceMeta map[string]any `json:"sourceMeta,omitempty"`
	PageURL    string         `json:"pageUrl,omitempty"`
	// IdempotencyKey is set server-side from the optional `Idempotency-Key`
	// request header — never from the wire body (json:"-"). When non-empty,
	// service.Ingestor dedups repeated ingests so a client retry of an
	// at-least-once delivery cannot create a duplicate feedback row.
	IdempotencyKey string `json:"-"`
}

// Validate enforces server-side invariants on input. Returns nil on
// success; the error message is user-facing (mapped to 400).
//
// set is the injected SourceSet (assembled in cmd/attune from CoreSources ∪
// registered inbound channels). A nil set falls back to DefaultSourceSet so the
// method stays callable from pure-package tests with no wiring.
func (in IngestInput) Validate(set SourceSet) error {
	if set == nil {
		set = DefaultSourceSet()
	}
	if in.Content == "" {
		return fmt.Errorf("content is required")
	}
	if len(in.Content) > MaxContentLen {
		return fmt.Errorf("content too long (max %d chars)", MaxContentLen)
	}
	// PostgreSQL TEXT cannot store a NUL byte; reject it as a clean validation
	// error rather than letting it fail at INSERT time as an opaque DB error.
	if strings.ContainsRune(in.Content, '\x00') {
		return fmt.Errorf("content contains a null byte")
	}
	if !set.Has(in.Source) {
		return fmt.Errorf("invalid source %q (valid: %s)", in.Source, strings.Join(set.All(), ", "))
	}
	return nil
}

// Enriched is the AI-derived classification for one feedback row.
// Built by service.Enricher from the LLM response, then persisted via
// repo.MarkDone and fanned out via notify.
//
// Metadata-driven Dimensions model (proposal
// 2026-06-07-flat-labels.md, E3): the LLM emits one entry per
// configured Dimension keyed by Dimension.Name. Single-kind dims
// store `string`; multi-kind dims store `[]string`. `IsUrgent` is
// derived by the enricher (NOT the LLM): true iff any dim's
// configured UrgentSet intersects the value(s) the LLM picked. The
// boolean is persisted at write time so historical urgent/non-urgent
// status is a snapshot of the operator's policy at classification.
type Enriched struct {
	Title                    string         `json:"title"`
	DisplayTitle             string         `json:"display_title,omitempty"`
	Attrs                    map[string]any `json:"attrs"`     // map<dim.Name, string|[]string>
	IsUrgent                 bool           `json:"is_urgent"` // derived; never set by the LLM
	Rationale                string         `json:"rationale"`
	DisplayRationale         string         `json:"display_rationale,omitempty"`
	ClassificationConfidence *float64       `json:"classification_confidence,omitempty"`
}

// Snapshot is the wire-format handed from service.Enricher to a
// Notifier after a row finishes classification. Copy-by-value so the
// notifier can outlive any DB transaction or HTTP request scope.
type Snapshot struct {
	ID               int64
	TenantID         string
	Content          string
	Source           string
	UserID           string
	Language         string
	DisplayLocale    string
	Title            string
	DisplayTitle     string
	Attrs            map[string]any // map<dim.Name, string|[]string>
	IsUrgent         bool           // routes to the radar channel when true
	Rationale        string         // LLM's short explanation; surfaced in outbox envelopes
	DisplayRationale string
	// ClassificationConfidence is the model's self-rated review signal in
	// [0,1]. Nil means the model or prompt did not provide one.
	ClassificationConfidence *float64
	// SubmittedAt is the user's original submission time
	// (user_feedback.created_at), independent of when the LLM finished
	// classifying. Surfaced in outbound envelopes so consumers doing
	// time-series ordering or SLA calculation see the real timeline,
	// not the enrichment-induced delay.
	SubmittedAt time.Time
	EnrichedAt  time.Time
}
