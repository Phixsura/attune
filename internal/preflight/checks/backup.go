package checks

import (
	"context"
	"fmt"
	"time"

	"github.com/Phixsura/attune/internal/preflight"
	"github.com/Phixsura/attune/internal/restoredrill"
)

// restoreDrillFreshness is how recent a passing drill must be to count as
// healthy. A passing-but-older drill degrades to a warning.
const restoreDrillFreshness = 7 * 24 * time.Hour

func init() {
	preflight.Register(preflight.Check{
		Name:     "backup:restore_drill",
		Category: preflight.CategoryBackup,
		Run:      checkRestoreDrill,
	})
}

// checkRestoreDrill reads the most recent recorded restore drill and grades it.
// It does NOT run a drill — the drill is a heavyweight, out-of-band operation
// (attune restore-drill run); this surface only reports its latest result.
func checkRestoreDrill(ctx context.Context, env *preflight.Environment) preflight.Result {
	if env.Pool == nil {
		return preflight.Result{
			Name:     "backup:restore_drill",
			Category: preflight.CategoryBackup,
			Status:   preflight.StatusSkipped,
			Message:  "Database pool not available",
		}
	}
	last, ok, err := restoredrill.ReadLast(ctx, env.Pool)
	if err != nil {
		return preflight.Result{
			Name:     "backup:restore_drill",
			Category: preflight.CategoryBackup,
			Status:   preflight.StatusSkipped,
			Message:  "Restore-drill history unavailable",
		}
	}
	return gradeRestoreDrill(ok, last.Status, time.Since(last.RanAt), restoreDrillFreshness)
}

// gradeRestoreDrill maps the latest recorded drill into a readiness result.
// Separated from the DB read so the grading is unit-testable without a database.
func gradeRestoreDrill(ok bool, status restoredrill.Status, age, freshness time.Duration) preflight.Result {
	r := preflight.Result{Name: "backup:restore_drill", Category: preflight.CategoryBackup}
	if !ok {
		r.Status = preflight.StatusWarn
		r.Message = "No restore drill has been recorded yet"
		r.Remediation = "Run a drill: attune restore-drill run --target <restored-db-url> --record. See docs/private-deploy.md."
		return r
	}
	switch status {
	case restoredrill.StatusPass:
		// graded by freshness below — the only path to a readiness PASS
	case restoredrill.StatusFail:
		r.Status = preflight.StatusFail
		r.Message = fmt.Sprintf("Last restore drill FAILED (%s)", agoString(age))
		r.Remediation = "Inspect with 'attune restore-drill status'; the latest backup may not be recoverable."
		return r
	case restoredrill.StatusWarn:
		r.Status = preflight.StatusWarn
		r.Message = fmt.Sprintf("Last restore drill passed with warnings (%s)", agoString(age))
		r.Remediation = "Inspect with 'attune restore-drill status'."
		return r
	default:
		// StatusSkip or any unrecognized status: the drill did NOT establish
		// recoverability, so it must not be reported as a pass.
		r.Status = preflight.StatusWarn
		r.Message = fmt.Sprintf("Last restore drill did not verify recoverability (status %q, %s)", status, agoString(age))
		r.Remediation = "Run a drill that verifies a restored target: attune restore-drill run --target <restored-db-url> --record."
		return r
	}
	if age < 0 {
		r.Status = preflight.StatusWarn
		r.Message = "Last restore drill has a future timestamp — clock skew or bad ran_at data"
		r.Remediation = "Check the recorder's clock; inspect with 'attune restore-drill status'."
		return r
	}
	if age > freshness {
		r.Status = preflight.StatusWarn
		r.Message = fmt.Sprintf("Last restore drill passed but is stale (%s)", agoString(age))
		r.Remediation = "Run a fresh drill: attune restore-drill run --target <restored-db-url> --record."
		return r
	}
	r.Status = preflight.StatusPass
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
