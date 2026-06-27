package llmclient

import (
	"math"
	"testing"
)

func TestPriceUsage_KnownModel(t *testing.T) {
	got := PriceUsage("gpt-4o-mini", Usage{InputTokens: 1_000_000, OutputTokens: 500_000})
	if !got.Known {
		t.Fatal("gpt-4o-mini should be priced")
	}
	want := 0.15 + 0.30
	if math.Abs(got.CostUSD-want) > 0.000000001 {
		t.Fatalf("cost: got %.8f want %.8f", got.CostUSD, want)
	}
}

func TestPriceUsage_DatedAlias(t *testing.T) {
	got := PriceUsage("claude-sonnet-4-5-20250929", Usage{InputTokens: 1000, OutputTokens: 1000})
	if !got.Known {
		t.Fatal("dated Claude alias should resolve from LiteLLM prices")
	}
	want := 0.003 + 0.015
	if math.Abs(got.CostUSD-want) > 0.000000001 {
		t.Fatalf("cost: got %.8f want %.8f", got.CostUSD, want)
	}
}

func TestPriceUsage_GPT55FromLiteLLMTable(t *testing.T) {
	got := PriceUsage("gpt-5.5", Usage{InputTokens: 809, OutputTokens: 99})
	if !got.Known {
		t.Fatal("gpt-5.5 should resolve from LiteLLM prices")
	}
	want := 809.0/1_000_000*5.00 + 99.0/1_000_000*30.00
	if math.Abs(got.CostUSD-want) > 0.000000001 {
		t.Fatalf("cost: got %.8f want %.8f", got.CostUSD, want)
	}
}

func TestPriceUsage_ProviderPrefixedModel(t *testing.T) {
	got := PriceUsage("openai/gpt-4o-mini", Usage{InputTokens: 1_000_000, OutputTokens: 1_000_000})
	if !got.Known {
		t.Fatal("provider-prefixed model should resolve by model id suffix")
	}
	want := 0.15 + 0.60
	if math.Abs(got.CostUSD-want) > 0.000000001 {
		t.Fatalf("cost: got %.8f want %.8f", got.CostUSD, want)
	}
}

func TestNonNegativeI32(t *testing.T) {
	t.Parallel()
	if nonNegativeI32(-5) != 0 {
		t.Fatal("negative should clamp to 0")
	}
	if nonNegativeI32(0) != 0 {
		t.Fatal("zero should stay 0")
	}
	if nonNegativeI32(42) != 42 {
		t.Fatal("positive should be unchanged")
	}
}

func TestModelHasPrefix_ExactMatch(t *testing.T) {
	t.Parallel()
	if !modelHasPrefix("gpt-4o", "gpt-4o") {
		t.Fatal("exact match should be true")
	}
}

func TestModelHasPrefix_Separators(t *testing.T) {
	t.Parallel()
	for _, sep := range []string{"-", "_", ":", "@"} {
		if !modelHasPrefix("gpt-4o"+sep+"mini", "gpt-4o") {
			t.Fatalf("separator %q should match", sep)
		}
	}
}

func TestModelHasPrefix_NoSeparator(t *testing.T) {
	t.Parallel()
	if modelHasPrefix("gpt-4omini", "gpt-4o") {
		t.Fatal("no separator should not match")
	}
}

func TestModelHasPrefix_NotPrefix(t *testing.T) {
	t.Parallel()
	if modelHasPrefix("llama-3", "gpt-4o") {
		t.Fatal("non-prefix should not match")
	}
}

func TestNormalizeModelName(t *testing.T) {
	t.Parallel()
	if normalizeModelName("  GPT-4O  ") != "gpt-4o" {
		t.Fatal("should trim and lowercase")
	}
}

func TestLookupModelPrice_EmptyModel(t *testing.T) {
	t.Parallel()
	_, ok := LookupModelPrice("")
	if ok {
		t.Fatal("empty model should not be found")
	}
}

func TestPriceUsage_NegativeTokens(t *testing.T) {
	t.Parallel()
	got := PriceUsage("gpt-4o-mini", Usage{InputTokens: -100, OutputTokens: -50})
	if got.CostUSD != 0 {
		t.Fatalf("negative tokens should result in 0 cost, got %f", got.CostUSD)
	}
}

func TestPriceUsage_UnknownModelRecordsZeroCost(t *testing.T) {
	got := PriceUsage("private-gateway-model", Usage{InputTokens: 42, OutputTokens: 11})
	if got.Known {
		t.Fatal("private model should be unknown")
	}
	if got.CostUSD != 0 {
		t.Fatalf("unknown model cost: got %f", got.CostUSD)
	}
}
