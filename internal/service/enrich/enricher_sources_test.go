package enrich

import (
	"testing"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// TestEnricher_SourceDisplayFallback verifies the enricher resolves a source
// label via DefaultSourceSet when no SourceSet was injected (eval / tests build
// an enricher without SetSourceSet) — a known source gets its label, a retired
// or unknown source degrades to the raw key, never empty.
func TestEnricher_SourceDisplayFallback(t *testing.T) {
	t.Parallel()
	e := ptrext.Of(Enricher{})
	e.SetSourceSet(nil) // explicit nil → DefaultSourceSet fallback

	if got := e.sourceDisplay("api"); got != "API client" {
		t.Errorf("sourceDisplay(api) with nil set = %q; want API client", got)
	}
	if got := e.sourceDisplay("rss-retired"); got != "rss-retired" {
		t.Errorf("sourceDisplay(rss-retired) with nil set = %q; want raw-key fallback", got)
	}
}
