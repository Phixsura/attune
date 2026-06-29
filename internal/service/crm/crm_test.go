// SPDX-License-Identifier: Apache-2.0
// ptrext:file-allow test mock fixtures

package crm

import (
	"context"
	"fmt"
	"testing"
)

type mockConnector struct {
	failOn int64
}

func (m *mockConnector) Name() string { return "mock" }

func (m *mockConnector) LookupContact(_ context.Context, email string) (*Contact, error) {
	return &Contact{ExternalID: "ext-1", Email: email}, nil
}

func (m *mockConnector) SyncFeedback(_ context.Context, item FeedbackSync) (SyncResult, error) {
	if item.FeedbackID == m.failOn {
		return SyncResult{}, fmt.Errorf("sync failed for %d", item.FeedbackID)
	}
	return SyncResult{ExternalID: fmt.Sprintf("crm-%d", item.FeedbackID), Success: true}, nil
}

func TestSyncAll(t *testing.T) {
	c := &mockConnector{}
	items := []FeedbackSync{
		{FeedbackID: 1, Content: "bug report"},
		{FeedbackID: 2, Content: "feature request"},
	}
	results := SyncAll(context.Background(), c, items)
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
	if !results[0].Success {
		t.Error("first item should succeed")
	}
	if results[0].ExternalID != "crm-1" {
		t.Errorf("external id = %q, want crm-1", results[0].ExternalID)
	}
}

func TestSyncAll_PartialFailure(t *testing.T) {
	c := &mockConnector{failOn: 2}
	items := []FeedbackSync{
		{FeedbackID: 1},
		{FeedbackID: 2},
		{FeedbackID: 3},
	}
	results := SyncAll(context.Background(), c, items)
	if results[0].Error != "" {
		t.Error("item 1 should succeed")
	}
	if results[1].Error == "" {
		t.Error("item 2 should fail")
	}
	if results[2].Error != "" {
		t.Error("item 3 should succeed")
	}
}

func TestConnectorInterface(t *testing.T) {
	c := &mockConnector{}
	contact, err := c.LookupContact(context.Background(), "test@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if contact.Email != "test@example.com" {
		t.Errorf("email = %q, want test@example.com", contact.Email)
	}
}
