// Package database provides checksum calculation for migration integrity.
package database

import (
	"crypto/sha256"
	"fmt"
)

// Checksum returns the SHA-256 hex digest of migration file content.
// The result is a 64-character lowercase hex string.
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
