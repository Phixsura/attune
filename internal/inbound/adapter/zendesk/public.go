// SPDX-License-Identifier: Apache-2.0

package zendesk

import (
	"context"
	"strings"
)

// Public aliases keep the adapter's runtime helpers reusable from the
// Console handler without re-declaring Zendesk payload shapes in a second
// package.
type (
	AccountInfo = zendeskAccountInfo
	ConnInputs  = zendeskConnInputs
)

// ValidateConnConfig normalizes the Zendesk connection payload.
func ValidateConnConfig(subdomain, authMode, email, apiToken, oauthAccessToken, oauthRefreshToken, oauthClientID, oauthClientSecret string) (ConnInputs, error) {
	subdomain = strings.TrimSpace(strings.ToLower(subdomain))
	if err := validateSubdomain(subdomain); err != nil {
		return ConnInputs{}, err
	}
	authMode = strings.TrimSpace(authMode)
	switch authMode {
	case AuthModeAPIToken:
		email = strings.TrimSpace(email)
		apiToken = strings.TrimSpace(apiToken)
		if email == "" {
			return ConnInputs{}, errMissing("email is required for api_token auth")
		}
		if apiToken == "" {
			return ConnInputs{}, errMissing("api_token is required")
		}
		return ConnInputs{
			Subdomain: subdomain,
			AuthMode:  authMode,
			Email:     email,
			APIToken:  apiToken,
		}, nil
	case AuthModeOAuth:
		oauthAccessToken = strings.TrimSpace(oauthAccessToken)
		if oauthAccessToken == "" {
			return ConnInputs{}, errMissing("oauth access_token is required")
		}
		return ConnInputs{
			Subdomain:         subdomain,
			AuthMode:          authMode,
			OAuthAccessToken:  oauthAccessToken,
			OAuthRefreshToken: strings.TrimSpace(oauthRefreshToken),
			OAuthClientID:     strings.TrimSpace(oauthClientID),
			OAuthClientSecret: strings.TrimSpace(oauthClientSecret),
		}, nil
	default:
		return ConnInputs{}, errMissing("auth_mode must be 'api_token' or 'oauth'")
	}
}

// AuthTestAPIToken checks API token credentials against Zendesk's users/me endpoint.
func AuthTestAPIToken(ctx context.Context, subdomain, email, apiToken string) (AccountInfo, error) {
	cred := credential{
		Mode:     AuthModeAPIToken,
		APIToken: []byte(apiToken),
		Email:    email,
	}
	defer wipeCred(cred)
	c := newAPIClient(baseURL(subdomain), cred)
	return c.AuthTest(ctx)
}

// AuthTestOAuth checks OAuth credentials against Zendesk's users/me endpoint.
func AuthTestOAuth(ctx context.Context, subdomain, accessToken string) (AccountInfo, error) {
	cred := credential{
		Mode:        AuthModeOAuth,
		AccessToken: accessToken,
	}
	c := newAPIClient(baseURL(subdomain), cred)
	return c.AuthTest(ctx)
}

// IsPermanentError reports whether a Zendesk API error should disable the
// source instead of being retried.
func IsPermanentError(err error) bool {
	return isPermanentZendeskError(err)
}

type missingFieldError struct{ msg string }

func (e missingFieldError) Error() string { return e.msg }

func errMissing(msg string) error { return missingFieldError{msg: msg} }
