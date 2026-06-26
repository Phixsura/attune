// SPDX-License-Identifier: Apache-2.0

package restoredrill

import (
	"context"
	"strings"
	"testing"
)

func TestEvaluateEncoding(t *testing.T) {
	if got := evaluateEncoding("UTF8"); got.Status != StatusPass || got.Name != "encoding" {
		t.Fatalf("UTF8: status=%q name=%q", got.Status, got.Name)
	}
	if got := evaluateEncoding("utf8"); got.Status != StatusPass {
		t.Fatalf("utf8 (case): status=%q, want pass", got.Status)
	}
	got := evaluateEncoding("LATIN1")
	if got.Status != StatusFail {
		t.Fatalf("LATIN1: status=%q, want fail", got.Status)
	}
	if !strings.Contains(got.Message, "LATIN1") {
		t.Fatalf("message %q missing encoding", got.Message)
	}
}

func TestEvaluateMatviews(t *testing.T) {
	if got := evaluateMatviews(nil); got.Status != StatusPass || got.Name != "materialized_views" {
		t.Fatalf("none: status=%q name=%q", got.Status, got.Name)
	}
	got := evaluateMatviews([]string{"public.mv_a"})
	if got.Status != StatusFail || !strings.Contains(got.Message, "mv_a") {
		t.Fatalf("unpopulated: status=%q msg=%q", got.Status, got.Message)
	}
}

func TestEvaluateExtensions(t *testing.T) {
	if got := evaluateExtensions([]string{"vector"}, nil); got.Status != StatusSkip || got.Name != "extensions" {
		t.Fatalf("no baseline: status=%q name=%q", got.Status, got.Name)
	}
	if got := evaluateExtensions([]string{"plpgsql", "vector"}, []string{"vector", "plpgsql"}); got.Status != StatusPass {
		t.Fatalf("superset: status=%q, want pass", got.Status)
	}
	got := evaluateExtensions([]string{"plpgsql"}, []string{"plpgsql", "vector"})
	if got.Status != StatusFail || !strings.Contains(got.Message, "vector") {
		t.Fatalf("missing: status=%q msg=%q", got.Status, got.Message)
	}
}

func TestEvaluateEncoding_Empty(t *testing.T) {
	t.Parallel()
	got := evaluateEncoding("")
	if got.Status != StatusFail {
		t.Fatalf("empty encoding: status=%q, want fail", got.Status)
	}
}

func TestEvaluateEncoding_SQL_ASCII(t *testing.T) {
	t.Parallel()
	got := evaluateEncoding("SQL_ASCII")
	if got.Status != StatusFail {
		t.Fatalf("SQL_ASCII: status=%q, want fail", got.Status)
	}
	if !strings.Contains(got.Message, "SQL_ASCII") {
		t.Fatalf("message %q should mention SQL_ASCII", got.Message)
	}
}

func TestEvaluateMatviews_Multiple(t *testing.T) {
	t.Parallel()
	// Multiple unpopulated views: ensure they are sorted in the output
	got := evaluateMatviews([]string{"public.mv_z", "public.mv_a", "public.mv_m"})
	if got.Status != StatusFail {
		t.Fatalf("multiple unpopulated: status=%q, want fail", got.Status)
	}
	if !strings.Contains(got.Message, "3 unpopulated") {
		t.Fatalf("message %q should mention count", got.Message)
	}
	// Verify sorted order in message
	aIdx := strings.Index(got.Message, "mv_a")
	mIdx := strings.Index(got.Message, "mv_m")
	zIdx := strings.Index(got.Message, "mv_z")
	if aIdx == -1 || mIdx == -1 || zIdx == -1 {
		t.Fatalf("message %q missing view names", got.Message)
	}
	if aIdx >= mIdx || mIdx >= zIdx {
		t.Fatalf("view names not sorted in message %q", got.Message)
	}
}

func TestEvaluateMatviews_EmptySlice(t *testing.T) {
	t.Parallel()
	// An empty (non-nil) slice still means all populated
	got := evaluateMatviews([]string{})
	if got.Status != StatusPass {
		t.Fatalf("empty slice: status=%q, want pass", got.Status)
	}
}

func TestEvaluateExtensions_MultipleMissing(t *testing.T) {
	t.Parallel()
	// Multiple missing extensions: ensure sorted
	got := evaluateExtensions(
		[]string{"plpgsql"},
		[]string{"plpgsql", "vector", "pgcrypto", "amcheck"},
	)
	if got.Status != StatusFail {
		t.Fatalf("multiple missing: status=%q, want fail", got.Status)
	}
	if !strings.Contains(got.Message, "3 extension(s)") {
		t.Fatalf("message %q should mention count", got.Message)
	}
	// Verify sorted order
	amIdx := strings.Index(got.Message, "amcheck")
	pgIdx := strings.Index(got.Message, "pgcrypto")
	vecIdx := strings.Index(got.Message, "vector")
	if amIdx == -1 || pgIdx == -1 || vecIdx == -1 {
		t.Fatalf("message %q missing extension names", got.Message)
	}
	if amIdx >= pgIdx || pgIdx >= vecIdx {
		t.Fatalf("extension names not sorted in message %q", got.Message)
	}
}

func TestEvaluateExtensions_EmptyBaseline(t *testing.T) {
	t.Parallel()
	// An empty baseline (not nil) means zero extensions expected
	got := evaluateExtensions([]string{"plpgsql"}, []string{})
	if got.Status != StatusPass {
		t.Fatalf("empty baseline: status=%q, want pass (nothing to miss)", got.Status)
	}
}

func TestEvaluateExtensions_BothEmpty(t *testing.T) {
	t.Parallel()
	got := evaluateExtensions([]string{}, []string{})
	if got.Status != StatusPass {
		t.Fatalf("both empty: status=%q, want pass", got.Status)
	}
}

func TestEvaluateExtensions_RestoredSuperset(t *testing.T) {
	t.Parallel()
	// Restored has MORE than baseline: still passes (only missing is fail)
	got := evaluateExtensions(
		[]string{"plpgsql", "vector", "pgcrypto", "amcheck"},
		[]string{"plpgsql", "vector"},
	)
	if got.Status != StatusPass {
		t.Fatalf("superset: status=%q, want pass", got.Status)
	}
}

func TestGatherExtensions_NilBaseline(t *testing.T) {
	t.Parallel()
	got := gatherExtensions(context.Background(), nil, nil)
	if got.Status != StatusSkip {
		t.Fatalf("nil baseline: status=%q, want skipped", got.Status)
	}
}
