// SPDX-License-Identifier: Apache-2.0

package restoredrill

import (
	"fmt"
	"time"
)

// DefaultFreshnessWindow is how recent a passing restore drill must be to
// count as healthy in readiness and recovery surfaces.
const DefaultFreshnessWindow = 7 * 24 * time.Hour

// RecoveryAssessment summarizes the latest recoverability state for a restored
// database.
type RecoveryAssessment struct {
	Status      Status `json:"status"`
	Message     string `json:"message"`
	Remediation string `json:"remediation,omitempty"`
}

// AssessLastRun grades the latest restore drill result using the same policy as
// the readiness surface. The helper is shared by preflight and Console so the
// operator sees one contract everywhere.
func AssessLastRun(ok bool, last LastRun, age, freshness time.Duration) RecoveryAssessment {
	r := RecoveryAssessment{}
	if !ok {
		r.Status = StatusWarn
		r.Message = "No restore drill has been recorded yet"
		r.Remediation = "Run a drill: attune restore-drill run --target <restored-db-url> --record. See docs/private-deploy.md."
		return r
	}
	switch last.Status {
	case StatusPass:
		// graded by freshness below — the only path to a healthy recovery state
	case StatusFail:
		r.Status = StatusFail
		r.Message = fmt.Sprintf("Last restore drill FAILED (%s)", agoString(age))
		r.Remediation = "Inspect with 'attune restore-drill status'; the latest backup may not be recoverable."
		return r
	case StatusWarn:
		r.Status = StatusWarn
		r.Message = fmt.Sprintf("Last restore drill passed with warnings (%s)", agoString(age))
		r.Remediation = "Inspect with 'attune restore-drill status'."
		return r
	default:
		// StatusSkip or any unrecognized status: the drill did NOT establish
		// recoverability, so it must not be reported as healthy.
		r.Status = StatusWarn
		r.Message = fmt.Sprintf("Last restore drill did not verify recoverability (status %q, %s)", last.Status, agoString(age))
		r.Remediation = "Run a drill that verifies a restored target: attune restore-drill run --target <restored-db-url> --record."
		return r
	}
	if age < 0 {
		r.Status = StatusWarn
		r.Message = "Last restore drill has a future timestamp — clock skew or bad ran_at data"
		r.Remediation = "Check the recorder's clock; inspect with 'attune restore-drill status'."
		return r
	}
	if age > freshness {
		r.Status = StatusWarn
		r.Message = fmt.Sprintf("Last restore drill passed but is stale (%s)", agoString(age))
		r.Remediation = "Run a fresh drill: attune restore-drill run --target <restored-db-url> --record."
		return r
	}
	r.Status = StatusPass
	r.Message = fmt.Sprintf("Last restore drill passed (%s)", agoString(age))
	return r
}

func agoString(age time.Duration) string {
	days := int(age.Hours() / 24)
	switch {
	case days <= 0:
		return "today"
	case days == 1:
		return "1 day ago"
	default:
		return fmt.Sprintf("%d days ago", days)
	}
}
