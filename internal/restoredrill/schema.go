// SPDX-License-Identifier: Apache-2.0

package restoredrill

import (
	"errors"
	"fmt"

	"github.com/Phixsura/attune/internal/infra/database"
)

// schemaInputs is the raw migration-state evidence gathered from the restored
// target database, fed to the pure classifier. Keeping classification separate
// from data-fetch lets the version-skew logic be unit-tested without a DB.
type schemaInputs struct {
	Total   int
	Applied int
	Pending int
	// MaxApplied is the highest migration version in the restored DB's tracker
	// table — read directly, independent of this binary and of the checksum
	// ledger, so a future migration with an empty checksum is still detected.
	MaxApplied int
	// ChecksumErr is nil, database.ErrChecksumDrift, or database.ErrMissingFile.
	ChecksumErr error
	// DirtyErr is nil or database.ErrDirtyMigration.
	DirtyErr error
	// ManifestErr is nil or database.ErrManifestReorder. Only meaningful when
	// Pending == 0; an older backup legitimately mismatches the binary manifest.
	ManifestErr error
}

// schemaDetail is the structured, non-sensitive evidence carried in the report.
type schemaDetail struct {
	Total   int `json:"total"`
	Applied int `json:"applied"`
	Pending int `json:"pending"`
}

func classifySchema(in schemaInputs) CheckResult {
	r := CheckResult{
		Name:   "schema",
		Detail: schemaDetail{Total: in.Total, Applied: in.Applied, Pending: in.Pending},
	}

	// A half-applied migration in the restore is the hardest failure: the
	// backup captured an interrupted migration.
	var dirty database.ErrDirtyMigration
	if errors.As(in.DirtyErr, &dirty) {
		r.Status = StatusFail
		r.Message = fmt.Sprintf("restored DB has a dirty migration: version %d (%s) started but did not complete",
			dirty.Version, dirty.Filename)
		return r
	}
	if in.DirtyErr != nil {
		r.Status = StatusFail
		r.Message = "could not verify migration dirty state on the restored DB"
		return r
	}

	// An applied migration whose file is absent from this binary means the
	// backup was taken by a NEWER attune than the one running the drill.
	var missing database.ErrMissingFile
	if errors.As(in.ChecksumErr, &missing) {
		r.Status = StatusFail
		r.Message = fmt.Sprintf("restored DB applied migration %d (%s) that this binary does not carry — "+
			"the backup was taken by a newer attune; run the drill with a matching or newer binary",
			missing.Version, missing.Filename)
		return r
	}
	// A stored checksum that no longer matches the binary's file means the
	// restored migration content differs from this build — corruption/tamper.
	var drift database.ErrChecksumDrift
	if errors.As(in.ChecksumErr, &drift) {
		r.Status = StatusFail
		r.Message = fmt.Sprintf("checksum drift on %d applied migration(s) in the restored DB — "+
			"restored migration content differs from this binary", len(drift.Drifted))
		return r
	}
	if in.ChecksumErr != nil {
		r.Status = StatusFail
		r.Message = "could not verify migration checksums on the restored DB"
		return r
	}

	// Independent of the checksum ledger (which inspects only non-empty-checksum
	// rows), a restored DB whose highest applied version exceeds what this binary
	// carries was taken by a NEWER attune — catches the empty-checksum future row
	// that would otherwise slip past ErrMissingFile and be reported healthy.
	if in.MaxApplied > in.Total {
		r.Status = StatusFail
		r.Message = fmt.Sprintf("restored DB is at migration version %d but this binary carries only %d — "+
			"the backup was taken by a newer attune; run the drill with a matching or newer binary",
			in.MaxApplied, in.Total)
		return r
	}

	// Pending migrations mean the backup predates this binary. The backup is
	// still recoverable — attune applies the pending migrations on boot — so
	// this is a warning, not a failure. The manifest hash legitimately
	// mismatches in this case (the binary carries more files), so it is ignored.
	if in.Pending > 0 {
		r.Status = StatusWarn
		r.Message = fmt.Sprintf("backup predates this binary by %d migration(s) (restored at version %d/%d); "+
			"they apply automatically on boot", in.Pending, in.Applied, in.Total)
		return r
	}

	// Same version: a manifest mismatch now means genuine reordering.
	var reorder database.ErrManifestReorder
	if errors.As(in.ManifestErr, &reorder) {
		r.Status = StatusFail
		r.Message = "migration manifest hash mismatch on the restored DB — migrations were reordered since backup"
		return r
	}
	if in.ManifestErr != nil {
		r.Status = StatusFail
		r.Message = "could not verify migration manifest on the restored DB"
		return r
	}

	r.Status = StatusPass
	r.Message = fmt.Sprintf("schema current and verified: %d/%d migrations applied, checksums + manifest OK",
		in.Applied, in.Total)
	return r
}
