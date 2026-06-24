// Package database provides checksum calculation for migration integrity.
package database

import (
	"crypto/sha256"
	"fmt"
)

// Checksum returns the SHA-256 hex digest of migration file content.
// The result is a 64-character lowercase hex string.
//
// Algorithm choice: SHA-256 (vs CRC32/MD5) provides cryptographic strength
// for tamper detection. Prisma and Atlas also use SHA-256. The checksum is
// computed from raw file bytes, so whitespace changes WILL break checksums.
func Checksum(body []byte) string {
	return fmt.Sprintf("%x", sha256.Sum256(body))
}

// ChecksumShort returns truncated checksum for display (12 chars + "...").
func ChecksumShort(checksum string) string {
	if len(checksum) <= 12 {
		return checksum
	}
	return checksum[:12] + "..."
}

// ComputeChecksum calculates the checksum for an embedded migration file.
func ComputeChecksum(filename string) (string, error) {
	body, err := migrationFS.ReadFile("migrations/" + filename)
	if err != nil {
		return "", fmt.Errorf("read migration file: %w", err)
	}
	return Checksum(body), nil
}

// ManifestHash computes a single hash over the entire migration set that
// changes if ANY file is modified, added, deleted, OR reordered.
//
// Algorithm: SHA-256 of concatenated (filename + ":" + file_checksum + "\n")
// for each file in order. This creates a linear hash chain where order matters,
// similar to Atlas's approach.
//
// The manifest hash is stored after all migrations are applied and verified on
// startup. A mismatch where individual checksums pass indicates reordering.
func ManifestHash(names []string) (string, error) {
	h := sha256.New()
	for _, name := range names {
		body, err := migrationFS.ReadFile("migrations/" + name)
		if err != nil {
			return "", fmt.Errorf("read %s: %w", name, err)
		}
		fileChecksum := Checksum(body)
		// Write "filename:checksum\n" to create order-dependent hash
		h.Write([]byte(name))
		h.Write([]byte(":"))
		h.Write([]byte(fileChecksum))
		h.Write([]byte("\n"))
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}
