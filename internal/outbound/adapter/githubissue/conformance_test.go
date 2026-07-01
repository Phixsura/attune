// SPDX-License-Identifier: Apache-2.0

package githubissue

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
			ID:              "target-github",
			TenantID:        "tenant-conformance",
			URL:             "https://github.com/attune/conformance",
			Secret:          outboundtest.SecretMarker,
			DestinationType: channelID,
		},
		Golden: "testdata/event_request.json",
		Capabilities: outboundtest.Capabilities{
			RequiresAuthHeader: true,
			AllowsHTTP201:      true,
			HasActiveMentions:  true,
		},
		ResponseCases: outboundtest.GitHubIssueResponses(),
		ForbiddenBody: []string{
			"@octocat",
			"@org/team",
			"<@U123456>",
		},
	})
}
