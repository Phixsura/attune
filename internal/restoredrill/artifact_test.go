// SPDX-License-Identifier: Apache-2.0

package restoredrill

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestVerifyBackupArtifact_BadDir: a non-existent directory is rejected before
// pg_verifybackup is invoked, so this runs without the tool installed. The real
// pg_verifybackup round-trip (clean + corrupted backup) is the integration test.
func TestVerifyBackupArtifact_BadDir(t *testing.T) {
	if err := VerifyBackupArtifact(context.Background(), filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("expected error for a non-existent backup directory")
	}
}

// TestVerifyBackupArtifact_NoManifest: an existing directory without a
// backup_manifest is not a pg_basebackup artifact and is rejected up front.
func TestVerifyBackupArtifact_NoManifest(t *testing.T) {
	if err := VerifyBackupArtifact(context.Background(), t.TempDir()); err == nil {
		t.Fatal("expected error for a directory with no backup_manifest")
	}
}

// TestVerifyBackupArtifact_NotADir: a regular file is rejected up front.
func TestVerifyBackupArtifact_NotADir(t *testing.T) {
	f := filepath.Join(t.TempDir(), "afile")
	if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := VerifyBackupArtifact(context.Background(), f); err == nil {
		t.Fatal("expected error for a non-directory path")
	}
}
