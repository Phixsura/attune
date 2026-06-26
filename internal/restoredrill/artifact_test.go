// SPDX-License-Identifier: Apache-2.0

package restoredrill

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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

// TestVerifyBackupArtifact_EmptyManifest: a directory with an empty
// backup_manifest file is rejected (incomplete backup).
func TestVerifyBackupArtifact_EmptyManifest(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "backup_manifest"), []byte{}, 0o600); err != nil {
		t.Fatal(err)
	}
	err := VerifyBackupArtifact(context.Background(), dir)
	if err == nil {
		t.Fatal("expected error for an empty backup_manifest")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Fatalf("error %q should mention 'empty'", err)
	}
}
