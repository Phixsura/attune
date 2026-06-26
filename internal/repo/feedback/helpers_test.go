// SPDX-License-Identifier: Apache-2.0
// ptrext:file-allow test-fixtures

package feedback

import (
	"database/sql"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// ---------------------------------------------------------------------------
// mergeEnrichmentMetadata
// ---------------------------------------------------------------------------

func Test_mergeEnrichmentMetadata(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   []EnrichmentMetadata
		want EnrichmentMetadata
	}{
		{
			name: "empty slice returns zero value",
			in:   nil,
			want: EnrichmentMetadata{},
		},
		{
			name: "single meta with both fields",
			in:   []EnrichmentMetadata{{Language: "en", DisplayLocale: "en-US"}},
			want: EnrichmentMetadata{Language: "en", DisplayLocale: "en-US"},
		},
		{
			name: "last non-empty wins independently",
			in: []EnrichmentMetadata{
				{Language: "en", DisplayLocale: "en-US"},
				{Language: "", DisplayLocale: "zh-CN"},
				{Language: "ja", DisplayLocale: ""},
			},
			want: EnrichmentMetadata{Language: "ja", DisplayLocale: "zh-CN"},
		},
		{
			name: "all empty fields returns zero value",
			in: []EnrichmentMetadata{
				{Language: "", DisplayLocale: ""},
				{Language: "", DisplayLocale: ""},
			},
			want: EnrichmentMetadata{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := mergeEnrichmentMetadata(tt.in)
			require.Equal(t, tt.want, got)
		})
	}
}

// ---------------------------------------------------------------------------
// inboundSourceIDFromMeta
// ---------------------------------------------------------------------------

func Test_inboundSourceIDFromMeta(t *testing.T) {
	t.Parallel()

	validUUID := uuid.MustParse("01234567-89ab-cdef-0123-456789abcdef")

	tests := []struct {
		name string
		meta map[string]any
		want *uuid.UUID
	}{
		{
			name: "nil map returns nil",
			meta: nil,
			want: nil,
		},
		{
			name: "missing key returns nil",
			meta: map[string]any{"other": "value"},
			want: nil,
		},
		{
			name: "wrong type value returns nil",
			meta: map[string]any{domain.SourceMetaInboundSourceID: 12345},
			want: nil,
		},
		{
			name: "empty string returns nil",
			meta: map[string]any{domain.SourceMetaInboundSourceID: ""},
			want: nil,
		},
		{
			name: "invalid UUID string returns nil",
			meta: map[string]any{domain.SourceMetaInboundSourceID: "not-a-uuid"},
			want: nil,
		},
		{
			name: "valid UUID string returns pointer",
			meta: map[string]any{domain.SourceMetaInboundSourceID: validUUID.String()},
			want: ptrext.Of(validUUID),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := inboundSourceIDFromMeta(tt.meta)
			if tt.want == nil {
				require.Nil(t, got)
			} else {
				require.NotNil(t, got)
				require.Equal(t, *tt.want, *got)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// nullFloatPtr
// ---------------------------------------------------------------------------

func Test_nullFloatPtr(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   sql.NullFloat64
		want *float64
	}{
		{
			name: "invalid returns nil",
			in:   sql.NullFloat64{Valid: false},
			want: nil,
		},
		{
			name: "valid zero",
			in:   sql.NullFloat64{Float64: 0.0, Valid: true},
			want: ptrext.Of(0.0),
		},
		{
			name: "valid non-zero",
			in:   sql.NullFloat64{Float64: 3.14, Valid: true},
			want: ptrext.Of(3.14),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := nullFloatPtr(tt.in)
			if tt.want == nil {
				require.Nil(t, got)
			} else {
				require.NotNil(t, got)
				require.InDelta(t, *tt.want, *got, 1e-15)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// normalizeSemanticRun
// ---------------------------------------------------------------------------

func Test_normalizeSemanticRun(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   SemanticExtractionRun
		want SemanticExtractionRun
	}{
		{
			name: "all defaults applied",
			in:   SemanticExtractionRun{TenantID: "t1", SubjectID: 42, Model: "gpt-4o"},
			want: SemanticExtractionRun{
				TenantID:      "t1",
				SubjectType:   SemanticSubjectFeedback,
				SubjectID:     42,
				DomainPack:    DefaultDomainPack,
				SchemaVersion: DefaultSchemaVersion,
				PromptVersion: "default",
				Model:         "gpt-4o",
			},
		},
		{
			name: "all custom values preserved",
			in: SemanticExtractionRun{
				TenantID:      "t2",
				SubjectType:   "ticket",
				SubjectID:     99,
				DomainPack:    "custom_pack",
				SchemaVersion: "custom.v2",
				PromptVersion: "v3-beta",
				Model:         "claude-3-opus",
			},
			want: SemanticExtractionRun{
				TenantID:      "t2",
				SubjectType:   "ticket",
				SubjectID:     99,
				DomainPack:    "custom_pack",
				SchemaVersion: "custom.v2",
				PromptVersion: "v3-beta",
				Model:         "claude-3-opus",
			},
		},
		{
			name: "partial custom keeps custom and defaults the rest",
			in: SemanticExtractionRun{
				TenantID:    "t3",
				SubjectType: "review",
				SubjectID:   7,
				Model:       "gpt-4o-mini",
			},
			want: SemanticExtractionRun{
				TenantID:      "t3",
				SubjectType:   "review",
				SubjectID:     7,
				DomainPack:    DefaultDomainPack,
				SchemaVersion: DefaultSchemaVersion,
				PromptVersion: "default",
				Model:         "gpt-4o-mini",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := normalizeSemanticRun(tt.in)
			require.Equal(t, tt.want, got)
		})
	}
}

// ---------------------------------------------------------------------------
// jsonObject
// ---------------------------------------------------------------------------

func Test_jsonObject(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		in      map[string]any
		want    string
		wantErr bool
	}{
		{
			name: "nil map returns empty object",
			in:   nil,
			want: "{}",
		},
		{
			name: "empty map returns empty object",
			in:   map[string]any{},
			want: "{}",
		},
		{
			name: "non-empty map returns valid JSON",
			in:   map[string]any{"key": "value", "n": float64(1)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := jsonObject(tt.in)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			if tt.want != "" {
				require.Equal(t, tt.want, string(got))
			} else {
				// Verify it is valid JSON and round-trips.
				var m map[string]any
				require.NoError(t, json.Unmarshal(got, &m))
				require.Equal(t, len(tt.in), len(m))
			}
		})
	}
}

// ---------------------------------------------------------------------------
// normalizeSearchParams
// ---------------------------------------------------------------------------

func Test_normalizeSearchParams(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		in        SemanticSearchParams
		wantLimit int
		wantSim   float64
		wantEf    int
	}{
		{
			name:      "all zeros get defaults",
			in:        SemanticSearchParams{},
			wantLimit: DefaultSearchLimit,
			wantSim:   DefaultMinSimilarity,
			wantEf:    DefaultEfSearch,
		},
		{
			name:      "negative values get defaults",
			in:        SemanticSearchParams{Limit: -1, MinSimilarity: -0.5, EfSearch: -10},
			wantLimit: DefaultSearchLimit,
			wantSim:   DefaultMinSimilarity,
			wantEf:    DefaultEfSearch,
		},
		{
			name:      "limit above max is clamped",
			in:        SemanticSearchParams{Limit: 200, MinSimilarity: 0.7, EfSearch: 400},
			wantLimit: MaxSearchLimit,
			wantSim:   0.7,
			wantEf:    400,
		},
		{
			name:      "valid values are preserved",
			in:        SemanticSearchParams{Limit: 50, MinSimilarity: 0.8, EfSearch: 300},
			wantLimit: 50,
			wantSim:   0.8,
			wantEf:    300,
		},
		{
			name:      "boundary: limit at max",
			in:        SemanticSearchParams{Limit: MaxSearchLimit, MinSimilarity: 0.1, EfSearch: 1},
			wantLimit: MaxSearchLimit,
			wantSim:   0.1,
			wantEf:    1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			limit, minSim, efSearch := normalizeSearchParams(&tt.in)
			require.Equal(t, tt.wantLimit, limit)
			require.InDelta(t, tt.wantSim, minSim, 1e-15)
			require.Equal(t, tt.wantEf, efSearch)
		})
	}
}
