// SPDX-License-Identifier: Apache-2.0

package restoredrill

import (
	"fmt"
	"strings"
	"time"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// recoveryDetail is the audit-grade RPO/RTO evidence in the report.
type recoveryDetail struct {
	RPOSeconds       *int64 `json:"rpo_seconds,omitempty"`
	RTOSeconds       *int64 `json:"rto_seconds,omitempty"`
	RPOTargetSeconds *int64 `json:"rpo_target_seconds,omitempty"`
	RTOTargetSeconds *int64 `json:"rto_target_seconds,omitempty"`
}

// evaluateRecoveryObjectives grades the measured recovery point objective (RPO —
// the data-loss window, i.e. how old the backup is) and recovery time objective
// (RTO — how long the restore took) against operator targets. Breaching a target
// is a warning: the data WAS recovered, just outside the stated SLA. Without any
// measurement the check is skipped.
func evaluateRecoveryObjectives(rpo, rto *time.Duration, rpoTarget, rtoTarget time.Duration) CheckResult {
	r := CheckResult{Name: "recovery_objectives"}
	if rpo == nil && rto == nil {
		r.Status = StatusSkip
		r.Message = "no recovery objectives measured (pass --backup-taken-at and/or --restore-duration)"
		return r
	}
	r.Detail = recoveryDetail{
		RPOSeconds:       secondsPtr(rpo),
		RTOSeconds:       secondsPtr(rto),
		RPOTargetSeconds: secondsOf(rpoTarget),
		RTOTargetSeconds: secondsOf(rtoTarget),
	}

	var breaches []string
	if rpo != nil && rpoTarget > 0 && ptrext.Indirect(rpo) > rpoTarget {
		breaches = append(breaches, fmt.Sprintf("RPO %s exceeds target %s", humanDur(ptrext.Indirect(rpo)), humanDur(rpoTarget)))
	}
	if rto != nil && rtoTarget > 0 && ptrext.Indirect(rto) > rtoTarget {
		breaches = append(breaches, fmt.Sprintf("RTO %s exceeds target %s", humanDur(ptrext.Indirect(rto)), humanDur(rtoTarget)))
	}
	if len(breaches) > 0 {
		r.Status = StatusWarn
		r.Message = strings.Join(breaches, "; ")
		return r
	}

	r.Status = StatusPass
	r.Message = "recovery objectives met: " + recoverySummary(rpo, rto)
	return r
}

func recoverySummary(rpo, rto *time.Duration) string {
	parts := make([]string, 0, 2)
	if rpo != nil {
		parts = append(parts, "RPO "+humanDur(ptrext.Indirect(rpo)))
	}
	if rto != nil {
		parts = append(parts, "RTO "+humanDur(ptrext.Indirect(rto)))
	}
	return strings.Join(parts, ", ")
}

func humanDur(d time.Duration) string {
	return d.Round(time.Second).String()
}

// secondsPtr reports a MEASURED duration (nil = not measured). A measured value
// is always reported, rounded to whole seconds and never collapsed to nil, so
// the persisted RPO/RTO matches the rounded human-readable message and a real
// sub-second restore is not mistaken for "never measured".
func secondsPtr(d *time.Duration) *int64 {
	if d == nil {
		return nil
	}
	secs := int64(ptrext.Indirect(d).Round(time.Second) / time.Second)
	if secs < 0 {
		secs = 0
	}
	return ptrext.Of(secs)
}

// secondsOf reports a TARGET duration; 0 (or less) means "no target" → nil.
func secondsOf(d time.Duration) *int64 {
	if d <= 0 {
		return nil
	}
	return ptrext.Of(int64(d.Round(time.Second) / time.Second))
}
