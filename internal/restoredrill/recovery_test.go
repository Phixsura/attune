// SPDX-License-Identifier: Apache-2.0

package restoredrill

import (
	"strings"
	"testing"
	"time"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func dptr(d time.Duration) *time.Duration { return ptrext.Of(d) }

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
