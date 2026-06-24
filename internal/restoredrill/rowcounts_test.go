// SPDX-License-Identifier: Apache-2.0

package restoredrill

import (
	"strings"
	"testing"
)

func TestEvaluateRowCounts(t *testing.T) {
	cases := []struct {
		name     string
		restored map[string]int64
		baseline map[string]int64
		want     Status
		wantMsg  string
	}{
		{
			name:     "no baseline is skipped",
			restored: map[string]int64{"user_feedback": 10, "tenants": 2},
			baseline: nil,
			want:     StatusSkip,
		},
		{
			name:     "data loss fails loudly",
			restored: map[string]int64{"user_feedback": 0, "tenants": 2},
			baseline: map[string]int64{"user_feedback": 10, "tenants": 2},
			want:     StatusFail,
			wantMsg:  "data loss",
		},
		{
			name:     "within live baseline passes",
			restored: map[string]int64{"user_feedback": 8, "tenants": 2},
			baseline: map[string]int64{"user_feedback": 10, "tenants": 2},
			want:     StatusPass,
		},
		{
			name:     "exceeding live baseline warns",
			restored: map[string]int64{"user_feedback": 12, "tenants": 2},
			baseline: map[string]int64{"user_feedback": 10, "tenants": 2},
			want:     StatusWarn,
			wantMsg:  "exceeds",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := evaluateRowCounts(tc.restored, tc.baseline)
			if got.Status != tc.want {
				t.Fatalf("evaluateRowCounts(...).Status = %q, want %q\nmessage: %s",
					got.Status, tc.want, got.Message)
			}
			if got.Name != "row_counts" {
				t.Fatalf("Name = %q, want \"row_counts\"", got.Name)
			}
			if tc.wantMsg != "" && !strings.Contains(got.Message, tc.wantMsg) {
				t.Fatalf("message %q does not contain %q", got.Message, tc.wantMsg)
			}
		})
	}
}
