// SPDX-License-Identifier: Apache-2.0

package discord

import (
	"testing"

	"github.com/Phixsura/attune/internal/outbound"
	"github.com/Phixsura/attune/internal/outbound/outboundtest"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func TestConformanceEvent(t *testing.T) {
	outboundtest.TestEventChannel(t, outboundtest.EventCase{
		Channel: ptrext.Of(channel{}),
		Target: outbound.Target{
			ID:              "target-discord",
			TenantID:        "tenant-conformance",
			URL:             "https://discord.com/api/webhooks/123/" + outboundtest.URLTokenMarker,
			DestinationType: channelID,
		},
		Golden:        "testdata/event_request.json",
		ProviderShape: outboundtest.ProviderShapeDiscord,
		Capabilities: outboundtest.Capabilities{
			URLIsCredential:   true,
			HasActiveMentions: true,
			AllowsHTTP204:     true,
		},
		ResponseCases: outboundtest.ChatWebhookResponses(true),
	})
}

func TestConformanceDigest(t *testing.T) {
	outboundtest.TestDigestChannel(t, outboundtest.DigestCase{
		Channel: ptrext.Of(channel{}),
		Target: outbound.Target{
			ID:              "target-discord",
			TenantID:        "tenant-conformance",
			URL:             "https://discord.com/api/webhooks/123/" + outboundtest.URLTokenMarker,
			DestinationType: channelID,
		},
		Golden:        "testdata/digest_request.json",
		ProviderShape: outboundtest.ProviderShapeDiscord,
		Capabilities: outboundtest.Capabilities{
			URLIsCredential:   true,
			HasActiveMentions: true,
			AllowsHTTP204:     true,
		},
		ResponseCases: outboundtest.ChatWebhookResponses(true),
	})
}
