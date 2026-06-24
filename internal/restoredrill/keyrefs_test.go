// SPDX-License-Identifier: Apache-2.0

package restoredrill

import (
	"reflect"
	"testing"
)

func TestMissingKeyRefs(t *testing.T) {
	cases := []struct {
		name       string
		referenced []string
		live       []string
		want       []string
	}{
		{"all present", []string{"1", "2"}, []string{"1", "2", "3"}, nil},
		{"one missing", []string{"1", "9"}, []string{"1", "2"}, []string{"9"}},
		{"dedup + sort missing", []string{"5", "5", "9", "1"}, []string{"1"}, []string{"5", "9"}},
		{"empty referenced", nil, []string{"1"}, nil},
		{"empty live, all missing", []string{"7", "3"}, nil, []string{"3", "7"}},
		{"ignores empty key id", []string{"", "1"}, []string{"1"}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := missingKeyRefs(tc.referenced, tc.live)
			if len(got) == 0 && len(tc.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("missingKeyRefs(%v, %v) = %v, want %v", tc.referenced, tc.live, got, tc.want)
			}
		})
	}
}
