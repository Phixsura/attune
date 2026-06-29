// SPDX-License-Identifier: Apache-2.0

package appstore

import "testing"

func TestRatingSeverity(t *testing.T) {
	tests := []struct {
		rating int
		want   string
	}{
		{1, "high"},
		{2, "high"},
		{3, "medium"},
		{4, "low"},
		{5, "low"},
	}
	for _, tt := range tests {
		got := ratingSeverity(tt.rating)
		if got != tt.want {
			t.Errorf("ratingSeverity(%d) = %q, want %q", tt.rating, got, tt.want)
		}
	}
}

func TestChannel(t *testing.T) {
	a := NewAdapter()
	if a.Channel() != channelName {
		t.Errorf("Channel() = %q, want %q", a.Channel(), channelName)
	}
}
