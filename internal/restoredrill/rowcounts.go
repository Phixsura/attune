// SPDX-License-Identifier: Apache-2.0

package restoredrill

import (
	"fmt"
	"sort"
	"strings"
)

// rowCountsDetail is the non-sensitive evidence carried in the report: the
// per-table counts. Counts are not secret.
type rowCountsDetail struct {
	Restored map[string]int64 `json:"restored"`
	Baseline map[string]int64 `json:"baseline,omitempty"`
}

// rowCountFloor: a restore holding less than this fraction of live's rows is a
// suspicious shortfall — too large to explain by a normal RPO gap, more likely
// partial data loss. Graded WARN (not FAIL) because, without the backup age and
// write rate, the drill cannot be certain it is loss vs. a long RPO window.
const rowCountFloor = 0.5

// evaluateRowCounts grades the restored core-table counts against a live
// baseline. A restore is older than live by construction, so the assertion is
// an RPO band, not equality:
//
//   - A table that has rows in live but is EMPTY in the restore is data loss → fail.
//   - A table whose restored count is FAR below live (< rowCountFloor) is a
//     suspicious shortfall → warn (possible partial data loss).
//   - A table whose restored count EXCEEDS live means the backup is newer than
//     the baseline (or live was pruned) → warn.
//   - Otherwise the counts are within band → pass.
//
// Without a baseline the check is skipped: an empty table is not corruption on
// its own, so there is nothing to assert.
func evaluateRowCounts(restored, baseline map[string]int64) CheckResult {
	r := CheckResult{
		Name:   "row_counts",
		Detail: rowCountsDetail{Restored: restored, Baseline: baseline},
	}
	if baseline == nil {
		r.Status = StatusSkip
		r.Message = "no live baseline supplied; pass --baseline-url to compare restored counts against live"
		return r
	}

	var lost, short, over []string
	for t, b := range baseline {
		rc := restored[t]
		switch {
		case b > 0 && rc == 0:
			lost = append(lost, t)
		case b > 0 && float64(rc) < float64(b)*rowCountFloor:
			short = append(short, t)
		case rc > b:
			over = append(over, t)
		}
	}
	if len(lost) > 0 {
		sort.Strings(lost)
		r.Status = StatusFail
		r.Message = fmt.Sprintf("data loss: live has rows but the restore is empty for: %s",
			strings.Join(lost, ", "))
		return r
	}
	if len(short) > 0 {
		sort.Strings(short)
		r.Status = StatusWarn
		r.Message = fmt.Sprintf("restored row count is far below live (< %.0f%%) for: %s — possible partial data loss or a large RPO gap; verify",
			rowCountFloor*100, strings.Join(short, ", "))
		return r
	}
	if len(over) > 0 {
		sort.Strings(over)
		r.Status = StatusWarn
		r.Message = fmt.Sprintf("restored row count exceeds live baseline for: %s "+
			"(backup newer than the baseline, or live was pruned)", strings.Join(over, ", "))
		return r
	}

	r.Status = StatusPass
	r.Message = fmt.Sprintf("restored counts within RPO band of live across %d table(s)", len(baseline))
	return r
}
