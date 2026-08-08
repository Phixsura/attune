// SPDX-License-Identifier: Apache-2.0

package survey

import "strings"

const (
	ResponseQualityMetadataVersion = "v1"
	ResponseQualityStatusObserved  = "observed"
	ResponseQualityStatusFlagged   = "flagged"

	responseQualityStatusKey  = "response_quality_status"
	responseQualityReasonsKey = "response_quality_reasons"
)

// ResponseQualityStatus reads the stable, non-PII quality status persisted on
// public survey responses. Unknown values are treated as observed so legacy
// responses remain visible without being silently reclassified.
func ResponseQualityStatus(metadata map[string]any) string {
	status, _ := metadata[responseQualityStatusKey].(string)
	status = strings.TrimSpace(status)
	if status == ResponseQualityStatusFlagged {
		return status
	}
	return ResponseQualityStatusObserved
}

// ResponseQualityReasons returns the bounded reason codes attached by the
// public response quality evaluator. It intentionally exposes codes only.
func ResponseQualityReasons(metadata map[string]any) []string {
	raw, ok := metadata[responseQualityReasonsKey].([]any)
	if !ok {
		if typed, typedOK := metadata[responseQualityReasonsKey].([]string); typedOK {
			return cleanResponseQualityReasons(typed)
		}
		return nil
	}
	reasons := make([]string, 0, len(raw))
	for _, value := range raw {
		if reason, ok := value.(string); ok {
			reasons = append(reasons, reason)
		}
	}
	return cleanResponseQualityReasons(reasons)
}

func cleanResponseQualityReasons(reasons []string) []string {
	seen := make(map[string]struct{}, len(reasons))
	out := make([]string, 0, len(reasons))
	for _, reason := range reasons {
		reason = strings.TrimSpace(reason)
		if reason == "" {
			continue
		}
		if _, ok := seen[reason]; ok {
			continue
		}
		seen[reason] = struct{}{}
		out = append(out, reason)
	}
	return out
}
