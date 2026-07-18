// SPDX-License-Identifier: Apache-2.0

package email

import (
	"testing"

	"github.com/Phixsura/attune/internal/outbound"
	"github.com/Phixsura/attune/internal/outbound/outboundtest"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func TestConformanceNotification(t *testing.T) {
	outboundtest.TestNotificationChannel(t, outboundtest.NotificationCase{
		Channel:       ptrext.Of(channel{}),
		Target:        conformanceTarget(),
		Golden:        "testdata/notification_request.json",
		ProviderShape: outboundtest.ProviderShapeEmail,
		Capabilities: outboundtest.Capabilities{
			RequiresAuthHeader: true,
		},
		ResponseCases: outboundtest.GenericWebhookResponses(),
	})
}

func conformanceTarget() outbound.Target {
	return outbound.Target{
		ID:              "target-email-provider",
		TenantID:        "tenant-conformance",
		URL:             "https://mail.example.test/send",
		Secret:          outboundtest.SecretMarker,
		DestinationType: channelID,
		Config: map[string]any{
			"from_name":  "Attune",
			"from_email": "updates@example.test",
			"reply_to":   "support@example.test",
			"to_email":   "customer@example.test",
		},
	}
}
