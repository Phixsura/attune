// SPDX-License-Identifier: Apache-2.0

package email

import "testing"

func TestParseAfterIngest(t *testing.T) {
	tests := []struct {
		raw    string
		want   afterIngestPolicy
		reason string
	}{
		{"", afterIngestPolicy{Kind: "mark_seen"}, "empty defaults to mark_seen"},
		{"mark_seen", afterIngestPolicy{Kind: "mark_seen"}, "explicit mark_seen"},
		{"keep_unseen", afterIngestPolicy{Kind: "keep_unseen"}, "explicit keep_unseen"},
		{"move_to:Processed", afterIngestPolicy{Kind: "move_to", Folder: "Processed"}, "move_to with folder"},
		{"unknown_policy", afterIngestPolicy{Kind: "mark_seen"}, "unknown falls back to safe default"},
		{"move_to:", afterIngestPolicy{Kind: "move_to", Folder: ""}, "move_to with empty folder is a config bug but parsed"},
	}
	for _, tt := range tests {
		t.Run(tt.reason, func(t *testing.T) {
			got := parseAfterIngest(tt.raw)
			if got != tt.want {
				t.Errorf("parseAfterIngest(%q) = %+v; want %+v", tt.raw, got, tt.want)
			}
		})
	}
}
