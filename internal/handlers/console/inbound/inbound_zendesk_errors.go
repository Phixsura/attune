// SPDX-License-Identifier: Apache-2.0

package inbound

import "strings"

// friendlyZendeskError maps raw Zendesk API / validation errors to
// messages a non-technical operator can understand. Unknown patterns
// fall back to a generic sentence.
func friendlyZendeskError(err error, subdomain string) string {
	msg := err.Error()
	lower := strings.ToLower(msg)

	switch {
	case strings.Contains(lower, "no help desk"):
		return "Zendesk subdomain \"" + subdomain + "\" not found. Please verify the subdomain is correct."
	case strings.Contains(lower, "couldn't authenticate"):
		return "Zendesk authentication failed. Please verify the email and API token (or OAuth credentials)."
	case strings.Contains(lower, "unauthorized"):
		return "Zendesk authentication failed. The credentials were rejected."
	case strings.Contains(lower, "forbidden"):
		return "Zendesk access denied. The credentials lack the required permissions."
	case strings.Contains(lower, "rate_limited") || strings.Contains(lower, "429"):
		return "Zendesk rate limit reached. Please wait a moment and try again."
	case strings.Contains(lower, "timeout") || strings.Contains(lower, "deadline exceeded"):
		return "Connection to Zendesk timed out. Please check the subdomain and try again."
	case strings.Contains(lower, "no such host") || strings.Contains(lower, "dns"):
		return "Cannot reach \"" + subdomain + ".zendesk.com\". Please check the subdomain."
	case strings.Contains(lower, "can only contain"):
		// Already user-friendly from validateSubdomain.
		return msg
	case strings.Contains(lower, "is required"):
		// Already user-friendly from ValidateConnConfig.
		return msg
	default:
		return "Zendesk connection failed: " + msg
	}
}
