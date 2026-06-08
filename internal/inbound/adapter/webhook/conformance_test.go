// SPDX-License-Identifier: Apache-2.0

package webhook_test

import (
	"testing"

	"github.com/Phixsura/attune/internal/inbound"
	"github.com/Phixsura/attune/internal/inbound/adapter/webhook"
	"github.com/Phixsura/attune/internal/inbound/inboundtest"
)

// TestWebhookAdapterContract runs the framework's 6-criterion suite
// against a fresh webhook adapter each call. The conformance suite
// internally calls inbound.ResetForTest before its duplicate-Register
// check, so we use the public NewAdapter constructor (which doesn't
// depend on registry state) instead of inbound.Factories().
func TestWebhookAdapterContract(t *testing.T) {
	inboundtest.TestAdapterContract(t, func() inbound.Adapter {
		return webhook.NewAdapter()
	})
}
