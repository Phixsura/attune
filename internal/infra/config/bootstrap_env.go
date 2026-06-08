// SPDX-License-Identifier: Apache-2.0

package config

import (
	"os"
	"strings"
)

// GetOrFile reads name, preferring "<name>_FILE" — which holds a path to
// a file containing the value. This is the standard *_FILE env pattern
// (Docker secrets, Kubernetes mounted secrets) that avoids exposing
// bootstrap passwords via /proc/<pid>/environ on Linux.
//
// Returns the empty string if neither <name>_FILE nor <name> is set, or
// if the file path was set but the file could not be read — the env-var
// fallback is NOT used in that case so a misconfigured file reference
// does not silently degrade security.
func GetOrFile(name string) string {
	if filePath := os.Getenv(name + "_FILE"); filePath != "" {
		b, err := os.ReadFile(filePath)
		if err != nil {
			return ""
		}
		return strings.TrimRight(string(b), "\r\n")
	}
	return os.Getenv(name)
}
