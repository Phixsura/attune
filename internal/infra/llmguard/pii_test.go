package llmguard

import (
	"context"
	"strings"
	"testing"
)

func TestPIIGuard_RedactsMultipleEntities(t *testing.T) {
	g := NewPIIGuard()
	res, err := g.Apply(context.Background(), GuardInput{
		Text: "联系 alice@example.com 或 13800138000，身份证 11010519491231002X。",
		Rules: []Rule{
			{Guard: "pii", Stage: StageLLMInput, Entities: []string{"email"}, Action: ActionRedact},
			{Guard: "pii", Stage: StageLLMInput, Entities: []string{"cn_mobile"}, Action: ActionRedact},
			{Guard: "pii", Stage: StageLLMInput, Entities: []string{"cn_id"}, Action: ActionRedact},
		},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	for _, raw := range []string{"alice@example.com", "13800138000", "11010519491231002X"} {
		if strings.Contains(res.Text, raw) {
			t.Fatalf("raw PII %q leaked in %q", raw, res.Text)
		}
	}
}

func TestPIIGuard_CreditCardUsesLuhn(t *testing.T) {
	g := NewPIIGuard()
	res, err := g.Apply(context.Background(), GuardInput{
		Text:  "sku 1234567890123 card 4111 1111 1111 1111",
		Rules: []Rule{{Guard: "pii", Stage: StageLLMInput, Entities: []string{"credit_card"}, Action: ActionRedact}},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if !strings.Contains(res.Text, "1234567890123") {
		t.Fatalf("non-Luhn SKU should not redact: %q", res.Text)
	}
	if strings.Contains(res.Text, "4111") {
		t.Fatalf("card was not redacted: %q", res.Text)
	}
}

func TestPIIGuard_BlockDecision(t *testing.T) {
	g := NewPIIGuard()
	res, err := g.Apply(context.Background(), GuardInput{
		Text:  "card 4111111111111111",
		Rules: []Rule{{Guard: "pii", Stage: StageLLMInput, Entities: []string{"credit_card"}, Action: ActionBlock}},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if !res.Blocked {
		t.Fatal("expected block")
	}
}

func TestPIIGuard_MultipleEntitiesInSingleRule(t *testing.T) {
	t.Parallel()
	g := NewPIIGuard()
	res, err := g.Apply(context.Background(), GuardInput{
		Text: "email alice@example.com phone 13800138000",
		Rules: []Rule{
			{Guard: "pii", Stage: StageLLMInput, Entities: []string{"email"}, Action: ActionRedact},
			{Guard: "pii", Stage: StageLLMInput, Entities: []string{"cn_mobile"}, Action: ActionRedact},
		},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if strings.Contains(res.Text, "alice@example.com") {
		t.Fatalf("email leaked: %q", res.Text)
	}
	if strings.Contains(res.Text, "13800138000") {
		t.Fatalf("phone leaked: %q", res.Text)
	}
}

func TestPIIGuard_AllSameDigitRejects(t *testing.T) {
	t.Parallel()
	g := NewPIIGuard()
	res, err := g.Apply(context.Background(), GuardInput{
		Text:  "number 1111111111111111",
		Rules: []Rule{{Guard: "pii", Stage: StageLLMInput, Entities: []string{"credit_card"}, Action: ActionRedact}},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if !strings.Contains(res.Text, "1111111111111111") {
		t.Fatalf("all-same-digit number should not be redacted: %q", res.Text)
	}
}

func TestPIIGuard_ActionOff(t *testing.T) {
	t.Parallel()
	g := NewPIIGuard()
	res, err := g.Apply(context.Background(), GuardInput{
		Text:  "alice@example.com",
		Rules: []Rule{{Guard: "pii", Stage: StageLLMInput, Entities: []string{"email"}, Action: ActionOff}},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if !strings.Contains(res.Text, "alice@example.com") {
		t.Fatalf("ActionOff should not redact: %q", res.Text)
	}
	if len(res.Findings) != 0 {
		t.Fatalf("ActionOff should produce no findings, got %v", res.Findings)
	}
}

func TestPIIGuard_PhoneFormats(t *testing.T) {
	t.Parallel()
	g := NewPIIGuard()
	res, err := g.Apply(context.Background(), GuardInput{
		Text:  "call +14155552671 asap",
		Rules: []Rule{{Guard: "pii", Stage: StageLLMInput, Entities: []string{"phone"}, Action: ActionRedact}},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if strings.Contains(res.Text, "4155552671") {
		t.Fatalf("phone leaked: %q", res.Text)
	}
}

func TestPIIGuard_CNIDChecksum(t *testing.T) {
	t.Parallel()
	g := NewPIIGuard()
	res, err := g.Apply(context.Background(), GuardInput{
		Text:  "fake 12345678901234567X real 11010519491231002X",
		Rules: []Rule{{Guard: "pii", Stage: StageLLMInput, Entities: []string{"cn_id"}, Action: ActionRedact}},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if strings.Contains(res.Text, "11010519491231002X") {
		t.Fatalf("valid CN ID leaked: %q", res.Text)
	}
}

func TestStrongerPIIMatch(t *testing.T) {
	t.Parallel()
	block := piiMatch{start: 0, end: 10, entity: "a", action: ActionBlock}
	redact := piiMatch{start: 0, end: 15, entity: "b", action: ActionRedact}

	result := strongerPIIMatch(redact, block)
	if result.action != ActionBlock {
		t.Fatalf("expected block wins, got %v", result.action)
	}

	wider := piiMatch{start: 0, end: 20, entity: "c", action: ActionRedact}
	result = strongerPIIMatch(redact, wider)
	if result.end != 20 {
		t.Fatalf("expected wider span wins, got end=%d", result.end)
	}

	result = strongerPIIMatch(redact, redact)
	if result.end != 15 {
		t.Fatalf("same rank same span returns a, got end=%d", result.end)
	}
}

func TestMergePIIMatches_Overlapping(t *testing.T) {
	t.Parallel()
	matches := []piiMatch{
		{start: 0, end: 10, entity: "a", action: ActionRedact},
		{start: 5, end: 15, entity: "b", action: ActionBlock},
		{start: 20, end: 30, entity: "c", action: ActionRedact},
	}
	merged := mergePIIMatches(matches)
	if len(merged) != 2 {
		t.Fatalf("expected 2 merged, got %d", len(merged))
	}
	if merged[0].action != ActionBlock {
		t.Fatalf("first match should be upgraded to block")
	}
}

func TestAllSameDigit(t *testing.T) {
	t.Parallel()
	if !allSameDigit("0000") {
		t.Fatal("all zeros should be true")
	}
	if !allSameDigit("5555555") {
		t.Fatal("all fives should be true")
	}
	if allSameDigit("0001") {
		t.Fatal("mixed should be false")
	}
	if !allSameDigit("") {
		t.Fatal("empty should be true")
	}
	if !allSameDigit("7") {
		t.Fatal("single digit should be true")
	}
}

func TestDetectEntity_UnknownEntity(t *testing.T) {
	t.Parallel()
	matches := detectEntity("hello world", "nonexistent_entity", Rule{Action: ActionRedact})
	if len(matches) != 0 {
		t.Fatalf("unknown entity should return 0 matches, got %d", len(matches))
	}
}

func TestCapturedPIISpan_ShortLoc(t *testing.T) {
	t.Parallel()
	start, end := capturedPIISpan([]int{0})
	if start != -1 || end != -1 {
		t.Fatalf("short loc should return -1,-1, got %d,%d", start, end)
	}
}

func TestCapturedPIISpan_Group1(t *testing.T) {
	t.Parallel()
	start, end := capturedPIISpan([]int{0, 20, 5, 10})
	if start != 5 || end != 10 {
		t.Fatalf("expected group1 5,10 got %d,%d", start, end)
	}
}

func TestCapturedPIISpan_Group2(t *testing.T) {
	t.Parallel()
	start, end := capturedPIISpan([]int{0, 20, -1, -1, 5, 10})
	if start != 5 || end != 10 {
		t.Fatalf("expected group2 5,10 got %d,%d", start, end)
	}
}

func TestCapturedPIISpan_NilLoc(t *testing.T) {
	t.Parallel()
	start, end := capturedPIISpan(nil)
	if start != -1 || end != -1 {
		t.Fatalf("nil loc should return -1,-1, got %d,%d", start, end)
	}
}

func BenchmarkPIIGuardRedact(b *testing.B) {
	text := strings.Repeat("hello alice@example.com phone +1 4155552671 card 4111111111111111\n", 1024)
	g := NewPIIGuard()
	input := GuardInput{
		Text: text,
		Rules: []Rule{
			{Guard: "pii", Stage: StageLLMInput, Entities: []string{"email"}, Action: ActionRedact},
			{Guard: "pii", Stage: StageLLMInput, Entities: []string{"phone"}, Action: ActionRedact},
			{Guard: "pii", Stage: StageLLMInput, Entities: []string{"credit_card"}, Action: ActionRedact},
		},
	}
	b.SetBytes(int64(len(text)))
	for i := 0; i < b.N; i++ {
		if _, err := g.Apply(context.Background(), input); err != nil {
			b.Fatal(err)
		}
	}
}
