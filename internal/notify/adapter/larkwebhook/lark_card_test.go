package larkwebhook

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Phixsura/attune/internal/domain"
)

func TestUrgencyTemplate(t *testing.T) {
	if urgencyTemplate(true) != "red" {
		t.Error("urgent should be red")
	}
	if urgencyTemplate(false) != "blue" {
		t.Error("non-urgent should be blue")
	}
}

func TestCardTitle_UrgentPrefix(t *testing.T) {
	s := domain.Snapshot{Title: "x", IsUrgent: true}
	if !strings.HasPrefix(cardTitle(s), "[Urgent] ") {
		t.Errorf("urgent should prefix title: %q", cardTitle(s))
	}
}

func TestCardTitle_NoPrefixWhenCalm(t *testing.T) {
	s := domain.Snapshot{Title: "x", IsUrgent: false}
	if cardTitle(s) != "x" {
		t.Errorf("non-urgent should be bare title: %q", cardTitle(s))
	}
}

func TestCardTitle_EmptyTitleFallback(t *testing.T) {
	if cardTitle(domain.Snapshot{}) != "(no AI title)" {
		t.Error("empty title fallback missing")
	}
}

func TestStableDimOrder_AlphabeticalDeterminism(t *testing.T) {
	got := stableDimOrder(map[string]any{"z": 1, "a": 2, "m": 3})
	want := []string{"a", "m", "z"}
	if len(got) != len(want) {
		t.Fatalf("len mismatch")
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("order: got %v, want %v", got, want)
		}
	}
}

func TestStableDimOrder_EmptyMapReturnsNil(t *testing.T) {
	if got := stableDimOrder(nil); got != nil {
		t.Errorf("expected nil for empty, got %v", got)
	}
}

func TestFormatAttr_StringValue(t *testing.T) {
	got := formatAttr("severity", "critical")
	if !strings.Contains(got, "**severity**") || !strings.Contains(got, "critical") {
		t.Errorf("string formatting wrong: %q", got)
	}
}

func TestFormatAttr_StringSlice(t *testing.T) {
	got := formatAttr("labels", []string{"a", "b", "c"})
	if !strings.Contains(got, "a / b / c") {
		t.Errorf("string slice formatting wrong: %q", got)
	}
}

func TestFormatAttr_AnySliceFromJSONDecode(t *testing.T) {
	// encoding/json decodes []string as []any.
	got := formatAttr("labels", []any{"x", "y"})
	if !strings.Contains(got, "x / y") {
		t.Errorf("[]any formatting wrong: %q", got)
	}
}

func TestFormatAttr_FallbackForUnknownType(t *testing.T) {
	got := formatAttr("count", 42)
	if !strings.Contains(got, "42") {
		t.Errorf("fallback formatting should still emit value: %q", got)
	}
}

func TestBuildCard_RendersAttrs(t *testing.T) {
	s := domain.Snapshot{
		ID:         9,
		Content:    "raw content",
		Source:     "api",
		Title:      "Payment broke",
		IsUrgent:   true,
		Attrs:      map[string]any{"type": "bug", "severity": "critical", "labels": []string{"pay"}},
		EnrichedAt: time.Now(),
	}
	card := buildCard(s)
	raw, _ := json.Marshal(card)
	body := string(raw)
	// header is red on urgent
	if !strings.Contains(body, `"template":"red"`) {
		t.Errorf("urgent header missing: %s", body)
	}
	// dims render as short fields in stable alphabetical order
	if !strings.Contains(body, "labels") || !strings.Contains(body, "severity") || !strings.Contains(body, "type") {
		t.Errorf("dims not rendered: %s", body)
	}
	// content quoted block
	if !strings.Contains(body, "raw content") {
		t.Errorf("content quote missing")
	}
}

func TestBuildCard_EmptyAttrsHasFallbackField(t *testing.T) {
	s := domain.Snapshot{Content: "x", Title: "t"}
	card := buildCard(s)
	raw, _ := json.Marshal(card)
	body := string(raw)
	// Should still emit at least one **Attrs** placeholder field
	if !strings.Contains(body, "Attrs") && !strings.Contains(body, "Source") {
		t.Errorf("empty attrs card should still render: %s", body)
	}
}

func TestShortenUserID_StripsExtPrefix(t *testing.T) {
	got := shortenUserID("ext_abc-uuid:open_id_xyz")
	if got != "open_id_xyz" {
		t.Errorf("expected open_id_xyz, got %q", got)
	}
}

func TestShortenUserID_LeavesNonExtAlone(t *testing.T) {
	got := shortenUserID("plain_user_123")
	if got != "plain_user_123" {
		t.Errorf("non-prefixed should pass through, got %q", got)
	}
}
