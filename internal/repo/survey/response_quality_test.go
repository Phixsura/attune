// SPDX-License-Identifier: Apache-2.0

package survey

import "testing"

func TestResponseQualityStatusDefaultsLegacyMetadataToObserved(t *testing.T) {
	t.Parallel()

	if got := ResponseQualityStatus(map[string]any{}); got != ResponseQualityStatusObserved {
		t.Fatalf("ResponseQualityStatus() = %q, want observed", got)
	}
	if got := ResponseQualityStatus(map[string]any{"response_quality_status": "unexpected"}); got != ResponseQualityStatusObserved {
		t.Fatalf("ResponseQualityStatus(unexpected) = %q, want observed", got)
	}
}

func TestResponseQualityReasonsNormalizesJSONValues(t *testing.T) {
	t.Parallel()

	got := ResponseQualityReasons(map[string]any{
		"response_quality_reasons": []any{"automated_client", "", "automated_client", "submitted_too_quickly"},
	})
	want := []string{"automated_client", "submitted_too_quickly"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("ResponseQualityReasons() = %#v, want %#v", got, want)
	}
}
