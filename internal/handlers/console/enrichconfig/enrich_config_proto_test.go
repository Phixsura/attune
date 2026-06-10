package enrichconfig

import (
	"errors"
	"reflect"
	"testing"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
)

func TestI18nProtoRoundTrip(t *testing.T) {
	in := domain.I18nString{"default": "X", "zh": "x中", "en": "X"}
	got := i18nFromProto(i18nToProto(in))
	if !reflect.DeepEqual(in, got) {
		t.Errorf("round-trip mismatch:\n  in:  %v\n  got: %v", in, got)
	}
}

func TestI18nProtoRoundTrip_Empty(t *testing.T) {
	// Empty in → empty (or nil) out.
	got := i18nFromProto(i18nToProto(domain.I18nString{}))
	if len(got) != 0 {
		t.Errorf("empty round-trip: got %v", got)
	}
}

func TestI18nProtoNilHandled(t *testing.T) {
	if got := i18nFromProto(nil); got != nil {
		t.Errorf("nil proto → nil domain, got %v", got)
	}
}

func TestTaxonomyProtoRoundTrip(t *testing.T) {
	in := []domain.Taxonomy{
		{
			Value:       "critical",
			DisplayName: domain.I18nString{"default": "Critical", "zh": "严重"},
			Description: domain.I18nString{"default": "High impact"},
			Examples:    []string{"checkout down"},
		},
		{Value: "minor", DisplayName: domain.I18nString{"default": "Minor"}},
	}
	got := taxonomyFromProto(taxonomyToProto(in))
	if len(got) != 2 {
		t.Fatalf("len mismatch: %d", len(got))
	}
	if got[0].Value != "critical" || got[0].DisplayName["zh"] != "严重" {
		t.Errorf("entry 0 corrupted: %+v", got[0])
	}
	if got[0].Description["default"] != "High impact" || len(got[0].Examples) != 1 {
		t.Errorf("semantic metadata lost: %+v", got[0])
	}
}

func TestDimsProtoRoundTrip(t *testing.T) {
	in := domain.DimensionSet{
		{
			Name:        "severity",
			DisplayName: domain.I18nString{"default": "Severity", "zh": "严重程度"},
			Description: domain.I18nString{"default": "Impact level"},
			Kind:        domain.DimSingle,
			Taxonomy: []domain.Taxonomy{
				{Value: "critical", DisplayName: domain.I18nString{"default": "Critical"}},
			},
			UrgentSet:      []string{"critical"},
			Required:       true,
			Examples:       []string{"critical: checkout is down"},
			ExtractionHint: "Prefer critical only for blocked core flows.",
			Renderer: domain.RendererSpec{
				Kind: "enum_badge",
				Values: map[string]domain.RendererValue{
					"critical": {Icon: "flame", Tone: "danger"},
				},
			},
		},
		{
			Name:        "labels",
			DisplayName: domain.I18nString{"default": "Labels"},
			Kind:        domain.DimMulti,
		},
	}
	got, err := dimsFromProto(dimsToProto(in))
	if err != nil {
		t.Fatalf("dimsFromProto: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("dim count: %d", len(got))
	}
	if got[0].Name != "severity" || got[0].Kind != domain.DimSingle {
		t.Errorf("dim 0 corrupted: %+v", got[0])
	}
	if len(got[0].UrgentSet) != 1 || got[0].UrgentSet[0] != "critical" {
		t.Errorf("urgent_set lost: %v", got[0].UrgentSet)
	}
	if !got[0].Required {
		t.Error("required flag lost")
	}
	if got[0].Description["default"] != "Impact level" || got[0].ExtractionHint == "" {
		t.Errorf("semantic dim metadata lost: %+v", got[0])
	}
	if got[0].Renderer.Kind != "enum_badge" || got[0].Renderer.Values["critical"].Icon != "flame" {
		t.Errorf("renderer lost: %+v", got[0].Renderer)
	}
	if got[1].Kind != domain.DimMulti {
		t.Errorf("dim 1 kind: %v", got[1].Kind)
	}
	// freeform multi keeps nil taxonomy
	if len(got[1].Taxonomy) != 0 {
		t.Errorf("freeform should preserve empty taxonomy: %v", got[1].Taxonomy)
	}
}

func TestDimsProtoRoundTrip_DimValidatesAfterRoundtrip(t *testing.T) {
	in := domain.DimensionSet{
		{
			Name:        "severity",
			DisplayName: domain.I18nString{"default": "Severity"},
			Kind:        domain.DimSingle,
			Taxonomy: []domain.Taxonomy{
				{Value: "critical", DisplayName: domain.I18nString{"default": "Critical"}},
			},
			UrgentSet: []string{"critical"},
		},
	}
	got, err := dimsFromProto(dimsToProto(in))
	if err != nil {
		t.Fatalf("dimsFromProto: %v", err)
	}
	if err := got.Validate(); err != nil {
		t.Errorf("round-tripped DimensionSet must still validate, got %v", err)
	}
}

func TestEmptyDimsProtoIsNil(t *testing.T) {
	if dimsToProto(nil) != nil {
		t.Error("nil in → nil out")
	}
	if got, err := dimsFromProto(nil); err != nil || got != nil {
		t.Error("nil proto → nil domain")
	}
}

func TestI18nToProtoNeverReturnsNilEntries(t *testing.T) {
	// Empty input still produces a non-nil entries map so JSON encodes as {}.
	got := i18nToProto(nil)
	if got == nil {
		t.Fatal("expected non-nil proto wrapper")
	}
	if got.Entries == nil {
		t.Error("entries map should be non-nil for stable wire shape")
	}
}

// Smoke: convert proto → domain through a hand-built attunev1 payload
// to confirm we don't rely on ts-proto's specific casing.
func TestDimsFromProto_ManualPayload(t *testing.T) {
	in := []*attunev1.Dimension{
		{
			Name:        "type",
			DisplayName: ptrext.Of(attunev1.I18NString{Entries: map[string]string{"default": "Type"}}),
			Kind:        "single",
			Taxonomy: []*attunev1.Taxonomy{
				{
					Value:       "bug",
					DisplayName: ptrext.Of(attunev1.I18NString{Entries: map[string]string{"default": "Bug"}}),
				},
			},
		},
	}
	got, err := dimsFromProto(in)
	if err != nil {
		t.Fatalf("dimsFromProto: %v", err)
	}
	if len(got) != 1 || got[0].Name != "type" {
		t.Errorf("manual payload decode: %+v", got)
	}
	if got[0].Kind != domain.DimSingle {
		t.Errorf("kind: %v", got[0].Kind)
	}
}

// Boundary check: an unknown kind on the wire must be rejected at the
// proto→domain edge with domain.ErrDimensionKindInvalid, before the
// value flows further into svc.Update. This is what lets the handler
// route bad input straight to a 400 dim_kind_invalid.
func TestDimsFromProto_UnknownKindRejected(t *testing.T) {
	in := []*attunev1.Dimension{{
		Name:        "x",
		DisplayName: ptrext.Of(attunev1.I18NString{Entries: map[string]string{"default": "X"}}),
		Kind:        "ranked", // not "single" / "multi"
	}}
	got, err := dimsFromProto(in)
	if got != nil {
		t.Errorf("expected nil DimensionSet on reject, got %+v", got)
	}
	if !errors.Is(err, domain.ErrDimensionKindInvalid) {
		t.Errorf("err = %v; want wraps ErrDimensionKindInvalid", err)
	}
}
