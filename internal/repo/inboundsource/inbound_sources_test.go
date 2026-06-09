// SPDX-License-Identifier: Apache-2.0

package inboundsource_test

import (
	"testing"

	"github.com/Phixsura/attune/internal/inbound"
	"github.com/Phixsura/attune/internal/repo/inboundsource"
)

// Smoke that the package's public surface compiles and Repo satisfies the
// inbound.SourceStore interface — no DB required.
func TestInboundSourceRepo_SatisfiesSourceStore(t *testing.T) {
	if inboundsource.ErrNotFound.Error() == "" {
		t.Error("ErrNotFound error text is empty")
	}
	var _ inbound.SourceStore = (*inboundsource.Repo)(nil)
}
