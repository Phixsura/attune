// SPDX-License-Identifier: Apache-2.0

package restoredrill

import "testing"

func TestAggregate(t *testing.T) {
	cases := []struct {
		name string
		in   []Status
		want Status
	}{
		{"empty", nil, StatusSkip},
		{"all skipped", []Status{StatusSkip, StatusSkip}, StatusSkip},
		{"pass over skip", []Status{StatusSkip, StatusPass}, StatusPass},
		{"warn beats pass", []Status{StatusPass, StatusWarn, StatusPass}, StatusWarn},
		{"fail beats all", []Status{StatusPass, StatusWarn, StatusFail, StatusSkip}, StatusFail},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			results := make([]CheckResult, 0, len(tc.in))
			for _, s := range tc.in {
				results = append(results, CheckResult{Status: s})
			}
			if got := Aggregate(results); got != tc.want {
				t.Fatalf("Aggregate(%v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
