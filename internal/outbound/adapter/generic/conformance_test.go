// SPDX-License-Identifier: Apache-2.0

package generic

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
			ID:               "target-generic",
			TenantID:         "tenant-conformance",
			URL:              "https://hooks.example.com/services/" + outboundtest.URLTokenMarker,
			Secret:           outboundtest.SecretMarker,
			DestinationType:  channelID,
			SignatureVersion: outbound.SignatureVersionContentHash,
		},
		Golden:        "testdata/event_request.json",
		Capabilities:  outboundtest.Capabilities{PreservesRawCustomerBody: true},
		ResponseCases: outboundtest.GenericWebhookResponses(),
	})
}

func TestConformanceDigest(t *testing.T) {
	outboundtest.TestDigestChannel(t, outboundtest.DigestCase{
		Channel: ptrext.Of(channel{}),
		Target: outbound.Target{
			ID:              "target-generic",
			TenantID:        "tenant-conformance",
			URL:             "https://hooks.example.com/services/" + outboundtest.URLTokenMarker,
			Secret:          outboundtest.SecretMarker,
			DestinationType: channelID,
		},
		Golden:        "testdata/digest_request.json",
		Capabilities:  outboundtest.Capabilities{PreservesRawCustomerBody: true},
		ResponseCases: outboundtest.GenericWebhookResponses(),
	})
}
