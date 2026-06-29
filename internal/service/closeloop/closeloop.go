// SPDX-License-Identifier: Apache-2.0

// Package closeloop implements voter notification when a feature
// request they voted on changes status. When a roadmap item transitions
// (e.g. planned → in_progress → completed), voters receive a
// notification about the status change.
package closeloop

// StatusChange represents a roadmap item state transition.
type StatusChange struct {
	ItemID    string
	ItemTitle string
	OldStatus string
	NewStatus string
}

// Voter represents a user who voted on a feature request.
type Voter struct {
	ID    string
	Email string
	Name  string
}

// Notification is a pending voter notification to send.
type Notification struct {
	Voter  Voter
	Change StatusChange
}

// BuildNotifications creates one notification per voter for the given
// status change. Only transitions to specific terminal/milestone
// statuses trigger notifications.
func BuildNotifications(voters []Voter, change StatusChange) []Notification {
	if !shouldNotify(change.NewStatus) {
		return nil
	}
	out := make([]Notification, len(voters))
	for i, v := range voters {
		out[i] = Notification{Voter: v, Change: change}
	}
	return out
}

func shouldNotify(newStatus string) bool {
	switch newStatus {
	case "in_progress", "completed", "shipped":
		return true
	default:
		return false
	}
}
