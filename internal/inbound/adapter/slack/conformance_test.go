// SPDX-License-Identifier: Apache-2.0

package slack_test

import (
	"testing"

	"github.com/Phixsura/attune/internal/inbound"
	"github.com/Phixsura/attune/internal/inbound/adapter/slack"
	"github.com/Phixsura/attune/internal/inbound/inboundtest"
)

// TestSlackAdapterContract runs the shared inbound adapter contract suite
// against a fresh Slack adapter instance.
func TestSlackAdapterContract(t *testing.T) {
	inboundtest.TestAdapterContract(t, func() inbound.Adapter {
		return slack.NewAdapter()
	})
}
