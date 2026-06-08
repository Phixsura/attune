// SPDX-License-Identifier: Apache-2.0

package webhook

import (
	"crypto/rand"
	"sync"
)

// ProcessStubSecret returns a per-process random secret used to
// equalise wall-clock time on unknown-source responses (enumeration
// resistance). The secret is generated lazily on first call so test
// binaries that never serve traffic don't pay for the rand.Read.
func ProcessStubSecret() []byte {
	stubOnce.Do(func() {
		stubSecret = make([]byte, 32)
		_, _ = rand.Read(stubSecret)
	})
	return stubSecret
}

var (
	stubOnce   sync.Once
	stubSecret []byte
)
