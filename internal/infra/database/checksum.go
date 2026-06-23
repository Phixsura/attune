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
