// SPDX-License-Identifier: Apache-2.0

package restoredrill

import "sort"

// missingKeyRefs returns the deduplicated, sorted set of referenced Tink key
// ids that are absent from the live keyset. Empty key ids are ignored (a row
// with no recorded key id is validated by sample decryption, not here). This is
// the whole-population guard: it catches keyset/restore drift even in rows the
// sampler never touches.
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
