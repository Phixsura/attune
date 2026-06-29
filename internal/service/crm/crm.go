// SPDX-License-Identifier: Apache-2.0

// Package crm implements a CRM integration abstraction for syncing
// feedback with Salesforce, HubSpot, and other CRM platforms.
// Each provider implements the Connector interface.
package crm

import "context"

// Contact represents a CRM contact record.
type Contact struct {
	ExternalID string
	Email      string
	Name       string
	Company    string
	Tier       string
	Revenue    float64
}

// FeedbackSync is a feedback item to push to a CRM.
type FeedbackSync struct {
	FeedbackID int64
	Content    string
	Kind       string
	Severity   string
	ContactID  string
}

// SyncResult describes the outcome of a CRM sync operation.
type SyncResult struct {
	ExternalID string
	Success    bool
	Error      string
}

// Connector is the interface each CRM provider implements.
type Connector interface {
	Name() string
	LookupContact(ctx context.Context, email string) (*Contact, error)
	SyncFeedback(ctx context.Context, item FeedbackSync) (SyncResult, error)
}

// SyncAll pushes a batch of feedback items to the given connector.
// Returns results in the same order as items.
func SyncAll(ctx context.Context, c Connector, items []FeedbackSync) []SyncResult {
	results := make([]SyncResult, len(items))
	for i, item := range items {
		result, err := c.SyncFeedback(ctx, item)
		if err != nil {
			results[i] = SyncResult{Error: err.Error()}
			continue
		}
		results[i] = result
	}
	return results
}
