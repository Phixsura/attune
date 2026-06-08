// SPDX-License-Identifier: Apache-2.0

package inbound

import (
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
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
// Returns the decoded 32-byte key on success; pass it straight to
// NewAESGCMSecretStore.
func BootstrapValidate() ([]byte, error) {
	raw := os.Getenv(MasterKeyEnv)
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

// ErrMasterKeyMissing — sentinel so cmd/attune can distinguish "operator
// has not opted-in to the inbound framework yet" from "the env value is
// malformed". Currently both still fail boot — kept here as a forward
// hook for a future graceful-skip mode.
var ErrMasterKeyMissing = errors.New("inbound: master key not configured")
