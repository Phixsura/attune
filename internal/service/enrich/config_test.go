package enrich

import (
	"errors"
	"strings"
	"testing"
)

func TestValidatePromptTemplate(t *testing.T) {
	if err := ValidatePromptTemplate("hello {{content}}"); err != nil {
		t.Fatalf("valid template rejected: %v", err)
	}
	if err := ValidatePromptTemplate("no token"); !errors.Is(err, ErrMissingContentToken) {
		t.Fatalf("want missing token, got %v", err)
	}
	long := strings.Repeat("x", MaxPromptTemplateLen+1)
	if err := ValidatePromptTemplate(long + " {{content}}"); !errors.Is(err, ErrTemplateTooLong) {
		t.Fatalf("want too long, got %v", err)
	}
}

func TestNormalizeModules(t *testing.T) {
	out, err := normalizeModules([]string{" Cart ", "cart", "Shipping"})
	if err != nil {
		t.Fatal(err)
	}
	if !slicesEqualStr(out, []string{"Cart", "Shipping"}) {
		t.Fatalf("want deduped canonical, got %v", out)
	}
	_, err = normalizeModules([]string{" "})
	if !errors.Is(err, ErrEmptyModuleName) {
		t.Fatalf("want empty module error, got %v", err)
	}
}

func TestPreviewUsesRenderPrompt(t *testing.T) {
	tmpl := "反馈：{{content}} / {{modules}}"
	got := renderPrompt(ClassifyConfig{
		PromptTemplate: &tmpl,
		Modules:        []string{"cart"},
	}, "sample")
	if !strings.Contains(got, "sample") || !strings.Contains(got, "cart") {
		t.Fatalf("preview path drift: %s", got)
	}
}
