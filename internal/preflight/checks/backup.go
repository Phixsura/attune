package checks

import (
	"context"
	"fmt"
	"time"

	"github.com/Phixsura/attune/internal/preflight"
	"github.com/Phixsura/attune/internal/restoredrill"
)

func init() {
	preflight.Register(preflight.Check{
		Name:     "backup:restore_drill",
		Category: preflight.CategoryBackup,
		Run:      checkRestoreDrill,
	})
}

const restoreDrillFreshness = restoredrill.DefaultFreshnessWindow

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
	grade := restoredrill.AssessLastRun(ok, last, time.Since(last.RanAt), restoreDrillFreshness)
	return preflight.Result{
		Name:        "backup:restore_drill",
		Category:    preflight.CategoryBackup,
		Status:      preflight.Status(grade.Status),
		Message:     grade.Message,
		Remediation: grade.Remediation,
	}
}

func gradeRestoreDrill(ok bool, status restoredrill.Status, age, freshness time.Duration) preflight.Result {
	grade := restoredrill.AssessLastRun(ok, restoredrill.LastRun{Status: status}, age, freshness)
	return preflight.Result{
		Name:        "backup:restore_drill",
		Category:    preflight.CategoryBackup,
		Status:      preflight.Status(grade.Status),
		Message:     grade.Message,
		Remediation: grade.Remediation,
	}
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
