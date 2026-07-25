// SPDX-License-Identifier: Apache-2.0

// client.go bridges the adapter to the shared zendeskclient package via
// type aliases. This minimizes changes across the adapter while making
// the HTTP implementation reusable for #31 externalsync.
package zendesk

import (
	"github.com/Phixsura/attune/internal/infra/zendeskclient"
	"github.com/Phixsura/attune/internal/pkg/nethardening"
)

// Type aliases — adapter code continues using the short names.
type (
	apiClient           = zendeskclient.Client
	ticket              = zendeskclient.Ticket
	ticketPage          = zendeskclient.TicketPage
	ticketVia           = zendeskclient.TicketVia
	satisfactionRating  = zendeskclient.SatisfactionRating
	comment             = zendeskclient.Comment
	zendeskUser         = zendeskclient.User
	zendeskOrganization = zendeskclient.Organization
	zendeskAccountInfo  = zendeskclient.AccountInfo
	oauthToken          = zendeskclient.OAuthToken
	apiError            = zendeskclient.APIError
	rateLimitError      = zendeskclient.RateLimitError
	credential          = zendeskclient.Credential
)

// Re-export constants so adapter code doesn't need to change.
const (
	AuthModeAPIToken = zendeskclient.AuthModeAPIToken
	AuthModeOAuth    = zendeskclient.AuthModeOAuth
)

type clientFactory func(apiBaseURL string, cred zendeskclient.Credential) zendeskclient.Client

var newAPIClient clientFactory = func(apiBaseURL string, cred zendeskclient.Credential) zendeskclient.Client {
	return zendeskclient.New(apiBaseURL, cred)
}

// SetAPIBaseURL points the Zendesk client at a test server.
func SetAPIBaseURL(u string) { zendeskclient.SetTestBaseURL(u) }

// SetEgressPolicy overrides the SSRF dial policy.
func SetEgressPolicy(p nethardening.Policy) { zendeskclient.SetEgressPolicy(p) }

// baseURL returns the Zendesk API base URL for a subdomain.
func baseURL(subdomain string) string { return zendeskclient.BaseURL(subdomain) }

// validateHost ensures the target host is *.zendesk.com in production.
func validateHost(base string) error { return zendeskclient.ValidateHost(base) }
