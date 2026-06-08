// SPDX-License-Identifier: Apache-2.0

package inbound

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"

	"github.com/Phixsura/attune/internal/infra/config"
)

// MasterKeyEnv — name of the env var read by BootstrapValidate.
const MasterKeyEnv = "ATTUNE_INBOUND_MASTER_KEY"

// BootstrapValidate is called at process start, BEFORE Manager.StartAll,
// so a misconfigured master key fails the boot instead of surfacing as
// a 500 the first time an admin creates an inbound source.
//
// The env var must decode to exactly 32 bytes. Both hex and standard
// base64 inputs are accepted — operators can paste `openssl rand -hex 32`
// output directly, or use the more compact base64 form (also 32 bytes
// raw → 44 chars base64). Empty / wrong length / undecodable → error.
//
// `ATTUNE_INBOUND_MASTER_KEY_FILE` is read first when set: it holds a
// path to a file containing the value (standard *_FILE pattern shared
// with the rest of the codebase via internal/infra/config.GetOrFile).
// This keeps the 32-byte master key off /proc/<pid>/environ on Linux
// (review H5, #66). A misconfigured `_FILE` path returns "" rather
// than silently falling back to the plain env var — bootstrap then
// aborts loudly with a clear error.
//
// Returns the decoded 32-byte key on success; pass it straight to
// NewAESGCMSecretStore.
func BootstrapValidate() ([]byte, error) {
	raw := config.GetOrFile(MasterKeyEnv)
	if raw == "" {
		return nil, fmt.Errorf("%s is not set; inbound framework cannot start", MasterKeyEnv)
	}
	// Try hex first (preferred input for `openssl rand -hex 32`),
	// fall back to base64 (Kubernetes secret manifests commonly use
	// base64).
	if key, err := hex.DecodeString(raw); err == nil && len(key) == 32 {
		return key, nil
	}
	if key, err := base64.StdEncoding.DecodeString(raw); err == nil && len(key) == 32 {
		return key, nil
	}
	return nil, fmt.Errorf(
		"%s must decode to exactly 32 bytes (hex or base64); got %d-byte input",
		MasterKeyEnv, len(raw),
	)
}
