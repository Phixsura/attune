// SPDX-License-Identifier: Apache-2.0

package restoredrill

import (
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
