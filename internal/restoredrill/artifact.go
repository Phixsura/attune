// SPDX-License-Identifier: Apache-2.0

package restoredrill

import (
	"context"
	"fmt"
	"os/exec"
)

// VerifyBackupArtifact verifies a pg_basebackup directory's integrity with
// pg_verifybackup — it checks every file against the backup_manifest's checksums
// and sizes, catching a corrupt or incomplete backup artifact BEFORE any restore
// is attempted (the pre-restore tier of the drill). Requires pg_verifybackup in
// the runtime.
func VerifyBackupArtifact(ctx context.Context, backupDir string) error {
	out, err := exec.CommandContext(ctx, "pg_verifybackup", backupDir).CombinedOutput()
	if err != nil {
		return fmt.Errorf("pg_verifybackup: %w: %s", err, lastLines(string(out), 5))
	}
	return nil
}
