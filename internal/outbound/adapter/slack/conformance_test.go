// SPDX-License-Identifier: Apache-2.0

package slack

import (
	"testing"

	"github.com/Phixsura/attune/internal/outbound"
	"github.com/Phixsura/attune/internal/outbound/outboundtest"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func TestConformanceEvent(t *testing.T) {
	outboundtest.TestEventChannel(t, outboundtest.EventCase{
		Channel:       ptrext.Of(channel{}),
		Target:        conformanceTarget(),
		Golden:        "testdata/event_request.json",
		Capabilities:  conformanceCapabilities(),
		ResponseCases: outboundtest.ChatWebhookResponses(false),
		ForbiddenBody: outboundtest.ChatMentionForbiddenBody(),
	})
}

func TestConformanceDigest(t *testing.T) {
	outboundtest.TestDigestChannel(t, outboundtest.DigestCase{
		Channel:       ptrext.Of(channel{}),
		Target:        conformanceTarget(),
		Golden:        "testdata/digest_request.json",
		Capabilities:  conformanceCapabilities(),
		ResponseCases: outboundtest.ChatWebhookResponses(false),
		ForbiddenBody: outboundtest.ChatMentionForbiddenBody(),
	})
}

func conformanceTarget() outbound.Target {
	return outbound.Target{
		ID:              "target-slack",
		TenantID:        "tenant-conformance",
		URL:             "https://hooks.slack.com/services/T0/B0/" + outboundtest.URLTokenMarker,
		DestinationType: channelID,
	}
}

func conformanceCapabilities() outboundtest.Capabilities {
	return outboundtest.Capabilities{
		URLIsCredential:   true,
		HasActiveMentions: true,
	}
}
