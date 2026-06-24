// SPDX-License-Identifier: Apache-2.0

package restoredrill

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// VerifyBackupArtifact verifies a pg_basebackup directory's integrity with
// pg_verifybackup — it checks every file against the backup_manifest's checksums
// and sizes, catching a corrupt or incomplete backup artifact BEFORE any restore
// is attempted (the pre-restore tier of the drill). Requires pg_verifybackup in
// the runtime.
//
// The directory and its backup_manifest are validated first, so an obviously
// bad artifact fails fast with a clear error instead of an opaque subprocess one.
func VerifyBackupArtifact(ctx context.Context, backupDir string) error {
	fi, err := os.Stat(backupDir)
	if err != nil {
		return fmt.Errorf("backup directory %q: %w", backupDir, err)
	}
	if !fi.IsDir() {
		return fmt.Errorf("backup path %q is not a directory", backupDir)
	}
	mi, err := os.Stat(filepath.Join(backupDir, "backup_manifest"))
	if err != nil {
		return fmt.Errorf("no backup_manifest in %q (not a pg_basebackup directory): %w", backupDir, err)
	}
	if mi.Size() == 0 {
		return fmt.Errorf("backup_manifest in %q is empty (incomplete backup)", backupDir)
	}
	out, err := exec.CommandContext(ctx, "pg_verifybackup", backupDir).CombinedOutput()
	if err != nil {
		return fmt.Errorf("pg_verifybackup: %w: %s", err, lastLines(string(out), 5))
	}
	return nil
}
