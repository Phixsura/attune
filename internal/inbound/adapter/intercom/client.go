// SPDX-License-Identifier: Apache-2.0

// client.go bridges the adapter to the shared intercomclient package via
// type aliases, keeping the HTTP implementation reusable for #32
// externalsync (same pattern as the zendesk adapter's client.go).
package intercom

import (
	"github.com/Phixsura/attune/internal/infra/intercomclient"
	"github.com/Phixsura/attune/internal/pkg/nethardening"
)

// Type aliases — adapter code uses the short names.
type (
	apiClient        = intercomclient.Client
	conversation     = intercomclient.Conversation
	conversationPage = intercomclient.ConversationPage
	part             = intercomclient.Part
	partAuthor       = intercomclient.PartAuthor
	contactRef       = intercomclient.ContactRef
	intercomContact  = intercomclient.Contact
	intercomAccount  = intercomclient.AccountInfo
	intercomCompany  = intercomclient.Company
	intercomAdmin    = intercomclient.Admin
	apiError         = intercomclient.APIError
	rateLimitError   = intercomclient.RateLimitError
)

type clientFactory func(region, accessToken string) intercomclient.Client

var newAPIClient clientFactory = intercomclient.New

// SetAPIBaseURL points the Intercom client at a test server.
func SetAPIBaseURL(u string) { intercomclient.SetTestBaseURL(u) }

// SetEgressPolicy overrides the SSRF dial policy.
func SetEgressPolicy(p nethardening.Policy) { intercomclient.SetEgressPolicy(p) }

// baseURL returns the Intercom API base URL for a region.
func baseURL(region string) string { return intercomclient.BaseURL(region) }

// validateHost ensures the target host is *.intercom.io in production.
func validateHost(base string) error { return intercomclient.ValidateHost(base) }
