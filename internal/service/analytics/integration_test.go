// SPDX-License-Identifier: Apache-2.0
// ptrext:file-allow test mock fixtures

package analytics

import (
	"context"
	"fmt"
	"testing"
)

type mockPlatform struct {
	tracked []Event
	failOn  string
}

func (m *mockPlatform) Name() string { return "mock" }

func (m *mockPlatform) Track(_ context.Context, event Event) error {
	if event.Name == m.failOn {
		return fmt.Errorf("track failed: %s", event.Name)
	}
	m.tracked = append(m.tracked, event)
	return nil
}

func (m *mockPlatform) Identify(_ context.Context, _ UserTraits) error {
	return nil
}

func TestFeedbackToEvent(t *testing.T) {
	e := FeedbackToEvent(42, "tenant-1", "bug", "high", "api")
	if e.Name != "feedback.enriched" {
		t.Errorf("name = %q, want feedback.enriched", e.Name)
	}
	if e.Properties["kind"] != "bug" {
		t.Error("missing kind property")
	}
	if e.Properties["feedback_id"] != int64(42) {
		t.Errorf("feedback_id = %v, want 42", e.Properties["feedback_id"])
	}
}

func TestTrackAll(t *testing.T) {
	p := &mockPlatform{}
	events := []Event{
		{Name: "a"},
		{Name: "b"},
	}
	errs := TrackAll(context.Background(), p, events)
	if len(errs) != 0 {
		t.Errorf("got %d errors, want 0", len(errs))
	}
	if len(p.tracked) != 2 {
		t.Errorf("tracked %d events, want 2", len(p.tracked))
	}
}

func TestTrackAll_PartialFailure(t *testing.T) {
	p := &mockPlatform{failOn: "fail-me"}
	events := []Event{
		{Name: "ok"},
		{Name: "fail-me"},
		{Name: "ok-too"},
	}
	errs := TrackAll(context.Background(), p, events)
	if len(errs) != 1 {
		t.Errorf("got %d errors, want 1", len(errs))
	}
	if len(p.tracked) != 2 {
		t.Errorf("tracked %d events, want 2 (skipping failed)", len(p.tracked))
	}
}
