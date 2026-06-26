// SPDX-License-Identifier: Apache-2.0

package embedding

import (
	"math"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func Test_normalizeClusterOpts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   ClusterListOpts
		want ClusterListOpts
	}{
		{
			name: "all zero gets defaults",
			in:   ClusterListOpts{},
			want: ClusterListOpts{RecencyDays: 30, MinCount: 2, Limit: 50, Sort: "latest_at"},
		},
		{
			name: "RecencyDays preserved when non-zero",
			in:   ClusterListOpts{RecencyDays: 7},
			want: ClusterListOpts{RecencyDays: 7, MinCount: 2, Limit: 50, Sort: "latest_at"},
		},
		{
			name: "MinCount preserved when non-zero",
			in:   ClusterListOpts{MinCount: 5},
			want: ClusterListOpts{RecencyDays: 30, MinCount: 5, Limit: 50, Sort: "latest_at"},
		},
		{
			name: "Limit in range preserved",
			in:   ClusterListOpts{Limit: 100},
			want: ClusterListOpts{RecencyDays: 30, MinCount: 2, Limit: 100, Sort: "latest_at"},
		},
		{
			name: "Limit clamped to 500",
			in:   ClusterListOpts{Limit: 999},
			want: ClusterListOpts{RecencyDays: 30, MinCount: 2, Limit: 500, Sort: "latest_at"},
		},
		{
			name: "Limit exactly 500 preserved",
			in:   ClusterListOpts{Limit: 500},
			want: ClusterListOpts{RecencyDays: 30, MinCount: 2, Limit: 500, Sort: "latest_at"},
		},
		{
			name: "Sort count preserved",
			in:   ClusterListOpts{Sort: "count"},
			want: ClusterListOpts{RecencyDays: 30, MinCount: 2, Limit: 50, Sort: "count"},
		},
		{
			name: "Sort latest_at preserved",
			in:   ClusterListOpts{Sort: "latest_at"},
			want: ClusterListOpts{RecencyDays: 30, MinCount: 2, Limit: 50, Sort: "latest_at"},
		},
		{
			name: "Sort invalid falls back to latest_at",
			in:   ClusterListOpts{Sort: "invalid"},
			want: ClusterListOpts{RecencyDays: 30, MinCount: 2, Limit: 50, Sort: "latest_at"},
		},
		{
			name: "Cursor and Query are pass-through",
			in:   ClusterListOpts{Cursor: "abc", Query: "search term"},
			want: ClusterListOpts{RecencyDays: 30, MinCount: 2, Limit: 50, Sort: "latest_at", Cursor: "abc", Query: "search term"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := normalizeClusterOpts(tt.in)
			require.Equal(t, tt.want, got)
		})
	}
}

func Test_parseClusterCursor(t *testing.T) {
	t.Parallel()

	validUUID := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")

	tests := []struct {
		name      string
		cursor    string
		wantTime  time.Time
		wantUUID  uuid.UUID
		wantError bool
	}{
		{
			name:     "empty cursor returns zero values",
			cursor:   "",
			wantTime: time.Time{},
			wantUUID: uuid.Nil,
		},
		{
			name:      "no colon returns error",
			cursor:    "invalid",
			wantError: true,
		},
		{
			name:      "bad timestamp returns error",
			cursor:    "abc:550e8400-e29b-41d4-a716-446655440000",
			wantError: true,
		},
		{
			name:      "bad uuid returns error",
			cursor:    "123:not-a-uuid",
			wantError: true,
		},
		{
			name:     "valid cursor parses correctly",
			cursor:   "1719500000000000000:550e8400-e29b-41d4-a716-446655440000",
			wantTime: time.Unix(0, 1719500000000000000),
			wantUUID: validUUID,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotTime, gotUUID, err := parseClusterCursor(tt.cursor)
			if tt.wantError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantTime, gotTime)
			require.Equal(t, tt.wantUUID, gotUUID)
		})
	}
}

func Test_normalizeMemberOpts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   ClusterMembersOpts
		want ClusterMembersOpts
	}{
		{
			name: "Limit zero gets default 50",
			in:   ClusterMembersOpts{},
			want: ClusterMembersOpts{Limit: 50},
		},
		{
			name: "Limit over 500 clamped",
			in:   ClusterMembersOpts{Limit: 600},
			want: ClusterMembersOpts{Limit: 500},
		},
		{
			name: "Limit in range preserved",
			in:   ClusterMembersOpts{Limit: 200},
			want: ClusterMembersOpts{Limit: 200},
		},
		{
			name: "Cursor is pass-through",
			in:   ClusterMembersOpts{Limit: 10, Cursor: "some-cursor"},
			want: ClusterMembersOpts{Limit: 10, Cursor: "some-cursor"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := normalizeMemberOpts(tt.in)
			require.Equal(t, tt.want, got)
		})
	}
}

func Test_parseMemberCursor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		cursor    string
		wantTime  time.Time
		wantID    int64
		wantError bool
	}{
		{
			name:     "empty cursor returns zero values",
			cursor:   "",
			wantTime: time.Time{},
			wantID:   0,
		},
		{
			name:      "no colon returns error",
			cursor:    "invalid",
			wantError: true,
		},
		{
			name:      "bad timestamp returns error",
			cursor:    "abc:123",
			wantError: true,
		},
		{
			name:      "bad id returns error",
			cursor:    "123:abc",
			wantError: true,
		},
		{
			name:     "valid cursor parses correctly",
			cursor:   "1719500000000000000:42",
			wantTime: time.Unix(0, 1719500000000000000),
			wantID:   42,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotTime, gotID, err := parseMemberCursor(tt.cursor)
			if tt.wantError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantTime, gotTime)
			require.Equal(t, tt.wantID, gotID)
		})
	}
}

func Test_parseVectorText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		want      []float32
		wantError bool
	}{
		{
			name:  "empty brackets returns nil",
			input: "[]",
			want:  nil,
		},
		{
			name:  "empty string returns nil",
			input: "",
			want:  nil,
		},
		{
			name:  "three elements",
			input: "[1.0,2.0,3.0]",
			want:  []float32{1.0, 2.0, 3.0},
		},
		{
			name:  "single element",
			input: "[1.5]",
			want:  []float32{1.5},
		},
		{
			name:      "non-numeric element returns error",
			input:     "[abc]",
			wantError: true,
		},
		{
			name:      "NaN returns error",
			input:     "[NaN]",
			wantError: true,
		},
		{
			name:      "Inf returns error",
			input:     "[Inf]",
			wantError: true,
		},
		{
			name:      "negative Inf returns error",
			input:     "[-Inf]",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseVectorText(tt.input)
			if tt.wantError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			if tt.want == nil {
				require.Nil(t, got)
				return
			}
			require.Len(t, got, len(tt.want))
			for i := range tt.want {
				require.False(t, math.IsNaN(float64(got[i])), "element %d is NaN", i)
				require.InDelta(t, tt.want[i], got[i], 1e-6, "element %d", i)
			}
		})
	}
}
