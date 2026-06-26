// SPDX-License-Identifier: Apache-2.0

package llmaudit

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_validGranularity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		g    UsageGranularity
		want bool
	}{
		{name: "day", g: GranularityDay, want: true},
		{name: "week", g: GranularityWeek, want: true},
		{name: "month", g: GranularityMonth, want: true},
		{name: "year_invalid", g: "year", want: false},
		{name: "empty", g: "", want: false},
		{name: "uppercase_DAY", g: "DAY", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, validGranularity(tt.g))
		})
	}
}

func Test_nonNegativeI32(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		n    int32
		want int32
	}{
		{name: "negative_100", n: -100, want: 0},
		{name: "negative_1", n: -1, want: 0},
		{name: "zero", n: 0, want: 0},
		{name: "positive_1", n: 1, want: 1},
		{name: "positive_100", n: 100, want: 100},
		{name: "max_int32", n: math.MaxInt32, want: math.MaxInt32},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, nonNegativeI32(tt.n))
		})
	}
}

func Test_nonNegativeInt(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		n    int
		want int
	}{
		{name: "negative_100", n: -100, want: 0},
		{name: "negative_1", n: -1, want: 0},
		{name: "zero", n: 0, want: 0},
		{name: "positive_1", n: 1, want: 1},
		{name: "positive_100", n: 100, want: 100},
		{name: "max_int", n: math.MaxInt, want: math.MaxInt},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, nonNegativeInt(tt.n))
		})
	}
}

func Test_nonNegativeInt64(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		n    int64
		want int64
	}{
		{name: "negative_100", n: -100, want: 0},
		{name: "negative_1", n: -1, want: 0},
		{name: "zero", n: 0, want: 0},
		{name: "positive_1", n: 1, want: 1},
		{name: "positive_100", n: 100, want: 100},
		{name: "max_int64", n: math.MaxInt64, want: math.MaxInt64},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, nonNegativeInt64(tt.n))
		})
	}
}
