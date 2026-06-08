// Package domain holds the pure types shared across attune's layered
// architecture. Files in this package MUST NOT depend on any other
// attune/internal/* package and MUST NOT import pgx, net/http, gRPC,
// or any I/O — keeping the boundary clean prevents cycles when
// handlers, service, repo, and notify all need to talk about the
// same shape.
package domain

import (
	"fmt"
	"time"
)

// MaxContentLen caps how much raw text a single feedback row may hold.
// Beyond this size the ingest path rejects with 400.
const MaxContentLen = 5000

// Source enums — kept as plain strings so they round-trip cleanly
// through JSON and SQL without enum machinery.
//
// Sprint 1.2 (2026-05-17) added four Lark-native source enums so
// customers can pipe Lark Approval / Bitable / Helpdesk / Form items
// directly into attune via a no-code automation → POST /v1/feedback/ingest
// (no extra attune-side endpoint needed). See README.md for the
// per-source configuration snippets.
//
// (Pre-flat-labels: ValidKinds and ValidSeverities lived here too. The
// proposal 2026-06-07-flat-labels.md, #10 moved classification to a
// per-tenant Dimension taxonomy, so the axes no longer have a global
// vocabulary to validate against.)
var ValidSources = map[string]bool{
	"api":     true, // generic API client (default for /v1/feedback/ingest)
	"webhook": true, // generic inbound HTTP webhook (#66 Phase 1)
	"email":   true, // mailbox poller / inbound IMAP (#66 Phase 1)
	"web":     true, // in-app JS feedback widget
	"other":   true, // catch-all for misc integrations
}

// SourceDisplayName returns the human-facing label for a source enum.
// Used by dispatch envelopes (github-issue body, lark-card, raw-webhook
// envelope) so downstream readers see "Lark Bitable #recXXX" instead
// of the bare "lark-bitable" technical key. Falls back to the raw key
// when unknown — never returns empty, never panics on unseen sources.
//
// The display strings are English-canonical by design; localizing
// them per-tenant requires threading locale into the notify path and is
// tracked as a follow-up i18n enhancement.
func SourceDisplayName(source string) string {
	switch source {
	case "api":
		return "API client"
	case "webhook":
		return "Webhook"
	case "email":
		return "Email"
	case "web":
		return "Web Widget"
	case "other":
		return "Other"
	default:
		return source
	}
}

// IngestInput is the canonical shape every ingest path normalizes to
// before hitting the database. Handlers fill it, service.Ingestor
// validates and persists.
type IngestInput struct {
	Content    string         `json:"content"`
	Source     string         `json:"source"`
	SourceUser string         `json:"sourceUser,omitempty"`
	SourceMeta map[string]any `json:"sourceMeta,omitempty"`
	PageURL    string         `json:"pageUrl,omitempty"`
}

// Validate enforces server-side invariants on input. Returns nil on
// success; the error message is user-facing (mapped to 400).
func (in IngestInput) Validate() error {
	if in.Content == "" {
		return fmt.Errorf("content is required")
	}
	if len(in.Content) > MaxContentLen {
		return fmt.Errorf("content too long (max %d chars)", MaxContentLen)
	}
	if !ValidSources[in.Source] {
		return fmt.Errorf("invalid source %q", in.Source)
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
	Title     string         `json:"title"`
	Attrs     map[string]any `json:"attrs"`     // map<dim.Name, string|[]string>
	IsUrgent  bool           `json:"is_urgent"` // derived; never set by the LLM
	Rationale string         `json:"rationale"`
}

// Snapshot is the wire-format handed from service.Enricher to a
// Notifier after a row finishes classification. Copy-by-value so the
// notifier can outlive any DB transaction or HTTP request scope.
type Snapshot struct {
	ID        int64
	TenantID  string
	Content   string
	Source    string
	UserID    string
	Title     string
	Attrs     map[string]any // map<dim.Name, string|[]string>
	IsUrgent  bool           // routes to the radar channel when true
	Rationale string         // LLM's short explanation; surfaced in outbox envelopes
	// SubmittedAt is the user's original submission time
	// (user_feedback.created_at), independent of when the LLM finished
	// classifying. Surfaced in outbound envelopes so consumers doing
	// time-series ordering or SLA calculation see the real timeline,
	// not the enrichment-induced delay.
	SubmittedAt time.Time
	EnrichedAt  time.Time
}
