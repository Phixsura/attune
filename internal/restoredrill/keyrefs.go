// SPDX-License-Identifier: Apache-2.0

package restoredrill

import "sort"

// missingKeyRefs returns the deduplicated, sorted set of referenced Tink key
// ids that are absent from the live (enabled) keyset. Empty key ids are ignored
// (a row with no recorded key id is covered by full-population decryption, not
// here). This is the whole-population guard: a fast pre-check that catches
// keyset/restore drift before the full-population decryption runs.
func missingKeyRefs(referenced, live []string) []string {
	liveSet := make(map[string]bool, len(live))
	for _, k := range live {
		liveSet[k] = true
	}
	seen := make(map[string]bool)
	var missing []string
	for _, k := range referenced {
		if k == "" || liveSet[k] || seen[k] {
			continue
		}
		seen[k] = true
		missing = append(missing, k)
	}
	sort.Strings(missing)
	return missing
}
