// SPDX-License-Identifier: Apache-2.0

package restoredrill

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// ---------- evaluateConstraints ----------

func TestEvaluateConstraints(t *testing.T) {
	t.Parallel()
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
	require.Contains(t, got.Message, "introduced")
}

func TestEvaluateConstraints_ZeroBaseline(t *testing.T) {
	t.Parallel()
	// Zero baseline with zero restored -> pass (no unvalidated constraints).
	got := evaluateConstraints(0, ptrext.Of(0))
	require.Equal(t, StatusPass, got.Status)
	require.Contains(t, got.Message, "0 unvalidated")
}

func TestEvaluateConstraints_FailMessageIncludesCounts(t *testing.T) {
	t.Parallel()
	got := evaluateConstraints(5, ptrext.Of(2))
	require.Equal(t, StatusFail, got.Status)
	require.Contains(t, got.Message, "3 new unvalidated")
	require.Contains(t, got.Message, "2")
	require.Contains(t, got.Message, "5")
}

func TestEvaluateConstraints_SkipMessageExplainsWhy(t *testing.T) {
	t.Parallel()
	got := evaluateConstraints(10, nil)
	require.Equal(t, StatusSkip, got.Status)
	require.Contains(t, got.Message, "no baseline")
}

// ---------- evaluateSequences ----------

func TestEvaluateSequences(t *testing.T) {
	t.Parallel()
	if got := evaluateSequences(nil); got.Status != StatusPass || got.Name != "sequences" {
		t.Fatalf("none behind: status=%q name=%q", got.Status, got.Name)
	}
	got := evaluateSequences([]string{"public.foo_id_seq", "public.bar_id_seq"})
	if got.Status != StatusFail {
		t.Fatalf("behind: status=%q, want fail", got.Status)
	}
	require.Contains(t, got.Message, "behind")
	require.Contains(t, got.Message, "foo_id_seq")
}

func TestEvaluateSequences_EmptySlice(t *testing.T) {
	t.Parallel()
	got := evaluateSequences([]string{})
	require.Equal(t, StatusPass, got.Status)
	require.Equal(t, "sequences", got.Name)
}

func TestEvaluateSequences_SortsNames(t *testing.T) {
	t.Parallel()
	// The function sorts the behind list alphabetically before including in the message.
	got := evaluateSequences([]string{"public.z_seq", "public.a_seq", "public.m_seq"})
	require.Equal(t, StatusFail, got.Status)
	require.Contains(t, got.Message, "3 sequence(s)")
	// Verify sorted order in message: a_seq should appear before m_seq before z_seq.
	aIdx := len(got.Message) // fallback
	mIdx := len(got.Message)
	zIdx := len(got.Message)
	for i := 0; i < len(got.Message); i++ {
		if i+5 <= len(got.Message) && got.Message[i:i+5] == "a_seq" {
			aIdx = i
		}
		if i+5 <= len(got.Message) && got.Message[i:i+5] == "m_seq" {
			mIdx = i
		}
		if i+5 <= len(got.Message) && got.Message[i:i+5] == "z_seq" {
			zIdx = i
		}
	}
	require.Less(t, aIdx, mIdx, "a_seq should appear before m_seq")
	require.Less(t, mIdx, zIdx, "m_seq should appear before z_seq")
}

func TestEvaluateSequences_SingleBehind(t *testing.T) {
	t.Parallel()
	got := evaluateSequences([]string{"public.users_id_seq"})
	require.Equal(t, StatusFail, got.Status)
	require.Contains(t, got.Message, "1 sequence(s)")
	require.Contains(t, got.Message, "users_id_seq")
}

func TestEvaluateSequences_PassMessage(t *testing.T) {
	t.Parallel()
	got := evaluateSequences(nil)
	require.Equal(t, StatusPass, got.Status)
	require.Contains(t, got.Message, "ahead")
}
