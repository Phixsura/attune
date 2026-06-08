// SPDX-License-Identifier: Apache-2.0

package email

import "strings"

// afterIngestPolicy describes what to do with the IMAP message after it
// has been turned into a feedback row. Default is mark_seen — the
// Helpdesk-style "this email was processed" convention; sub-second
// inboxes that want to keep visual cues use keep_unseen; ops teams
// that prefer archive-on-success use move_to:<folder>.
type afterIngestPolicy struct {
	Kind   string // "mark_seen" | "keep_unseen" | "move_to"
	Folder string // populated only when Kind == "move_to"
}

func parseAfterIngest(raw string) afterIngestPolicy {
	if raw == "" || raw == "mark_seen" {
		return afterIngestPolicy{Kind: "mark_seen"}
	}
	if raw == "keep_unseen" {
		return afterIngestPolicy{Kind: "keep_unseen"}
	}
	if strings.HasPrefix(raw, "move_to:") {
		return afterIngestPolicy{Kind: "move_to", Folder: strings.TrimPrefix(raw, "move_to:")}
	}
	// Unknown value — fall back to the safe default. Spec §Email
	// adapter is explicit that mark_seen is the documented default.
	return afterIngestPolicy{Kind: "mark_seen"}
}
