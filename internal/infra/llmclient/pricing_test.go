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

func TestPriceUsage_UnknownModelRecordsZeroCost(t *testing.T) {
	got := PriceUsage("private-gateway-model", Usage{InputTokens: 42, OutputTokens: 11})
	if got.Known {
		t.Fatal("private model should be unknown")
	}
	if got.CostUSD != 0 {
		t.Fatalf("unknown model cost: got %f", got.CostUSD)
	}
}
