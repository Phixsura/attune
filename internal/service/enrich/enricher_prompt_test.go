package enrich

import (
	"strings"
	"testing"

	"github.com/Phixsura/attune/internal/domain"
)

func TestRenderPrompt_DefaultByteIdentical(t *testing.T) {
	content := "结账页一直转圈，付不了款"
	got := renderPrompt(ClassifyConfig{}, content)
	want := strings.NewReplacer(
		"{{content}}", content,
		"{{modules}}", "识别到的产品模块名字符串数组，没识别到就空数组",
	).Replace(defaultPromptTmpl)
	if got != want {
		t.Fatalf("default render drift:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestRenderPrompt_LiteralPercentInContent(t *testing.T) {
	content := "折扣 50% 不生效"
	got := renderPrompt(ClassifyConfig{}, content)
	if !strings.Contains(got, "50%") {
		t.Fatalf("literal %% in content should survive substitution, got:\n%s", got)
	}
}

func TestRenderPrompt_CustomTemplate(t *testing.T) {
	tmpl := "分类这段：{{content}}。模块规则：{{modules}}"
	got := renderPrompt(ClassifyConfig{
		PromptTemplate: &tmpl,
		Modules:        []string{"cart", "shipping"},
	}, "hello")
	if !strings.Contains(got, "hello") {
		t.Fatalf("missing content: %s", got)
	}
	if !strings.Contains(got, "cart | shipping") {
		t.Fatalf("missing constrained modules clause: %s", got)
	}
}

func TestFilterModules_FreeformPassthrough(t *testing.T) {
	in := []string{"购物车", "cart"}
	kept, dropped := filterModules(in, nil)
	if len(dropped) != 0 {
		t.Fatalf("freeform should not drop, got %v", dropped)
	}
	if !slicesEqualStr(kept, in) {
		t.Fatalf("want passthrough %v, got %v", in, kept)
	}
}

func TestFilterModules_CaseInsensitiveCanonical(t *testing.T) {
	allowed := []string{"Cart", "Shipping"}
	kept, dropped := filterModules([]string{"cart", "SHIPPING", "cart"}, allowed)
	if !slicesEqualStr(kept, []string{"Cart", "Shipping"}) {
		t.Fatalf("canonical spellings: want [Cart Shipping], got %v", kept)
	}
	if !slicesEqualStr(dropped, nil) {
		t.Fatalf("unexpected dropped: %v", dropped)
	}
}

func TestFilterModules_DropsOffList(t *testing.T) {
	allowed := []string{"cart"}
	kept, dropped := filterModules([]string{"cart", "checkout"}, allowed)
	if !slicesEqualStr(kept, []string{"cart"}) {
		t.Fatalf("kept: want [cart], got %v", kept)
	}
	if !slicesEqualStr(dropped, []string{"checkout"}) {
		t.Fatalf("dropped: want [checkout], got %v", dropped)
	}
}

func TestFilterModules_AllDropped(t *testing.T) {
	kept, dropped := filterModules([]string{"foo", "bar"}, []string{"cart"})
	if len(kept) != 0 {
		t.Fatalf("want empty kept, got %v", kept)
	}
	if len(dropped) != 2 {
		t.Fatalf("want 2 dropped, got %v", dropped)
	}
}

func TestClassifyConfig_IsConstrained(t *testing.T) {
	if (ClassifyConfig{}).IsConstrained() {
		t.Fatal("empty config should be freeform")
	}
	if !(ClassifyConfig{Modules: []string{"a"}}).IsConstrained() {
		t.Fatal("non-empty modules should be constrained")
	}
}

func TestBuildEnrichSchema_PinsEnums(t *testing.T) {
	s := buildEnrichSchema([]string{"cart", "shipping"})
	if s == nil || s.Name != "attune_enriched_v1" {
		t.Fatalf("unexpected schema header: %+v", s)
	}
	props, ok := s.Schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("missing properties")
	}
	mods, ok := props["modules"].(map[string]any)
	if !ok {
		t.Fatal("missing modules property")
	}
	items, ok := mods["items"].(map[string]any)
	if !ok {
		t.Fatal("missing modules.items")
	}
	enum, ok := items["enum"].([]any)
	if !ok || len(enum) != 2 {
		t.Fatalf("modules enum: want 2 entries, got %v", items["enum"])
	}
}

// TestFilterModules_NilProducedWithWhitelist verifies the edge case
// where the LLM returns no modules at all but a whitelist is active.
func TestFilterModules_NilProducedWithWhitelist(t *testing.T) {
	kept, dropped := filterModules(nil, []string{"cart"})
	if len(kept) != 0 {
		t.Errorf("want empty kept, got %v", kept)
	}
	if len(dropped) != 0 {
		t.Errorf("want empty dropped, got %v", dropped)
	}
}

// TestFilterModules_WhitespaceAndEmptyEntries verifies that whitespace-
// heavy or empty module names from the LLM are handled gracefully.
func TestFilterModules_WhitespaceAndEmptyEntries(t *testing.T) {
	allowed := []string{"Cart"}
	kept, dropped := filterModules([]string{"  cart  ", "", "  "}, allowed)
	if !slicesEqualStr(kept, []string{"Cart"}) {
		t.Errorf("kept: want [Cart], got %v", kept)
	}
	if len(dropped) != 0 {
		t.Errorf("empty/whitespace entries should be skipped, not dropped: %v", dropped)
	}
}

// TestFilterModules_SlashSeparated verifies that LLM-produced compound
// module names like "Cart/Checkout" are split and matched individually.
func TestFilterModules_SlashSeparated(t *testing.T) {
	allowed := []string{"Cart", "Checkout"}
	kept, dropped := filterModules([]string{"Cart/Checkout"}, allowed)
	if !slicesEqualStr(kept, []string{"Cart", "Checkout"}) {
		t.Errorf("slash-separated: want [Cart, Checkout], got %v", kept)
	}
	if len(dropped) != 0 {
		t.Errorf("unexpected dropped: %v", dropped)
	}
}

// TestFilterModules_CommaSeparated verifies comma-separated splitting.
func TestFilterModules_CommaSeparated(t *testing.T) {
	allowed := []string{"cart", "shipping"}
	kept, dropped := filterModules([]string{"cart, shipping"}, allowed)
	if !slicesEqualStr(kept, []string{"cart", "shipping"}) {
		t.Errorf("comma-separated: want [cart, shipping], got %v", kept)
	}
	if len(dropped) != 0 {
		t.Errorf("unexpected dropped: %v", dropped)
	}
}

// TestFilterModules_ChineseCommaSeparated verifies Chinese comma (、) splitting.
func TestFilterModules_ChineseCommaSeparated(t *testing.T) {
	allowed := []string{"cart", "account"}
	kept, dropped := filterModules([]string{"cart、account"}, allowed)
	if !slicesEqualStr(kept, []string{"cart", "account"}) {
		t.Errorf("Chinese comma: want [cart, account], got %v", kept)
	}
	if len(dropped) != 0 {
		t.Errorf("unexpected dropped: %v", dropped)
	}
}

// TestFilterModules_MixedSeparatorAndIndividual verifies that a mix of
// separator-joined and individual module names works correctly.
func TestFilterModules_MixedSeparatorAndIndividual(t *testing.T) {
	allowed := []string{"Cart", "Checkout", "Shipping"}
	kept, dropped := filterModules([]string{"Cart/Checkout", "Shipping", "unknown"}, allowed)
	if !slicesEqualStr(kept, []string{"Cart", "Checkout", "Shipping"}) {
		t.Errorf("mixed: want [Cart, Checkout, Shipping], got %v", kept)
	}
	if !slicesEqualStr(dropped, []string{"unknown"}) {
		t.Errorf("unexpected dropped: %v", dropped)
	}
}

// TestBuildEnrichSchema_EnumsMatchDomain verifies that the schema enums
// for kind and severity are derived from domain constants (no drift).
func TestBuildEnrichSchema_EnumsMatchDomain(t *testing.T) {
	s := buildEnrichSchema([]string{"cart"})
	props := s.Schema["properties"].(map[string]any)

	// Check kind enum matches domain.ValidKinds
	kindEnum := props["kind"].(map[string]any)["enum"].([]any)
	if len(kindEnum) != len(domain.ValidKinds) {
		t.Errorf("kind enum length: want %d, got %d", len(domain.ValidKinds), len(kindEnum))
	}
	for _, v := range kindEnum {
		if !domain.ValidKinds[v.(string)] {
			t.Errorf("schema kind %q not in domain.ValidKinds", v)
		}
	}

	// Check severity enum matches domain.ValidSeverities
	sevEnum := props["severity"].(map[string]any)["enum"].([]any)
	if len(sevEnum) != len(domain.ValidSeverities) {
		t.Errorf("severity enum length: want %d, got %d", len(domain.ValidSeverities), len(sevEnum))
	}
	for _, v := range sevEnum {
		if !domain.ValidSeverities[v.(string)] {
			t.Errorf("schema severity %q not in domain.ValidSeverities", v)
		}
	}
}

// TestFilterModules_EmptyAllowedPassthrough verifies that an empty
// allowed slice is treated as free-form (pass-through).
func TestFilterModules_EmptyAllowedPassthrough(t *testing.T) {
	produced := []string{"anything", "goes"}
	kept, dropped := filterModules(produced, []string{})
	if !slicesEqualStr(kept, produced) {
		t.Errorf("empty allowed should passthrough, got %v", kept)
	}
	if len(dropped) != 0 {
		t.Errorf("empty allowed should not drop, got %v", dropped)
	}
}

// TestRenderPrompt_ContentContainsModulesToken verifies that a
// literal {{modules}} inside the user's feedback text is handled
// correctly by the single-pass strings.Replacer (SSTI-safe).
func TestRenderPrompt_ContentContainsModulesToken(t *testing.T) {
	content := "用户反馈提到了 {{modules}} 这个字符串"
	got := renderPrompt(ClassifyConfig{
		Modules: []string{"cart"},
	}, content)
	if !strings.Contains(got, "用户反馈提到了 {{modules}} 这个字符串") {
		t.Fatalf("literal {{modules}} in content should survive single-pass substitution, got:\n%s", got)
	}
	// The template's {{modules}} slot should have been replaced with
	// the constrained clause.
	if !strings.Contains(got, "cart") {
		t.Fatalf("modules clause should contain 'cart', got:\n%s", got)
	}
}

// TestRenderPrompt_EmptyContent verifies empty content is handled
// gracefully (no crash, prompt still renders).
func TestRenderPrompt_EmptyContent(t *testing.T) {
	got := renderPrompt(ClassifyConfig{}, "")
	if got == "" {
		t.Fatal("rendered prompt should not be empty even with empty content")
	}
	if !strings.Contains(got, `"""`) {
		t.Fatalf("prompt should still contain the triple-quote delimiters, got:\n%s", got)
	}
}

// TestModuleMode_Labels verifies stable metric labels.
func TestModuleMode_Labels(t *testing.T) {
	if got := moduleMode(ClassifyConfig{}); got != "freeform" {
		t.Errorf("freeform: want freeform, got %q", got)
	}
	if got := moduleMode(ClassifyConfig{Modules: []string{"cart"}}); got != "constrained" {
		t.Errorf("constrained: want constrained, got %q", got)
	}
}

func slicesEqualStr(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
