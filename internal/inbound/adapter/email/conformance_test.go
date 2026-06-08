// SPDX-License-Identifier: Apache-2.0

package email_test

import (
	"testing"

	"github.com/Phixsura/attune/internal/inbound"
	"github.com/Phixsura/attune/internal/inbound/adapter/email"
	"github.com/Phixsura/attune/internal/inbound/inboundtest"
)

// TestEmailAdapterContract runs the framework's 6-criterion suite
// against a fresh email adapter each call.
func TestEmailAdapterContract(t *testing.T) {
	inboundtest.TestAdapterContract(t, func() inbound.Adapter {
		return email.NewAdapter()
	})
}
