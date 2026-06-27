// SPDX-License-Identifier: Apache-2.0

package restoredrill

import (
	"strings"
	"testing"
	"time"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func dptr(d time.Duration) *time.Duration { return ptrext.Of(d) }

// TestSecondsPtr_RoundsAndKeepsMeasured pins the RPO/RTO serialization fix: a
// measured duration is ROUNDED (not truncated) and never collapsed to nil, so
// the persisted integer matches the rounded human-readable message and a real
// sub-second restore is not reported as "never measured".
func TestSecondsPtr_RoundsAndKeepsMeasured(t *testing.T) {
	cases := []struct {
		name string
		in   *time.Duration
		want *int64
	}{
		{"not measured stays nil", nil, nil},
		{"rounds up", dptr(59*time.Second + 900*time.Millisecond), ptrext.Of(int64(60))},
		{"rounds down", dptr(2*time.Second + 400*time.Millisecond), ptrext.Of(int64(2))},
		{"sub-second measured is 0 not nil", dptr(300 * time.Millisecond), ptrext.Of(int64(0))},
		{"exact zero measured is 0 not nil", dptr(0), ptrext.Of(int64(0))},
		{"negative duration clamped to 0", dptr(-5 * time.Second), ptrext.Of(int64(0))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := secondsPtr(tc.in)
			switch {
			case tc.want == nil && got != nil:
				t.Fatalf("got %d, want nil", ptrext.Indirect(got))
			case tc.want != nil && got == nil:
				t.Fatalf("got nil, want %d", ptrext.Indirect(tc.want))
			case tc.want != nil && ptrext.Indirect(got) != ptrext.Indirect(tc.want):
				t.Fatalf("got %d, want %d", ptrext.Indirect(got), ptrext.Indirect(tc.want))
			}
		})
	}
}

// TestSecondsOf_TargetZeroIsNil: a 0 target means "no target" → nil; a positive
// target is rounded to whole seconds.
func TestSecondsOf_TargetZeroIsNil(t *testing.T) {
	if got := secondsOf(0); got != nil {
		t.Fatalf("zero target = %d, want nil", ptrext.Indirect(got))
	}
	if got := secondsOf(90*time.Second + 600*time.Millisecond); got == nil || ptrext.Indirect(got) != 91 {
		t.Fatalf("target round = %v, want 91", got)
	}
}

func TestRecoverySummary(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		rpo, rto *time.Duration
		want     string
	}{
		{"both nil", nil, nil, ""},
		{"RPO only", dptr(2 * time.Hour), nil, "RPO 2h0m0s"},
		{"RTO only", nil, dptr(3 * time.Minute), "RTO 3m0s"},
		{"both", dptr(1 * time.Hour), dptr(5 * time.Minute), "RPO 1h0m0s, RTO 5m0s"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := recoverySummary(tc.rpo, tc.rto)
			if got != tc.want {
				t.Fatalf("recoverySummary = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestHumanDur(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   time.Duration
		want string
	}{
		{0, "0s"},
		{30 * time.Second, "30s"},
		{5*time.Minute + 30*time.Second, "5m30s"},
		{2*time.Hour + 15*time.Minute, "2h15m0s"},
	}
	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			t.Parallel()
			got := humanDur(tc.in)
			if got != tc.want {
				t.Fatalf("humanDur(%v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestSecondsOf_NegativeIsNil(t *testing.T) {
	t.Parallel()
	if got := secondsOf(-5 * time.Second); got != nil {
		t.Fatalf("negative target = %v, want nil", got)
	}
}

func TestEvaluateRecoveryObjectives(t *testing.T) {
	cases := []struct {
		name      string
		rpo, rto  *time.Duration
		rpoTarget time.Duration
		rtoTarget time.Duration
		want      Status
		wantMsg   string
	}{
		{
			name: "nothing measured is skipped",
			want: StatusSkip,
		},
		{
			name: "within both targets passes",
			rpo:  dptr(2 * time.Hour), rto: dptr(3 * time.Minute),
			rpoTarget: 24 * time.Hour, rtoTarget: 30 * time.Minute,
			want: StatusPass,
		},
		{
			name:      "RPO breach warns",
			rpo:       dptr(48 * time.Hour),
			rpoTarget: 24 * time.Hour,
			want:      StatusWarn,
			wantMsg:   "RPO",
		},
		{
			name:      "RTO breach warns",
			rto:       dptr(2 * time.Hour),
			rtoTarget: 30 * time.Minute,
			want:      StatusWarn,
			wantMsg:   "RTO",
		},
		{
			name: "measured but no targets passes",
			rpo:  dptr(100 * time.Hour), rto: dptr(100 * time.Hour),
			want: StatusPass,
		},
		{
			name: "both breach warns with both mentioned",
			rpo:  dptr(48 * time.Hour), rto: dptr(2 * time.Hour),
			rpoTarget: 24 * time.Hour, rtoTarget: 30 * time.Minute,
			want:    StatusWarn,
			wantMsg: "RPO",
		},
		{
			name:      "only RPO measured passes without target",
			rpo:       dptr(5 * time.Hour),
			rpoTarget: 0,
			want:      StatusPass,
		},
		{
			name:      "only RTO measured passes without target",
			rto:       dptr(10 * time.Minute),
			rtoTarget: 0,
			want:      StatusPass,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := evaluateRecoveryObjectives(tc.rpo, tc.rto, tc.rpoTarget, tc.rtoTarget)
			if got.Status != tc.want {
				t.Fatalf("status = %q, want %q (msg: %s)", got.Status, tc.want, got.Message)
			}
			if got.Name != "recovery_objectives" {
				t.Fatalf("name = %q", got.Name)
			}
			if tc.wantMsg != "" && !strings.Contains(got.Message, tc.wantMsg) {
				t.Fatalf("message %q missing %q", got.Message, tc.wantMsg)
			}
		})
	}
}
