// SPDX-License-Identifier: Apache-2.0

package restoredrill

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestVerifyBackupArtifact_BadDir confirms the function actually invokes
// pg_verifybackup and surfaces its failure. Skips where the tool is absent (CI
// without PostgreSQL client tools). The happy path needs a real pg_basebackup
// directory — exercised by the integration test where the server permits it.
func TestVerifyBackupArtifact_BadDir(t *testing.T) {
	if _, err := exec.LookPath("pg_verifybackup"); err != nil {
		t.Skip("pg_verifybackup not in PATH")
	}
	if err := VerifyBackupArtifact(context.Background(), filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("expected error for a non-existent backup directory")
	}
}
