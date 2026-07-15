// SPDX-License-Identifier: Apache-2.0

package publicvisibility

import (
	"errors"
	"testing"

	repo "github.com/Phixsura/attune/internal/repo/publicvisibility"
)

func TestNormalizeRoadmapStatusMappings(t *testing.T) {
	t.Parallel()

	got, err := normalizeRoadmapStatusMappings(nil)
	if err != nil {
		t.Fatalf("normalizeRoadmapStatusMappings(nil) error = %v", err)
	}
	wantDefaults := defaultRoadmapStatusMappings()
	if len(got) != len(wantDefaults) {
		t.Fatalf("normalizeRoadmapStatusMappings(nil) len = %d, want %d", len(got), len(wantDefaults))
	}
	for i := range wantDefaults {
		if got[i] != wantDefaults[i] {
			t.Fatalf("normalizeRoadmapStatusMappings(nil)[%d] = %#v, want %#v", i, got[i], wantDefaults[i])
		}
	}

	custom, err := normalizeRoadmapStatusMappings([]repo.RoadmapStatusMapping{
		{Status: " shipped ", Label: " Shipped ", Order: 3, Included: true},
		{Status: " open ", Label: " Under consideration ", Order: 1, Included: true},
		{Status: " planned ", Label: " Planned ", Order: 1, Included: true},
		{Status: " in_progress ", Label: " In progress ", Order: 2, Included: true},
		{Status: " cancelled ", Label: " Cancelled ", Order: 5, Included: false},
	})
	if err != nil {
		t.Fatalf("normalizeRoadmapStatusMappings(custom) error = %v", err)
	}
	for i, want := range []repo.RoadmapStatusMapping{
		{Status: "open", Label: "Under consideration", Order: 1, Included: true},
		{Status: "planned", Label: "Planned", Order: 2, Included: true},
		{Status: "in_progress", Label: "In progress", Order: 3, Included: true},
		{Status: "shipped", Label: "Shipped", Order: 4, Included: true},
		{Status: "cancelled", Label: "Cancelled", Order: 5, Included: false},
	} {
		if custom[i] != want {
			t.Fatalf("normalizeRoadmapStatusMappings(custom)[%d] = %#v, want %#v", i, custom[i], want)
		}
	}

	tests := []struct {
		name     string
		mappings []repo.RoadmapStatusMapping
	}{
		{
			name: "duplicate status",
			mappings: []repo.RoadmapStatusMapping{
				{Status: "open", Label: "Under consideration", Order: 1, Included: true},
				{Status: "open", Label: "Planned", Order: 2, Included: true},
				{Status: "in_progress", Label: "In progress", Order: 3, Included: true},
				{Status: "shipped", Label: "Shipped", Order: 4, Included: true},
				{Status: "cancelled", Label: "Cancelled", Order: 5, Included: false},
			},
		},
		{
			name: "duplicate label",
			mappings: []repo.RoadmapStatusMapping{
				{Status: "open", Label: "Planned", Order: 1, Included: true},
				{Status: "planned", Label: "planned", Order: 2, Included: true},
				{Status: "in_progress", Label: "In progress", Order: 3, Included: true},
				{Status: "shipped", Label: "Shipped", Order: 4, Included: true},
				{Status: "cancelled", Label: "Cancelled", Order: 5, Included: false},
			},
		},
		{
			name: "unknown status",
			mappings: []repo.RoadmapStatusMapping{
				{Status: "open", Label: "Under consideration", Order: 1, Included: true},
				{Status: "planned", Label: "Planned", Order: 2, Included: true},
				{Status: "draft", Label: "Draft", Order: 3, Included: true},
				{Status: "shipped", Label: "Shipped", Order: 4, Included: true},
				{Status: "cancelled", Label: "Cancelled", Order: 5, Included: false},
			},
		},
		{
			name: "zero order",
			mappings: []repo.RoadmapStatusMapping{
				{Status: "open", Label: "Under consideration", Order: 1, Included: true},
				{Status: "planned", Label: "Planned", Order: 0, Included: true},
				{Status: "in_progress", Label: "In progress", Order: 3, Included: true},
				{Status: "shipped", Label: "Shipped", Order: 4, Included: true},
				{Status: "cancelled", Label: "Cancelled", Order: 5, Included: false},
			},
		},
		{
			name: "wrong length",
			mappings: []repo.RoadmapStatusMapping{
				{Status: "open", Label: "Under consideration", Order: 1, Included: true},
				{Status: "planned", Label: "Planned", Order: 2, Included: true},
				{Status: "in_progress", Label: "In progress", Order: 3, Included: true},
				{Status: "shipped", Label: "Shipped", Order: 4, Included: true},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if _, err := normalizeRoadmapStatusMappings(tt.mappings); !errors.Is(err, ErrValidation) {
				t.Fatalf("normalizeRoadmapStatusMappings() error = %v, want %v", err, ErrValidation)
			}
		})
	}
}

func TestRoadmapStatusRank(t *testing.T) {
	t.Parallel()

	if got := roadmapStatusRank("planned"); got != 1 {
		t.Fatalf("roadmapStatusRank(planned) = %d, want 1", got)
	}
	if got := roadmapStatusRank("unknown"); got != len(roadmapStatusRanks) {
		t.Fatalf("roadmapStatusRank(unknown) = %d, want fallback %d", got, len(roadmapStatusRanks))
	}
}
