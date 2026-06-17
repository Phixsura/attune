// SPDX-License-Identifier: Apache-2.0

// Package subjectkey owns tenant-scoped end-user subject identity helpers used
// by GDPR export/delete and the ingest path.
package subjectkey

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// Normalize returns the canonical subject key and display string for one
// end-user subject. New ingest prefers the raw upstream subject identifier; old
// rows may fall back to the legacy composed user id.
func Normalize(sourceUser, legacyUserID string) (string, string) {
	if trimmed := strings.TrimSpace(sourceUser); trimmed != "" {
		return trimmed, trimmed
	}
	legacy := strings.TrimSpace(parseLegacyComposedUserID(legacyUserID))
	if legacy == "" {
		return "", ""
	}
	return legacy, legacy
}

// Hash returns a tenant-scoped deterministic digest for audit metadata.
func Hash(tenantID, subjectKey string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(tenantID) + "\x00" + strings.TrimSpace(subjectKey)))
	return hex.EncodeToString(sum[:])
}

func parseLegacyComposedUserID(userID string) string {
	trimmed := strings.TrimSpace(userID)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, "ext_") {
		if _, after, ok := strings.Cut(trimmed, ":"); ok {
			return strings.TrimSpace(after)
		}
		return trimmed
	}
	return trimmed
}
