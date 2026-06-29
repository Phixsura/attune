// SPDX-License-Identifier: Apache-2.0

package closeloop

import "testing"

func TestBuildNotifications_Shipped(t *testing.T) {
	voters := []Voter{
		{ID: "u1", Email: "a@test.com", Name: "Alice"},
		{ID: "u2", Email: "b@test.com", Name: "Bob"},
	}
	change := StatusChange{
		ItemID: "feat-1", ItemTitle: "Dark mode",
		OldStatus: "in_progress", NewStatus: "shipped",
	}
	got := BuildNotifications(voters, change)
	if len(got) != 2 {
		t.Fatalf("got %d notifications, want 2", len(got))
	}
	if got[0].Change.ItemTitle != "Dark mode" {
		t.Error("notification should carry item title")
	}
}

func TestBuildNotifications_InProgress(t *testing.T) {
	voters := []Voter{{ID: "u1"}}
	change := StatusChange{NewStatus: "in_progress"}
	got := BuildNotifications(voters, change)
	if len(got) != 1 {
		t.Fatalf("in_progress should trigger notification, got %d", len(got))
	}
}

func TestBuildNotifications_Planned(t *testing.T) {
	voters := []Voter{{ID: "u1"}}
	change := StatusChange{NewStatus: "planned"}
	got := BuildNotifications(voters, change)
	if got != nil {
		t.Errorf("'planned' status should not trigger notification, got %d", len(got))
	}
}

func TestBuildNotifications_NoVoters(t *testing.T) {
	change := StatusChange{NewStatus: "shipped"}
	got := BuildNotifications(nil, change)
	if len(got) != 0 {
		t.Errorf("no voters should produce 0 notifications, got %d", len(got))
	}
}
