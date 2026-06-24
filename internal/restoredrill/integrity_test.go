// SPDX-License-Identifier: Apache-2.0

package restoredrill

import (
	"strings"
	"testing"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func TestEvaluateConstraints(t *testing.T) {
	if got := evaluateConstraints(5, nil); got.Status != StatusSkip || got.Name != "constraints" {
		t.Fatalf("no baseline: status=%q name=%q, want skipped", got.Status, got.Name)
	}
	if got := evaluateConstraints(1, ptrext.Of(1)); got.Status != StatusPass {
		t.Fatalf("matches baseline: status=%q, want pass", got.Status)
	}
	if got := evaluateConstraints(0, ptrext.Of(2)); got.Status != StatusPass {
		t.Fatalf("below baseline (restore validated more): status=%q, want pass", got.Status)
	}
	got := evaluateConstraints(3, ptrext.Of(1)) // 3 restored > 1 baseline
	if got.Status != StatusFail {
		t.Fatalf("above baseline: status=%q, want fail", got.Status)
	}
	if !strings.Contains(got.Message, "introduced") {
		t.Fatalf("message %q missing 'introduced'", got.Message)
	}
}

func TestEvaluateSequences(t *testing.T) {
	if got := evaluateSequences(nil); got.Status != StatusPass || got.Name != "sequences" {
		t.Fatalf("none behind: status=%q name=%q", got.Status, got.Name)
	}
	got := evaluateSequences([]string{"public.foo_id_seq", "public.bar_id_seq"})
	if got.Status != StatusFail {
		t.Fatalf("behind: status=%q, want fail", got.Status)
	}
	if !strings.Contains(got.Message, "behind") || !strings.Contains(got.Message, "foo_id_seq") {
		t.Fatalf("message %q missing detail", got.Message)
	}
}
