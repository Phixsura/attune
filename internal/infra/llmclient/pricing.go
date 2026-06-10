package llmclient

import (
	_ "embed"
	"encoding/json"
	"sort"
	"strings"
	"sync"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

//go:embed model_prices_and_context_window.json
var modelPricesJSON []byte

// ModelPrice is an input/output token price in USD per one million tokens.
type ModelPrice struct {
	InputPerMTok  float64
	OutputPerMTok float64
}

// PricingResult is the derived cost for one LLM completion.
type PricingResult struct {
	Known   bool
	Price   ModelPrice
	CostUSD float64
}

type liteLLMModelPrice struct {
	InputCostPerToken  *float64 `json:"input_cost_per_token"`
	OutputCostPerToken *float64 `json:"output_cost_per_token"`
}

var (
	priceCatalogOnce sync.Once
	priceCatalog     map[string]ModelPrice
	pricePrefixes    []string
)

// PriceUsage returns the estimated USD cost for model + token usage.
func PriceUsage(model string, usage Usage) PricingResult {
	price, ok := LookupModelPrice(model)
	if !ok {
		return PricingResult{}
	}
	in := float64(nonNegativeI32(usage.InputTokens)) / 1_000_000 * price.InputPerMTok
	out := float64(nonNegativeI32(usage.OutputTokens)) / 1_000_000 * price.OutputPerMTok
	return PricingResult{Known: true, Price: price, CostUSD: in + out}
}

// LookupModelPrice resolves exact model names and dated aliases by prefix.
func LookupModelPrice(model string) (ModelPrice, bool) {
	return lookupModelPrice(normalizeModelName(model))
}

func nonNegativeI32(n int32) int32 {
	if n < 0 {
		return 0
	}
	return n
}

func lookupModelPrice(name string) (ModelPrice, bool) {
	if name == "" {
		return ModelPrice{}, false
	}
	loadPriceCatalog()
	if price, ok := priceCatalog[name]; ok {
		return price, true
	}
	if idx := strings.LastIndexByte(name, '/'); idx >= 0 && idx < len(name)-1 {
		if price, ok := lookupModelPriceNoProvider(name[idx+1:]); ok {
			return price, true
		}
	}
	return lookupModelPriceNoProvider(name)
}

func lookupModelPriceNoProvider(name string) (ModelPrice, bool) {
	if price, ok := priceCatalog[name]; ok {
		return price, true
	}
	for _, prefix := range pricePrefixes {
		if modelHasPrefix(name, prefix) {
			return priceCatalog[prefix], true
		}
	}
	return ModelPrice{}, false
}

func loadPriceCatalog() {
	priceCatalogOnce.Do(func() {
		var raw map[string]liteLLMModelPrice
		if err := json.Unmarshal(modelPricesJSON, &raw); err != nil {
			priceCatalog = map[string]ModelPrice{}
			return
		}
		priceCatalog = make(map[string]ModelPrice, len(raw))
		pricePrefixes = make([]string, 0, len(raw))
		for key, entry := range raw {
			price, ok := liteLLMPrice(entry)
			if !ok {
				continue
			}
			name := normalizeModelName(key)
			priceCatalog[name] = price
			pricePrefixes = append(pricePrefixes, name)
		}
		sort.Slice(pricePrefixes, func(i, j int) bool {
			return len(pricePrefixes[i]) > len(pricePrefixes[j])
		})
	})
}

func liteLLMPrice(entry liteLLMModelPrice) (ModelPrice, bool) {
	if entry.InputCostPerToken == nil && entry.OutputCostPerToken == nil {
		return ModelPrice{}, false
	}
	price := ModelPrice{}
	if entry.InputCostPerToken != nil {
		price.InputPerMTok = ptrext.Indirect(entry.InputCostPerToken) * 1_000_000
	}
	if entry.OutputCostPerToken != nil {
		price.OutputPerMTok = ptrext.Indirect(entry.OutputCostPerToken) * 1_000_000
	}
	return price, true
}

func normalizeModelName(model string) string {
	return strings.ToLower(strings.TrimSpace(model))
}

func modelHasPrefix(model, prefix string) bool {
	if !strings.HasPrefix(model, prefix) {
		return false
	}
	if len(model) == len(prefix) {
		return true
	}
	switch model[len(prefix)] {
	case '-', '_', ':', '@':
		return true
	default:
		return false
	}
}
