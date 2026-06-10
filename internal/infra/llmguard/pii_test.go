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
