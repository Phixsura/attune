package domain

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// DimensionKind reports how many values the LLM may emit for one
// Dimension on one row. The closed set is intentional: a tenant's
// taxonomy can be open (any number of dims, any taxonomy values), but
// the storage / wire / UI surface only needs two shapes — string for
// single, []string for multi.
type DimensionKind string

const (
	DimSingle DimensionKind = "single" // LLM picks one (or none if !Required)
	DimMulti  DimensionKind = "multi"  // LLM picks 0..N
)

// IsValid reports whether k is one of the recognized kinds. Anything
// else loaded from persistence is treated as a corruption / DB-schema
// drift and rejected at the boundary.
func (k DimensionKind) IsValid() bool {
	return k == DimSingle || k == DimMulti
}

// Taxonomy is one allowed value for a Dimension. The Value is the
// stable machine identifier — the LLM emits it, SQL filters use it,
// webhook envelopes carry it. DisplayName is i18n surface only and is
// freely editable without breaking any persisted attrs or downstream
// contract.
type Taxonomy struct {
	Value       string     `json:"value"`        // any non-empty Unicode, immutable after creation
	DisplayName I18nString `json:"display_name"` // per-locale display surface
}

// Dimension is one enrichment axis the LLM populates for each row.
// Per-tenant Dimensions are open in count (any number of dims) but
// closed in shape: every dim has the same fields. The LLM JSON
// schema, prompt rendering, post-parse whitelist filter, urgency
// derivation, console rendering, and webhook envelope all derive
// uniformly from this struct — adding a new dim is a tenant-settings
// edit, not a code change.
type Dimension struct {
	Name        string        `json:"name"`         // stable machine key: dimensionNameRe; immutable after creation
	DisplayName I18nString    `json:"display_name"` // per-locale display surface for the dim itself
	Kind        DimensionKind `json:"kind"`         // single | multi
	Taxonomy    []Taxonomy    `json:"taxonomy"`     // allowed values; empty + Kind=multi = freeform (GitHub-labels style)
	UrgentSet   []string      `json:"urgent_set"`   // subset of Taxonomy.Value whose presence flips is_urgent
	Required    bool          `json:"required"`     // if true, LLM JSON schema marks the property required
}

// DimensionSet is a tenant's ordered list of Dimensions. The order is
// the operator-authored order: it controls the order of columns in
// the console list, fields in the LLM prompt, and entries in the
// outbound envelope (when stable order matters — JSON object key
// order does not, but our rendered prompt does).
type DimensionSet []Dimension

// ByName looks up a dimension by its stable Name. Returns (nil, false)
// when not found.
func (s DimensionSet) ByName(name string) (*Dimension, bool) {
	for i := range s {
		if s[i].Name == name {
			return &s[i], true
		}
	}
	return nil, false
}

// dimensionNameRe is the stable-machine-key constraint for
// Dimension.Name. Lower-case Latin letter start; lower-case Latin
// letters, digits, and underscore body; 1-31 chars total.
var dimensionNameRe = regexp.MustCompile(`^[a-z][a-z0-9_]{0,30}$`)

// reservedDimensionNames are Dimension.Name values rejected at
// validation because they collide with attune's own LLM-payload or
// envelope keys. The dimensionNameRe already rejects underscore-led
// names, so a separate `_attune_*` prefix check would be unreachable.
var reservedDimensionNames = map[string]bool{
	"title":     true,
	"rationale": true,
	"content":   true,
	"attrs":     true,
	"is_urgent": true,
	"id":        true,
	"tenant_id": true,
	"source":    true,
	"user_id":   true,
}

// Validation errors are exported because the service layer maps them
// to stable API error codes (and i18n user-facing messages).
var (
	ErrDimensionNameFormat   = errors.New("dimension name must match ^[a-z][a-z0-9_]{0,30}$")
	ErrDimensionNameReserved = errors.New("dimension name is reserved")
	ErrDimensionNameDup      = errors.New("dimension name appears more than once in this tenant")
	ErrDimensionKindInvalid  = errors.New("dimension kind must be \"single\" or \"multi\"")
	ErrDimensionDisplayEmpty = errors.New("dimension display_name must have at least one non-empty entry")
	ErrTaxonomyValueEmpty    = errors.New("taxonomy value must be non-empty")
	ErrTaxonomyValueDup      = errors.New("taxonomy value appears more than once in this dimension")
	ErrTaxonomyDisplayEmpty  = errors.New("taxonomy display_name must have at least one non-empty entry")
	ErrUrgentNotInTaxonomy   = errors.New("urgent_set value not present in taxonomy")
)

// Validate checks the dim in isolation. Tenant-level checks (unique
// names across the set) are done by DimensionSet.Validate.
func (d Dimension) Validate() error {
	if !dimensionNameRe.MatchString(d.Name) {
		return fmt.Errorf("%w: got %q", ErrDimensionNameFormat, d.Name)
	}
	if reservedDimensionNames[d.Name] {
		return fmt.Errorf("%w: %q", ErrDimensionNameReserved, d.Name)
	}
	if !d.Kind.IsValid() {
		return fmt.Errorf("%w: got %q", ErrDimensionKindInvalid, d.Kind)
	}
	if !d.DisplayName.NonEmpty() {
		return fmt.Errorf("%w: dim %q", ErrDimensionDisplayEmpty, d.Name)
	}
	seenVal := make(map[string]bool, len(d.Taxonomy))
	for i, t := range d.Taxonomy {
		v := strings.TrimSpace(t.Value)
		if v == "" {
			return fmt.Errorf("%w: dim %q, index %d", ErrTaxonomyValueEmpty, d.Name, i)
		}
		if seenVal[v] {
			return fmt.Errorf("%w: dim %q, value %q", ErrTaxonomyValueDup, d.Name, v)
		}
		seenVal[v] = true
		if !t.DisplayName.NonEmpty() {
			return fmt.Errorf("%w: dim %q, value %q", ErrTaxonomyDisplayEmpty, d.Name, v)
		}
	}
	for _, u := range d.UrgentSet {
		if !seenVal[u] {
			return fmt.Errorf("%w: dim %q, urgent_set entry %q", ErrUrgentNotInTaxonomy, d.Name, u)
		}
	}
	return nil
}

// Validate checks the full set: every dim is individually valid, and
// names are unique across the set.
func (s DimensionSet) Validate() error {
	seen := make(map[string]bool, len(s))
	for i, d := range s {
		if err := d.Validate(); err != nil {
			return fmt.Errorf("dimensions[%d]: %w", i, err)
		}
		if seen[d.Name] {
			return fmt.Errorf("%w: %q", ErrDimensionNameDup, d.Name)
		}
		seen[d.Name] = true
	}
	return nil
}

// ComputeIsUrgent ORs every dim's UrgentSet against the row's attrs.
// Returns true as soon as any dim contributes — short-circuit semantics
// are fine because urgent has no "more urgent" gradient.
//
// Persisted to user_feedback.is_urgent by the enricher so the row's
// urgent status is a snapshot at classification time — later
// operator edits to UrgentSet apply to new rows only, never
// retroactively flip historical urgent/non-urgent.
func ComputeIsUrgent(attrs map[string]any, dims DimensionSet) bool {
	for _, d := range dims {
		if len(d.UrgentSet) == 0 {
			continue
		}
		v, ok := attrs[d.Name]
		if !ok {
			continue
		}
		switch d.Kind {
		case DimSingle:
			if s, ok := v.(string); ok && containsString(d.UrgentSet, s) {
				return true
			}
		case DimMulti:
			arr, ok := toStringSlice(v)
			if !ok {
				continue
			}
			for _, x := range arr {
				if containsString(d.UrgentSet, x) {
					return true
				}
			}
		}
	}
	return false
}

// FilterAttrs trims an LLM-produced attrs payload to the allowed
// shape under the tenant's DimensionSet:
// - drops dims absent from the set
// - for each dim, drops values absent from its taxonomy (unless
// the taxonomy is empty, i.e. freeform multi-kind)
// - emits per-dim diagnostics via the dropped/suggested return
// channels for the caller to record as metrics
//
// Returns (kept, dropped, suggestedDims) where:
// - kept is the canonical attrs map to persist
// - dropped counts the total off-list values dropped, keyed by dim
// - suggested lists the dims that produced at least one off-list value
func FilterAttrs(produced map[string]any, dims DimensionSet) (
	kept map[string]any, dropped map[string]int, suggested []string,
) {
	kept = make(map[string]any, len(dims))
	dropped = make(map[string]int, len(dims))
	for _, d := range dims {
		v, ok := produced[d.Name]
		if !ok {
			continue
		}
		switch d.Kind {
		case DimSingle:
			if dropN := filterSingle(d, v, kept); dropN > 0 {
				dropped[d.Name] += dropN
				suggested = append(suggested, d.Name)
			}
		case DimMulti:
			if dropN := filterMulti(d, v, kept); dropN > 0 {
				dropped[d.Name] += dropN
				suggested = append(suggested, d.Name)
			}
		}
	}
	return kept, dropped, suggested
}

// filterSingle writes the kept value for one single-kind dim into kept
// and returns how many values were dropped (0 or 1). Off-list / wrong-
// type / empty values are dropped; freeform single-kind keeps anything
// non-empty.
func filterSingle(d Dimension, v any, kept map[string]any) int {
	s, ok := v.(string)
	if !ok || s == "" {
		return 0
	}
	allowed := taxonomyValues(d.Taxonomy)
	if len(allowed) == 0 || containsString(allowed, s) {
		kept[d.Name] = s
		return 0
	}
	return 1
}

// filterMulti writes the kept values for one multi-kind dim into kept
// and returns how many values were dropped (always 0 for freeform).
func filterMulti(d Dimension, v any, kept map[string]any) int {
	arr, ok := toStringSlice(v)
	if !ok {
		return 0
	}
	allowed := taxonomyValues(d.Taxonomy)
	if len(allowed) == 0 {
		kept[d.Name] = dedupeStrings(arr)
		return 0
	}
	outKept := make([]string, 0, len(arr))
	outDropped := 0
	seen := make(map[string]bool, len(arr))
	for _, x := range arr {
		if x == "" || seen[x] {
			continue
		}
		seen[x] = true
		if containsString(allowed, x) {
			outKept = append(outKept, x)
		} else {
			outDropped++
		}
	}
	kept[d.Name] = outKept
	return outDropped
}

// taxonomyValues extracts the Value list from a taxonomy slice.
func taxonomyValues(t []Taxonomy) []string {
	if len(t) == 0 {
		return nil
	}
	out := make([]string, len(t))
	for i, x := range t {
		out[i] = x.Value
	}
	return out
}

func containsString(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

// toStringSlice converts JSON-decoded array shapes to []string. The
// LLM-decoded payload may come back as []any (encoding/json) or
// []string (typed) depending on the path. Both are accepted.
func toStringSlice(v any) ([]string, bool) {
	switch x := v.(type) {
	case []string:
		return x, true
	case []any:
		out := make([]string, 0, len(x))
		for _, item := range x {
			s, ok := item.(string)
			if !ok {
				return nil, false
			}
			out = append(out, s)
		}
		return out, true
	default:
		return nil, false
	}
}

func dedupeStrings(in []string) []string {
	if len(in) == 0 {
		return in
	}
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, x := range in {
		if x == "" || seen[x] {
			continue
		}
		seen[x] = true
		out = append(out, x)
	}
	return out
}
