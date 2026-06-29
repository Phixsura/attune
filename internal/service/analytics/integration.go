// SPDX-License-Identifier: Apache-2.0

// Package analytics provides integration with analytics platforms
// (Segment, Amplitude, Mixpanel). It transforms feedback events into
// platform-specific track/identify calls.
package analytics

import "context"

// Event is a feedback event to send to an analytics platform.
type Event struct {
	Name       string
	UserID     string
	TenantID   string
	Properties map[string]any
}

// UserTraits are user-level properties for identify calls.
type UserTraits struct {
	UserID  string
	Email   string
	Name    string
	Company string
	Plan    string
}

// Platform is the interface analytics providers implement.
type Platform interface {
	Name() string
	Track(ctx context.Context, event Event) error
	Identify(ctx context.Context, traits UserTraits) error
}

// FeedbackToEvent converts a feedback enrichment result into an
// analytics event for tracking.
func FeedbackToEvent(feedbackID int64, tenantID, kind, severity, source string) Event {
	return Event{
		Name:     "feedback.enriched",
		TenantID: tenantID,
		Properties: map[string]any{
			"feedback_id": feedbackID,
			"kind":        kind,
			"severity":    severity,
			"source":      source,
		},
	}
}

// TrackAll sends a batch of events to the given platform.
// Errors are collected but do not stop processing.
func TrackAll(ctx context.Context, p Platform, events []Event) []error {
	var errs []error
	for _, e := range events {
		if err := p.Track(ctx, e); err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}
