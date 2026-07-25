// SPDX-License-Identifier: Apache-2.0

package intercom

import (
	"context"
	"strings"

	"github.com/Phixsura/attune/internal/infra/intercomclient"
)

// Public aliases keep the adapter's runtime helpers reusable from the
// Console handler without re-declaring Intercom payload shapes in a
// second package.
type (
	AccountInfo = intercomAccount
	ConnInputs  = intercomConnInputs
)

// ValidateConnConfig normalizes the Intercom connection payload.
func ValidateConnConfig(region, accessToken, startFrom string, filterStates []string, maxDetailFetches int) (ConnInputs, error) {
	region = strings.ToLower(strings.TrimSpace(region))
	if !intercomclient.ValidRegion(region) {
		return ConnInputs{}, errMissing("region must be 'us', 'eu', or 'au'")
	}
	accessToken = strings.TrimSpace(accessToken)
	if accessToken == "" {
		return ConnInputs{}, errMissing("access_token is required")
	}
	startFrom = strings.TrimSpace(startFrom)
	switch startFrom {
	case "", "now":
		startFrom = "now"
	case "full":
	default:
		return ConnInputs{}, errMissing("start_from must be 'now' or 'full'")
	}
	states, err := normalizeFilterStates(filterStates)
	if err != nil {
		return ConnInputs{}, err
	}
	if maxDetailFetches < 0 {
		return ConnInputs{}, errMissing("max_detail_fetches must not be negative")
	}
	return ConnInputs{
		Region:           region,
		AccessToken:      accessToken,
		StartFrom:        startFrom,
		FilterStates:     states,
		MaxDetailFetches: maxDetailFetches,
	}, nil
}

// AuthTest checks an access token against Intercom's /me endpoint.
func AuthTest(ctx context.Context, region, accessToken string) (AccountInfo, error) {
	c := newAPIClient(region, accessToken)
	return c.AuthTest(ctx)
}

// IsPermanentError reports whether an Intercom API error should disable
// the source instead of being retried.
func IsPermanentError(err error) bool {
	return isPermanentIntercomError(err)
}

type missingFieldError struct{ msg string }

func (e missingFieldError) Error() string { return e.msg }

func errMissing(msg string) error { return missingFieldError{msg: msg} }
